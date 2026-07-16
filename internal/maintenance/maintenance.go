// Package maintenance manages per-vehicle preventive-service schedules and the
// due/overdue reminder dashboard.
package maintenance

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is a service-schedule row with joined vehicle context.
type DTO struct {
	ID               int64    `json:"id"`
	VehicleID        *int64   `json:"vehicle_id"`
	LicensePlate     *string  `json:"license_plate"`
	VehicleType      *string  `json:"vehicle_type"`
	Component        *string  `json:"component"`
	Scheduler        *string  `json:"scheduler"`
	KmInterval       *float64 `json:"km_interval"`
	DayInterval      *int32   `json:"day_interval"`
	NextDueAt        *string  `json:"next_due_at"`
	Status           string   `json:"status"`
	CurrentOdometer  *float64 `json:"current_odometer"`
	CurrentHourmeter *float64 `json:"current_hourmeter"`
}

// Counts is the reminder-dashboard summary.
type Counts struct {
	Overdue   int64 `json:"overdue"`
	DueSoon   int64 `json:"due_soon"`
	InService int64 `json:"in_service"`
	Normal    int64 `json:"normal"`
}

// Response bundles a page of schedules with the status counts.
type Response struct {
	Items  []DTO  `json:"items"`
	Total  int64  `json:"total"`
	Counts Counts `json:"counts"`
}

// CreateRequest is the create-schedule body.
type CreateRequest struct {
	VehicleID   *int64   `json:"vehicle_id"`
	Component   *string  `json:"component"`
	Scheduler   *string  `json:"scheduler"`
	KmInterval  *float64 `json:"km_interval"`
	KmReminder  *float64 `json:"km_reminder"`
	DayInterval *int32   `json:"day_interval"`
	DayReminder *int32   `json:"day_reminder"`
	NextDueAt   *string  `json:"next_due_at"`
	Status      *string  `json:"status"`
}

func fromRow(s store.ListServiceSchedulesRow) DTO {
	return DTO{
		ID: s.ID, VehicleID: s.VehicleID, LicensePlate: s.LicensePlate, VehicleType: s.VehicleType,
		Component: s.Component, Scheduler: s.Scheduler, KmInterval: conv.NumF(s.KmInterval),
		DayInterval: s.DayInterval, NextDueAt: conv.DateStr(s.NextDueAt), Status: s.Status,
		CurrentOdometer: conv.NumF(s.CurrentOdometer), CurrentHourmeter: conv.NumF(s.CurrentHourmeter),
	}
}

// Handler exposes maintenance endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts maintenance endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/service-schedules", h.list)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/service-schedules", h.create)
		mr.Delete("/v1/service-schedules/{id}", h.del)
	})
}

// list godoc
//
//	@Summary	List service schedules with due/overdue counts
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Produce	json
//	@Param		q		query		string	false	"search plate/component"
//	@Param		status	query		string	false	"normal|due_soon|overdue|in_service"
//	@Success	200	{object}	maintenance.Response
//	@Router		/v1/service-schedules [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	tid := auth.TenantID(r.Context())
	var resp Response
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		rows, err := st.ListServiceSchedules(r.Context(), store.ListServiceSchedulesParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		resp.Items = make([]DTO, 0, len(rows))
		for _, s := range rows {
			resp.Items = append(resp.Items, fromRow(s))
		}
		resp.Total, err = st.CountServiceSchedules(r.Context(), store.CountServiceSchedulesParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status)})
		if err != nil {
			return err
		}
		c, err := st.ServiceStatusCounts(r.Context())
		if err != nil {
			return err
		}
		resp.Counts = Counts{Overdue: c.Overdue, DueSoon: c.DueSoon, InService: c.InService, Normal: c.Normal}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// create godoc
//
//	@Summary	Create a service schedule
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		maintenance.CreateRequest	true	"schedule"
//	@Success	201		{object}	map[string]int64
//	@Router		/v1/service-schedules [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	nextDue, err := conv.Date(req.NextDueAt)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "next_due_at must be YYYY-MM-DD")
		return
	}
	tid := auth.TenantID(r.Context())
	var id int64
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		s, err := store.New(tx).CreateServiceSchedule(r.Context(), store.CreateServiceScheduleParams{
			TenantID: tid, VehicleID: req.VehicleID, Component: req.Component, Scheduler: req.Scheduler,
			KmInterval: conv.Num(req.KmInterval), KmReminder: conv.Num(req.KmReminder),
			DayInterval: req.DayInterval, DayReminder: req.DayReminder, NextDueAt: nextDue,
			Status: ifaceStr(req.Status),
		})
		if err != nil {
			return err
		}
		id = s.ID
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusBadRequest, e.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// del godoc
//
//	@Summary	Delete a service schedule
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/service-schedules/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	e := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteServiceSchedule(r.Context(), id)
	})
	if e != nil {
		httpx.Error(w, http.StatusInternalServerError, e.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ifaceStr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}
