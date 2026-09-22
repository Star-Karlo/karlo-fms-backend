// Package fleetgroup provides CRUD for vehicle fleet groups.
package fleetgroup

import (
	"context"
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

// DTO is the JSON shape of a fleet group.
type DTO struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

// WriteRequest is the create/update body.
type WriteRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func fromRow(g store.FleetGroup) DTO {
	return DTO{ID: g.ID, Name: g.Name, Description: g.Description, CreatedAt: conv.Time(g.CreatedAt), UpdatedAt: conv.Time(g.UpdatedAt)}
}

// Service holds fleet-group logic.
type Service struct{ pool *db.Pool }

// NewService builds the service.
func NewService(pool *db.Pool) *Service { return &Service{pool: pool} }

func (s *Service) list(ctx context.Context, tid int64, p httpx.ListParams) ([]DTO, int64, error) {
	var out []DTO
	var total int64
	err := db.Tx(ctx, s.pool, tid, func(tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ListFleetGroups(ctx, store.ListFleetGroupsParams{Q: conv.StrPtr(p.Q), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		out = make([]DTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromRow(r))
		}
		total, err = q.CountFleetGroups(ctx, conv.StrPtr(p.Q))
		return err
	})
	return out, total, err
}

// Handler exposes fleet-group endpoints.
type Handler struct {
	svc *Service
	md  *mdwrite.Client         // nil before cutover: groups stay local
	dir mdwrite.CompanyResolver // tenant -> company for the write-through's caller
}

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// WithMasterData makes group writes reach master data first (catalogue
// kind vehicleGroup) — it owns groups; groupsync projects them back.
func (h *Handler) WithMasterData(md *mdwrite.Client, dir mdwrite.CompanyResolver) *Handler {
	h.md, h.dir = md, dir
	return h
}

// caller builds the identity to forward upstream, or writes a 409 when a
// staff member acts for a tenant whose company is unknown.
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Routes mounts fleet-group endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/fleet-groups", h.list)
	r.Get("/v1/fleet-groups/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/fleet-groups", h.create)
		mr.Put("/v1/fleet-groups/{id}", h.update)
		mr.Delete("/v1/fleet-groups/{id}", h.del)
	})
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List fleet groups
//	@Tags		fleet-groups
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[fleetgroup.DTO]
//	@Router		/v1/fleet-groups [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, total, err := h.svc.list(r.Context(), auth.TenantID(r.Context()), httpx.ParseList(r))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// get godoc
//
//	@Summary	Get a fleet group
//	@Tags		fleet-groups
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"id"
//	@Success	200	{object}	fleetgroup.DTO
//	@Router		/v1/fleet-groups/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto DTO
	err := db.Tx(r.Context(), h.svc.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		g, err := store.New(tx).GetFleetGroup(r.Context(), id)
		if err != nil {
			return err
		}
		dto = fromRow(g)
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
//	@Summary	Create a fleet group
//	@Tags		fleet-groups
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		fleetgroup.WriteRequest	true	"group"
//	@Success	201		{object}	fleetgroup.DTO
//	@Router		/v1/fleet-groups [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req WriteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	tid := auth.TenantID(r.Context())
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	// Master data first, so the row is born paired; its refusal is the answer.
	var mdID *string
	if h.md != nil {
		id, err := h.md.CreateVehicleGroup(r.Context(), caller, mdwrite.VehicleGroup{Name: req.Name, Description: deref(req.Description)})
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "master data refused the group: "+err.Error())
			return
		}
		if id != "" {
			mdID = &id
		}
	}
	var dto DTO
	err := db.Tx(r.Context(), h.svc.pool, tid, func(tx pgx.Tx) error {
		g, err := store.New(tx).CreateFleetGroup(r.Context(), store.CreateFleetGroupParams{TenantID: tid, Name: req.Name, Description: req.Description, MasterDataID: mdID})
		if err != nil {
			return err
		}
		dto = fromRow(g)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a fleet group
//	@Tags		fleet-groups
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"id"
//	@Param		body	body		fleetgroup.WriteRequest	true	"group"
//	@Success	200		{object}	fleetgroup.DTO
//	@Router		/v1/fleet-groups/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req WriteRequest
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
	err := db.Tx(r.Context(), h.svc.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		g, err := store.New(tx).UpdateFleetGroup(r.Context(), store.UpdateFleetGroupParams{ID: id, Name: conv.StrPtr(req.Name), Description: req.Description})
		if err != nil {
			return err
		}
		mdID = g.MasterDataID
		dto = fromRow(g)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	// Mirror upstream; an unpaired group (pre-backfill) is left to the backfill.
	if h.md != nil && mdID != nil {
		if err := h.md.UpdateVehicleGroup(r.Context(), caller, *mdID, mdwrite.VehicleGroup{Name: req.Name, Description: deref(req.Description)}); err != nil {
			log.Printf("fleet group %d: local update committed, master data %s rejected it: %v", id, *mdID, err)
			httpx.Error(w, http.StatusBadGateway, "saved here, but master data rejected it: "+err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// del godoc
//
//	@Summary	Delete a fleet group
//	@Tags		fleet-groups
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/fleet-groups/{id} [delete]
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
	err := db.Tx(r.Context(), h.svc.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		q := store.New(tx)
		if g, err := q.GetFleetGroup(r.Context(), id); err == nil {
			mdID = g.MasterDataID
		}
		return q.DeleteFleetGroup(r.Context(), id)
	})
	// Retire it upstream too, or groupsync brings it back on the next pass.
	if err == nil && h.md != nil && mdID != nil {
		if e := h.md.DeleteVehicleGroup(r.Context(), caller, *mdID); e != nil {
			log.Printf("fleet group %d: deleted locally, master data %s rejected the delete: %v", id, *mdID, e)
		}
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
