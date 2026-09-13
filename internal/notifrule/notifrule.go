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
	// WindowMinutes, when set, makes this a FREQUENCY rule: fire when >= Threshold
	// events of AlertCode occur for a vehicle within this many minutes (e.g.
	// "smoking >= 10 times in 60 minutes"). Nil = an instantaneous rule.
	WindowMinutes  *int            `json:"window_minutes"`
	SentToSelf     bool            `json:"sent_to_self"`
	GeofenceScoped bool            `json:"geofence_scoped"`
	TimeScoped     bool            `json:"time_scoped"`
	AutoPopup      bool            `json:"auto_popup"`
	Delivery       json.RawMessage `json:"delivery"`
	CreatedAt      *time.Time      `json:"created_at"`
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
	WindowMinutes    *int            `json:"window_minutes"`
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
		Threshold: conv.NumF(r.Threshold), WindowMinutes: i32toInt(r.WindowMinutes),
		SentToSelf: r.SentToSelf, GeofenceScoped: r.GeofenceScoped,
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

// i32toInt / intToI32 bridge the store's nullable int32 window column and the
// DTO's *int.
func i32toInt(p *int32) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func intToI32(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

// Handler exposes notification-rule endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts rule endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/notification-rules", h.list)
	r.Get("/v1/notification-rules/{id}", h.get)
	// Which rules apply to a given vehicle (with an attached flag), for the
	// vehicle panel's rule toggles.
	r.Get("/v1/vehicles/{vehicle_id}/rules", h.vehicleRules)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/notification-rules", h.create)
		mr.Put("/v1/notification-rules/{id}", h.update)
		mr.Delete("/v1/notification-rules/{id}", h.del)
		// Attach / detach a rule to a vehicle.
		mr.Post("/v1/vehicles/{vehicle_id}/rules/{rule_id}", h.attach)
		mr.Delete("/v1/vehicles/{vehicle_id}/rules/{rule_id}", h.detach)
	})
}

// VehicleRule is a rule plus whether it is attached to the queried vehicle.
type VehicleRule struct {
	ID        int64    `json:"id"`
	RuleName  string   `json:"rule_name"`
	AlertCode string   `json:"alert_code"`
	Urgency   string   `json:"urgency"`
	Status    string   `json:"status"`
	Threshold *float64 `json:"threshold"`
	Attached  bool     `json:"attached"`
}

// vehicleRules godoc
//
//	@Summary	Rules applicable to a vehicle, each flagged attached/not
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path		int	true	"vehicle id"
//	@Success	200			{object}	httpx.List[notifrule.VehicleRule]
//	@Router		/v1/vehicles/{vehicle_id}/rules [get]
func (h *Handler) vehicleRules(w http.ResponseWriter, r *http.Request) {
	vid, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	tid := auth.TenantID(r.Context())
	var out []VehicleRule
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		attachedIDs, err := st.ListRuleIDsForVehicle(r.Context(), vid)
		if err != nil {
			return err
		}
		attached := make(map[int64]bool, len(attachedIDs))
		for _, id := range attachedIDs {
			attached[id] = true
		}
		rules, err := st.ListNotificationRules(r.Context(), store.ListNotificationRulesParams{Lim: 500, Off: 0})
		if err != nil {
			return err
		}
		out = make([]VehicleRule, 0, len(rules))
		for _, rr := range rules {
			out = append(out, VehicleRule{
				ID: rr.ID, RuleName: rr.RuleName, AlertCode: rr.AlertCode, Urgency: rr.Urgency,
				Status: rr.Status, Threshold: conv.NumF(rr.Threshold), Attached: attached[rr.ID],
			})
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[VehicleRule]{Items: out, Total: int64(len(out))})
}

// attach godoc
//
//	@Summary	Attach a rule to a vehicle
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Param		vehicle_id	path	int	true	"vehicle id"
//	@Param		rule_id		path	int	true	"rule id"
//	@Success	204
//	@Router		/v1/vehicles/{vehicle_id}/rules/{rule_id} [post]
func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	h.attachDetach(w, r, true)
}

// detach godoc
//
//	@Summary	Detach a rule from a vehicle
//	@Tags		notification-rules
//	@Security	BearerAuth
//	@Param		vehicle_id	path	int	true	"vehicle id"
//	@Param		rule_id		path	int	true	"rule id"
//	@Success	204
//	@Router		/v1/vehicles/{vehicle_id}/rules/{rule_id} [delete]
func (h *Handler) detach(w http.ResponseWriter, r *http.Request) {
	h.attachDetach(w, r, false)
}

func (h *Handler) attachDetach(w http.ResponseWriter, r *http.Request, add bool) {
	vid, err1 := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	rid, err2 := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid ids")
		return
	}
	tid := auth.TenantID(r.Context())
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		if add {
			return st.AttachRuleVehicle(r.Context(), store.AttachRuleVehicleParams{TenantID: tid, RuleID: rid, VehicleID: vid})
		}
		return st.DetachRuleVehicle(r.Context(), store.DetachRuleVehicleParams{RuleID: rid, VehicleID: vid})
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
			Target: jsonBytes(req.Target), Threshold: conv.Num(req.Threshold), WindowMinutes: intToI32(req.WindowMinutes),
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
			Target: jsonBytes(req.Target), Threshold: conv.Num(req.Threshold), WindowMinutes: intToI32(req.WindowMinutes),
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
