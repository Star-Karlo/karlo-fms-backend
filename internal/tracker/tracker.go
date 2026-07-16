// Package tracker provides CRUD for GPS tracker devices (keyed by IMEI).
package tracker

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON shape of a tracker.
type DTO struct {
	ID        int64      `json:"id"`
	Imei      string     `json:"imei"`
	PhoneNo   *string    `json:"phone_no"`
	VehicleID *int64     `json:"vehicle_id"`
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// CreateRequest is the create body.
type CreateRequest struct {
	Imei      string  `json:"imei"`
	PhoneNo   *string `json:"phone_no"`
	VehicleID *int64  `json:"vehicle_id"`
	Status    *string `json:"status"`
}

// UpdateRequest is the update body (patch-style).
type UpdateRequest struct {
	Imei    *string `json:"imei"`
	PhoneNo *string `json:"phone_no"`
	Status  *string `json:"status"`
}

func fromRow(t store.Tracker) DTO {
	return DTO{ID: t.ID, Imei: t.Imei, PhoneNo: t.PhoneNo, VehicleID: t.VehicleID, Status: t.Status, CreatedAt: conv.Time(t.CreatedAt), UpdatedAt: conv.Time(t.UpdatedAt)}
}

func iface(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// Handler exposes tracker endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the tracker handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts tracker endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/trackers", h.list)
	r.Get("/v1/trackers/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/trackers", h.create)
		mr.Put("/v1/trackers/{id}", h.update)
		mr.Delete("/v1/trackers/{id}", h.del)
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
	tid := auth.TenantID(r.Context())
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
			items = append(items, fromRow(t))
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
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
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
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Imei == "" {
		httpx.Error(w, http.StatusBadRequest, "imei is required")
		return
	}
	tid := auth.TenantID(r.Context())
	var dto DTO
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		t, err := store.New(tx).CreateTracker(r.Context(), store.CreateTrackerParams{TenantID: tid, Imei: req.Imei, PhoneNo: req.PhoneNo, VehicleID: req.VehicleID, Status: iface(req.Status)})
		if err != nil {
			return err
		}
		dto = fromRow(t)
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
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		t, err := store.New(tx).UpdateTracker(r.Context(), store.UpdateTrackerParams{ID: id, Imei: req.Imei, PhoneNo: req.PhoneNo, Status: req.Status})
		if err != nil {
			return err
		}
		dto = fromRow(t)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
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
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteTracker(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusConflict, "tracker in use or delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
