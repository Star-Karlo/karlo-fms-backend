// Package notifrule is the notification-rule engine: which alert types are
// enabled, their thresholds/urgency, scope, and delivery recipients.
package notifrule

import (
	"encoding/json"
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

// DTO is the JSON shape of a notification rule.
type DTO struct {
	ID               int64           `json:"id"`
	RuleName         string          `json:"rule_name"`
	AlertCode        string          `json:"alert_code"`
	NotificationName *string         `json:"notification_name"`
	Urgency          string          `json:"urgency"`
	Status           string          `json:"status"`
	AppliesTo        string          `json:"applies_to"`
	Target           json.RawMessage `json:"target"`
	Threshold        *float64        `json:"threshold"`
	SentToSelf       bool            `json:"sent_to_self"`
	GeofenceScoped   bool            `json:"geofence_scoped"`
	TimeScoped       bool            `json:"time_scoped"`
	AutoPopup        bool            `json:"auto_popup"`
	Delivery         json.RawMessage `json:"delivery"`
	CreatedAt        *time.Time      `json:"created_at"`
}

// WriteRequest is the create/update body.
type WriteRequest struct {
	RuleName         string          `json:"rule_name"`
	AlertCode        string          `json:"alert_code"`
	NotificationName *string         `json:"notification_name"`
	Urgency          *string         `json:"urgency"`
	Status           *string         `json:"status"`
	AppliesTo        *string         `json:"applies_to"`
	Target           json.RawMessage `json:"target"`
	Threshold        *float64        `json:"threshold"`
	SentToSelf       *bool           `json:"sent_to_self"`
	GeofenceScoped   *bool           `json:"geofence_scoped"`
	TimeScoped       *bool           `json:"time_scoped"`
	AutoPopup        *bool           `json:"auto_popup"`
	Delivery         json.RawMessage `json:"delivery"`
}

func fromRow(r store.NotificationRule) DTO {
	return DTO{
		ID: r.ID, RuleName: r.RuleName, AlertCode: r.AlertCode, NotificationName: r.NotificationName,
		Urgency: r.Urgency, Status: r.Status, AppliesTo: r.AppliesTo, Target: json.RawMessage(r.Target),
		Threshold: conv.NumF(r.Threshold), SentToSelf: r.SentToSelf, GeofenceScoped: r.GeofenceScoped,
		TimeScoped: r.TimeScoped, AutoPopup: r.AutoPopup, Delivery: json.RawMessage(r.Delivery),
		CreatedAt: conv.Time(r.CreatedAt),
	}
}

// jsonBytes turns raw JSON into a []byte for a jsonb update param (nil ⇒ keep).
func jsonBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}

func iface[T any](p *T) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// Handler exposes notification-rule endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts rule endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/notification-rules", h.list)
	r.Get("/v1/notification-rules/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/notification-rules", h.create)
		mr.Put("/v1/notification-rules/{id}", h.update)
		mr.Delete("/v1/notification-rules/{id}", h.del)
	})
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List notification rules
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[notifrule.DTO]
//	@Router		/v1/notification-rules [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	tid := auth.TenantID(r.Context())
	var items []DTO
	var total int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		rows, err := st.ListNotificationRules(r.Context(), store.ListNotificationRulesParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, rr := range rows {
			items = append(items, fromRow(rr))
		}
		total, err = st.CountNotificationRules(r.Context(), store.CountNotificationRulesParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status)})
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
//	@Summary	Get a notification rule
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"id"
//	@Success	200	{object}	notifrule.DTO
//	@Router		/v1/notification-rules/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rr, err := store.New(tx).GetNotificationRule(r.Context(), id)
		if err != nil {
			return err
		}
		dto = fromRow(rr)
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
//	@Summary	Create a notification rule
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		notifrule.WriteRequest	true	"rule"
//	@Success	201		{object}	notifrule.DTO
//	@Router		/v1/notification-rules [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req WriteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.RuleName == "" || req.AlertCode == "" {
		httpx.Error(w, http.StatusBadRequest, "rule_name and alert_code are required")
		return
	}
	tid := auth.TenantID(r.Context())
	var dto DTO
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		rr, err := store.New(tx).CreateNotificationRule(r.Context(), store.CreateNotificationRuleParams{
			TenantID: tid, RuleName: req.RuleName, AlertCode: req.AlertCode, NotificationName: req.NotificationName,
			Urgency: iface(req.Urgency), Status: iface(req.Status), AppliesTo: iface(req.AppliesTo),
			Target: jsonBytes(req.Target), Threshold: conv.Num(req.Threshold),
			SentToSelf: iface(req.SentToSelf), GeofenceScoped: iface(req.GeofenceScoped),
			TimeScoped: iface(req.TimeScoped), AutoPopup: iface(req.AutoPopup), Delivery: jsonBytes(req.Delivery),
		})
		if err != nil {
			return err
		}
		dto = fromRow(rr)
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
//	@Summary	Update a notification rule
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"id"
//	@Param		body	body		notifrule.WriteRequest	true	"rule"
//	@Success	200		{object}	notifrule.DTO
//	@Router		/v1/notification-rules/{id} [put]
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
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rr, err := store.New(tx).UpdateNotificationRule(r.Context(), store.UpdateNotificationRuleParams{
			ID: id, RuleName: conv.StrPtr(req.RuleName), NotificationName: req.NotificationName,
			Urgency: req.Urgency, Status: req.Status, AppliesTo: req.AppliesTo,
			Target: jsonBytes(req.Target), Threshold: conv.Num(req.Threshold),
			SentToSelf: req.SentToSelf, GeofenceScoped: req.GeofenceScoped,
			TimeScoped: req.TimeScoped, AutoPopup: req.AutoPopup, Delivery: jsonBytes(req.Delivery),
		})
		if err != nil {
			return err
		}
		dto = fromRow(rr)
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
//	@Summary	Delete a notification rule
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/notification-rules/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteNotificationRule(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
