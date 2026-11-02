// Package tracker provides CRUD for GPS tracker devices (keyed by IMEI).
package tracker

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON shape of a tracker.
type DTO struct {
	// Owning account — set only in the platform-admin all-accounts list.
	TenantID   *int64  `json:"tenant_id,omitempty"`
	TenantName *string `json:"tenant_name,omitempty"`
	ID         int64   `json:"id"`
	Imei       string  `json:"imei"`
	PhoneNo    *string `json:"phone_no"`
	VehicleID  *int64  `json:"vehicle_id"`
	Status     string  `json:"status"`
	// VehiclePlate is the truck this device is fitted to right now, nil when
	// it is in stock. The id alone answers "is it linked"; the plate answers
	// "to what", which is the question actually being asked.
	VehiclePlate *string `json:"vehicle_plate"`
	// IsDemo marks a copy of a device that really belongs to another account
	// (shown here for a demo). Never paired with or written to master data.
	IsDemo bool `json:"is_demo"`
	// Owner is who owns the hardware — "karlo", "customer" or "vendor" — and
	// OwnerName is that third party when it is not Karlo's own unit. Both are
	// projected from master data, which is their only writer, so they are
	// read-only here and nil for a device master data has not labelled.
	// This is the field that separates our own trackers from a vendor's feed.
	Owner     *string    `json:"owner"`
	OwnerName *string    `json:"owner_name"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// CreateRequest is the create body.
type CreateRequest struct {
	Imei      string  `json:"imei"`
	PhoneNo   *string `json:"phone_no"`
	VehicleID *int64  `json:"vehicle_id"`
	Status    *string `json:"status"`
	// IsDemo: this account only demos the device; its real owner is another
	// account. Not registered in master data.
	IsDemo bool `json:"is_demo"`
}

// UpdateRequest is the update body (patch-style).
type UpdateRequest struct {
	Imei    *string `json:"imei"`
	PhoneNo *string `json:"phone_no"`
	Status  *string `json:"status"`
	IsDemo  *bool   `json:"is_demo"`
}

func fromRow(t store.Tracker) DTO {
	return DTO{ID: t.ID, Imei: t.Imei, PhoneNo: t.PhoneNo, VehicleID: t.VehicleID, Status: t.Status, IsDemo: t.IsDemo,
		Owner: t.Owner, OwnerName: t.OwnerName, CreatedAt: conv.Time(t.CreatedAt), UpdatedAt: conv.Time(t.UpdatedAt)}
}

// fromListRow is the same, plus the joined plate.
func fromListRow(t store.ListTrackersRow) DTO {
	return DTO{ID: t.ID, Imei: t.Imei, PhoneNo: t.PhoneNo, VehicleID: t.VehicleID, VehiclePlate: t.VehiclePlate,
		Status: t.Status, IsDemo: t.IsDemo, Owner: t.Owner, OwnerName: t.OwnerName,
		CreatedAt: conv.Time(t.CreatedAt), UpdatedAt: conv.Time(t.UpdatedAt)}
}

func iface(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// Handler exposes tracker endpoints.
type Handler struct {
	pool   *db.Pool
	md     *mdwrite.Client         // nil before cutover: writes stay local
	dir    mdwrite.CompanyResolver // tenant -> company for the write-through's caller
	wakers []interface{ Wake() }   // the device projection, woken after a transfer
}

// NewHandler builds the tracker handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// WithMasterData makes device writes reach master data first — it owns
// devices now (trackersync projects them back), so a device registered or
// re-fitted here must exist there or TMS keeps the old fitting.
func (h *Handler) WithMasterData(md *mdwrite.Client) *Handler { h.md = md; return h }

// WithResolver supplies the tenant -> company lookup the write-through needs.
func (h *Handler) WithResolver(dir mdwrite.CompanyResolver) *Handler { h.dir = dir; return h }

// caller builds the identity to forward upstream, or writes a 409 and
// returns false when a staff member acts for a tenant whose company is
// unknown — refusing beats filing the device under the wrong customer.
func (h *Handler) caller(w http.ResponseWriter, r *http.Request) (mdwrite.Caller, bool) {
	if h.md == nil {
		return mdwrite.Caller{}, true
	}
	c, err := mdwrite.CallerFor(r, auth.TenantID(r.Context()), auth.EffectiveTenant(r), h.dir)
	if err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return c, false
	}
	return c, true
}

// mdStatus maps this table's status onto master data's vocabulary.
func mdStatus(s *string) string {
	if s == nil {
		return ""
	}
	if *s == "inactive" {
		return "inactive"
	}
	return "active"
}

// Routes mounts tracker endpoints. IMEI/tracker configuration is restricted to
// the platform operator (platform_admin) -- customers do not self-configure
// devices. Writes accept ?account_id so the operator manages any account; reads
// stay tenant-scoped (a customer still sees its own device on the vehicle form).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/trackers", h.list)
	r.Get("/v1/trackers/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RolePlatformAdmin))
		mr.Post("/v1/trackers", h.create)
		mr.Put("/v1/trackers/{id}", h.update)
		mr.Delete("/v1/trackers/{id}", h.del)
		mr.Post("/v1/trackers/{id}/unassign", h.unassign)
		mr.Post("/v1/trackers/{id}/transfer", h.transfer)
	})
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List trackers
//	@Tags		trackers
//	@Security	BearerAuth
//	@Produce	json
//	@Param		q			query		string	false	"search imei/phone"
//	@Param		status		query		string	false	"filter status"
//	@Param		vehicle_id	query		int		false	"filter by vehicle"
//	@Success	200	{object}	httpx.List[tracker.DTO]
//	@Router		/v1/trackers [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	var veh *int64
	if v := r.URL.Query().Get("vehicle_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			veh = &id
		}
	}
	// Platform admin: ?all_accounts=true lists every account's devices at once,
	// each read inside its own tenant scope and tagged with its account.
	if c, ok := auth.FromContext(r.Context()); ok && c.Role == auth.RolePlatformAdmin && r.URL.Query().Get("all_accounts") == "true" {
		items, total := h.listAllAccounts(r, p, veh)
		httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
		return
	}

	tid := auth.EffectiveTenant(r)
	var items []DTO
	var total int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ListTrackers(r.Context(), store.ListTrackersParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), VehicleID: veh, Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, t := range rows {
			items = append(items, fromListRow(t))
		}
		total, err = q.CountTrackers(r.Context(), store.CountTrackersParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), VehicleID: veh})
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// listAllAccounts aggregates trackers across every tenant (platform admin), each
// read inside its own scope and tagged with its account.
func (h *Handler) listAllAccounts(r *http.Request, p httpx.ListParams, veh *int64) ([]DTO, int64) {
	out := []DTO{}
	var total int64
	rows, err := h.pool.Query(r.Context(), `SELECT id, name FROM tenants ORDER BY id`)
	if err != nil {
		return out, 0
	}
	type tref struct {
		id   int64
		name string
	}
	var tenants []tref
	for rows.Next() {
		var t tref
		if rows.Scan(&t.id, &t.name) == nil {
			tenants = append(tenants, t)
		}
	}
	rows.Close()
	for _, t := range tenants {
		tid, name := t.id, t.name
		_ = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
			q := store.New(tx)
			trs, err := q.ListTrackers(r.Context(), store.ListTrackersParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), VehicleID: veh, Lim: 1000, Off: 0})
			if err != nil {
				return err
			}
			for _, tr := range trs {
				d := fromListRow(tr)
				d.TenantID, d.TenantName = &tid, &name
				out = append(out, d)
			}
			total += int64(len(trs))
			return nil
		})
	}
	return out, total
}

// get godoc
//
//	@Summary	Get a tracker
//	@Tags		trackers
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"id"
//	@Success	200	{object}	tracker.DTO
//	@Router		/v1/trackers/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		t, err := store.New(tx).GetTracker(r.Context(), id)
		if err != nil {
			return err
		}
		dto = fromRow(t)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// create godoc
//
//	@Summary	Create a tracker
//	@Tags		trackers
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		tracker.CreateRequest	true	"tracker"
//	@Success	201		{object}	tracker.DTO
//	@Router		/v1/trackers [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	// Report a malformed body separately from a missing field. These were
	// collapsed into one "imei is required" message, which is actively
	// misleading: httpx.DecodeJSON sets DisallowUnknownFields, so a body that
	// carries one extra key — an imei included — fails decoding and the caller
	// is told the field they DID send is missing.
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Imei == "" {
		httpx.Error(w, http.StatusBadRequest, "imei is required")
		return
	}
	tid := auth.EffectiveTenant(r)
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	// Master data first, so the local row is born paired. Its refusal (a
	// duplicate IMEI in another company, say) is the caller's answer.
	var mdID *string
	if h.md != nil && !req.IsDemo {
		phone := ""
		if req.PhoneNo != nil {
			phone = *req.PhoneNo
		}
		id, err := h.md.CreateTracker(r.Context(), caller, mdwrite.Tracker{Kind: "gps", DeviceID: req.Imei, PhoneNo: phone, Status: mdStatus(req.Status)})
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "master data refused the tracker: "+err.Error())
			return
		}
		mdID = &id
	}
	var dto DTO
	var vehicleMD *string
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		q := store.New(tx)
		t, err := q.CreateTracker(r.Context(), store.CreateTrackerParams{TenantID: tid, Imei: req.Imei, PhoneNo: req.PhoneNo, VehicleID: req.VehicleID, Status: iface(req.Status), MasterDataID: mdID})
		if err != nil {
			return err
		}
		if req.IsDemo {
			if t, err = q.UpdateTracker(r.Context(), store.UpdateTrackerParams{ID: t.ID, IsDemo: &req.IsDemo}); err != nil {
				return err
			}
		}
		if req.VehicleID != nil {
			if v, err := q.GetVehicleRow(r.Context(), *req.VehicleID); err == nil {
				vehicleMD = v.MasterDataID
			}
		}
		dto = fromRow(t)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.md != nil && mdID != nil && req.VehicleID != nil {
		if vehicleMD == nil {
			log.Printf("tracker %d: fitted locally to vehicle %d, which has no master_data_id; master data not told", dto.ID, *req.VehicleID)
		} else if err := h.md.FitTracker(r.Context(), caller, *mdID, *vehicleMD); err != nil {
			log.Printf("tracker %d: created and fitted locally, master data %s refused the fitting: %v", dto.ID, *mdID, err)
		}
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a tracker
//	@Tags		trackers
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"id"
//	@Param		body	body		tracker.UpdateRequest	true	"fields"
//	@Success	200		{object}	tracker.DTO
//	@Router		/v1/trackers/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
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
	var dto DTO
	var mdID *string
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		t, err := store.New(tx).UpdateTracker(r.Context(), store.UpdateTrackerParams{ID: id, Imei: req.Imei, PhoneNo: req.PhoneNo, Status: req.Status, IsDemo: req.IsDemo})
		if err != nil {
			return err
		}
		mdID = t.MasterDataID
		dto = fromRow(t)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	// Mirror upstream. An unpaired device (pre-cutover row the reconcile
	// has not reached) is skipped, not created here.
	if h.md != nil && mdID != nil {
		t := mdwrite.Tracker{Status: mdStatus(req.Status)}
		if req.Imei != nil {
			t.DeviceID = *req.Imei
		}
		if req.PhoneNo != nil {
			t.PhoneNo = *req.PhoneNo
		}
		if req.Status == nil {
			t.Status = ""
		}
		if err := h.md.UpdateTracker(r.Context(), caller, *mdID, t); err != nil {
			log.Printf("tracker %d: local update committed, master data %s rejected it: %v", id, *mdID, err)
			httpx.Error(w, http.StatusBadGateway, "saved here, but master data rejected it: "+err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// del godoc
//
//	@Summary	Delete a tracker
//	@Tags		trackers
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Failure	409	{object}	map[string]string
//	@Router		/v1/trackers/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	var mdID *string
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		q := store.New(tx)
		if t, err := q.GetTracker(r.Context(), id); err == nil {
			mdID = t.MasterDataID
		}
		return q.DeleteTracker(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusConflict, "tracker in use or delete failed")
		return
	}
	// Retire it upstream too, or trackersync brings it back on the next pass.
	if h.md != nil && mdID != nil {
		if err := h.md.DeleteTracker(r.Context(), caller, *mdID); err != nil {
			log.Printf("tracker %d: deleted locally, master data %s rejected the delete: %v", id, *mdID, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
