package vehicle

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
)

// Handler exposes vehicle CRUD endpoints.
type Handler struct {
	svc *Service
	dir mdwrite.CompanyResolver // tenant id -> master-data company, for staff acting via ?account_id
}

// NewHandler builds the vehicle HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// WithResolver lets staff writes made with ?account_id reach master data under
// the customer's company rather than the staff member's own.
func (h *Handler) WithResolver(dir mdwrite.CompanyResolver) *Handler { h.dir = dir; return h }

// caller builds the identity to forward upstream, or writes a 409 and returns
// false when a staff member is acting for a tenant whose company is unknown —
// refusing beats filing the record under the wrong customer.
func (h *Handler) caller(w http.ResponseWriter, r *http.Request) (mdwrite.Caller, bool) {
	c, err := mdwrite.CallerFor(r, auth.TenantID(r.Context()), auth.EffectiveTenant(r), h.dir)
	if err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return c, false
	}
	return c, true
}

// Routes mounts vehicle endpoints. Reads require any authenticated user;
// mutations require manager+.
func (h *Handler) Routes(r chi.Router) {
	h.CatalogRoutes(r)
	r.Get("/v1/vehicles", h.list)
	r.Get("/v1/vehicles/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/vehicles", h.create)
		mr.Put("/v1/vehicles/{id}", h.update)
		mr.Delete("/v1/vehicles/{id}", h.archive)
		mr.Post("/v1/vehicles/{id}/assign-driver", h.assignDriver)
	})
	// Binding a tracker (IMEI) to a vehicle IS device configuration, so it is
	// restricted to the platform operator like the tracker registry (?account_id
	// lets them do it for any customer account).
	r.Group(func(pr chi.Router) {
		pr.Use(auth.RequireRole(auth.RolePlatformAdmin))
		pr.Post("/v1/vehicles/{id}/assign-tracker", h.assignTracker)
	})
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List vehicles
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Produce	json
//	@Param		q				query		string	false	"search plate/hull"
//	@Param		status			query		string	false	"filter status"
//	@Param		fleet_group_id	query		int		false	"filter fleet group"
//	@Param		limit			query		int		false	"page size"
//	@Param		offset			query		int		false	"offset"
//	@Success	200	{object}	httpx.List[vehicle.DTO]
//	@Router		/v1/vehicles [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	q := r.URL.Query()
	var fg *int64
	if v := q.Get("fleet_group_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			fg = &id
		}
	}
	// Platform admin may list across accounts: ?all_accounts=true or ?account_id=N.
	if c, ok := auth.FromContext(r.Context()); ok && c.Role == auth.RolePlatformAdmin && (q.Get("all_accounts") == "true" || q.Get("account_id") != "") {
		var only *int64
		if v := q.Get("account_id"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				only = &id
			}
		}
		items, total, err := h.svc.ListAcross(r.Context(), only, p, fg)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
		return
	}
	items, total, err := h.svc.List(r.Context(), auth.TenantID(r.Context()), p, fg)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// get godoc
//
//	@Summary	Get a vehicle
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"vehicle id"
//	@Success	200	{object}	vehicle.DTO
//	@Failure	404	{object}	map[string]string
//	@Router		/v1/vehicles/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	// Platform admin may view a vehicle in another account via ?account_id
	// (EffectiveTenant); everyone else is scoped to their own tenant.
	dto, err := h.svc.Get(r.Context(), auth.EffectiveTenant(r), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "vehicle not found")
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// create godoc
//
//	@Summary	Create a vehicle
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		vehicle.CreateRequest	true	"vehicle"
//	@Success	201		{object}	vehicle.DTO
//	@Router		/v1/vehicles [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.LicensePlate == "" {
		httpx.Error(w, http.StatusBadRequest, "license_plate is required")
		return
	}
	// Platform admin may create the vehicle in a chosen customer account via
	// ?account_id (EffectiveTenant); everyone else creates in their own tenant.
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	dto, err := h.svc.Create(r.Context(), auth.EffectiveTenant(r), caller, req)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a vehicle
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"vehicle id"
//	@Param		body	body		vehicle.UpdateRequest	true	"fields"
//	@Success	200		{object}	vehicle.DTO
//	@Router		/v1/vehicles/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req UpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	dto, err := h.svc.Update(r.Context(), auth.EffectiveTenant(r), id, caller, req)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// archive godoc
//
//	@Summary		Delete a vehicle — archive first, remove on the second call
//	@Description	Two steps, because "delete" means two things. On an active vehicle this archives it: out of live view and the registry's default list, still there to explain past trips, and retired in master data so the projection does not restore it. Called again on a vehicle that is ALREADY archived, it deletes the row for good, together with the FMS records that name it (alerts, service schedules, score events, SOP runs, documents) and, by cascade, its telemetry state, geofence membership, rule attachments and tracker fit history. Telemetry itself — positions and trips — lives in the other database keyed by IMEI and is not touched. The response says which step ran.
//	@Tags			vehicles
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"vehicle id"
//	@Success		200	{object}	vehicle.RemoveResult
//	@Failure		404	{object}	map[string]string
//	@Failure		502	{object}	map[string]string	"master data would not retire it, so nothing was changed"
//	@Router			/v1/vehicles/{id} [delete]
func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Remove(r.Context(), auth.EffectiveTenant(r), id, caller)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, http.StatusNotFound, "vehicle not found")
			return
		}
		// A refusal from master data is the usual failure, and it means
		// nothing changed here — say so with a status that is not ours.
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// assignTracker godoc
//
//	@Summary	Assign/unassign a tracker to a vehicle
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int								true	"vehicle id"
//	@Param		body	body		vehicle.AssignTrackerRequest	true	"tracker"
//	@Success	200		{object}	vehicle.DTO
//	@Router		/v1/vehicles/{id}/assign-tracker [post]
func (h *Handler) assignTracker(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req AssignTrackerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	dto, err := h.svc.AssignTracker(r.Context(), auth.EffectiveTenant(r), id, caller, req.TrackerID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// assignDriver godoc
//
//	@Summary	Assign a driver to a vehicle
//	@Description	Sets the vehicle's current driver here and mirrors it to master data as currentDriverId (null to unassign), so TMS sees the same pairing. 400 with the local change kept when the driver is not paired with master data or master data rejects it.
//	@Tags		vehicles
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int								true	"vehicle id"
//	@Param		body	body		vehicle.AssignDriverRequest		true	"driver"
//	@Success	200		{object}	vehicle.DTO
//	@Router		/v1/vehicles/{id}/assign-driver [post]
func (h *Handler) assignDriver(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req AssignDriverRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	dto, err := h.svc.AssignDriver(r.Context(), auth.EffectiveTenant(r), id, caller, req.DriverID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}
