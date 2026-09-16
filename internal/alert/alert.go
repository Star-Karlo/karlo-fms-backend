package alert

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/coldstore"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON shape of an alert (with joined plate/driver + taxonomy label).
type DTO struct {
	ID           int64      `json:"id"`
	VehicleID    *int64     `json:"vehicle_id"`
	LicensePlate *string    `json:"license_plate"`
	DriverID     *int64     `json:"driver_id"`
	DriverName   *string    `json:"driver_name"`
	Source       *string    `json:"source"`
	DeviceSource *string    `json:"device_source"`
	AlertCode    *string    `json:"alert_code"`
	AlertName    *string    `json:"alert_name"`
	NameEN       string     `json:"name_en"`
	Severity     *string    `json:"severity"`
	OccurredAt   *time.Time `json:"occurred_at"`
	ActualValue  *float64   `json:"actual_value"`
	LimitValue   *float64   `json:"limit_value"`
	Lat          *float64   `json:"lat"`
	Lon          *float64   `json:"lon"`
	MediaURL     *string    `json:"media_url"`
	Address      *string    `json:"address"`
	CheckStatus  string     `json:"check_status"`
	Note         *string    `json:"note"`
	CheckedAt    *time.Time `json:"checked_at"`
	// Who last changed check_status (a user id; the client resolves the name).
	CheckedBy *int64 `json:"checked_by"`
}

// HistoryItem is a compact alert entry for the detail timeline.
type HistoryItem struct {
	ID         int64      `json:"id"`
	AlertCode  *string    `json:"alert_code"`
	AlertName  *string    `json:"alert_name"`
	Severity   *string    `json:"severity"`
	OccurredAt *time.Time `json:"occurred_at"`
	Lat        *float64   `json:"lat"`
	Lon        *float64   `json:"lon"`
}

// Detail is the notification-detail payload: the alert plus its surrounding
// same-vehicle history (one hour on each side).
type Detail struct {
	Alert   DTO           `json:"alert"`
	History []HistoryItem `json:"history"`
}

// CheckRequest updates an alert's check status.
type CheckRequest struct {
	CheckStatus string  `json:"check_status"` // unchecked (open) | followed_up | checked (resolved) | false_alert
	Note        *string `json:"note"`
}

func nameEN(code *string) string {
	if code == nil {
		return ""
	}
	if t, ok := Lookup(*code); ok {
		return t.NameEN
	}
	return ""
}

func fromList(a store.ListAlertsRow) DTO {
	return DTO{
		ID: a.ID, VehicleID: a.VehicleID, LicensePlate: a.LicensePlate,
		DriverID: a.DriverID, DriverName: a.DriverName, Source: a.Source, DeviceSource: a.DeviceSource,
		AlertCode: a.AlertCode, AlertName: a.AlertName, NameEN: nameEN(a.AlertCode), Severity: a.Severity,
		OccurredAt: conv.Time(a.OccurredAt), ActualValue: conv.NumF(a.ActualValue), LimitValue: conv.NumF(a.LimitValue),
		Lat: a.Lat, Lon: a.Lon, MediaURL: a.MediaUrl, Address: a.Address,
		CheckStatus: a.CheckStatus, Note: a.Note, CheckedAt: conv.Time(a.CheckedAt), CheckedBy: a.CheckedBy,
	}
}

func fromGet(a store.GetAlertRow) DTO {
	return DTO{
		ID: a.ID, VehicleID: a.VehicleID, LicensePlate: a.LicensePlate,
		DriverID: a.DriverID, DriverName: a.DriverName, Source: a.Source, DeviceSource: a.DeviceSource,
		AlertCode: a.AlertCode, AlertName: a.AlertName, NameEN: nameEN(a.AlertCode), Severity: a.Severity,
		OccurredAt: conv.Time(a.OccurredAt), ActualValue: conv.NumF(a.ActualValue), LimitValue: conv.NumF(a.LimitValue),
		Lat: a.Lat, Lon: a.Lon, MediaURL: a.MediaUrl, Address: a.Address,
		CheckStatus: a.CheckStatus, Note: a.Note, CheckedAt: conv.Time(a.CheckedAt), CheckedBy: a.CheckedBy,
	}
}

// Handler exposes alert + taxonomy endpoints. When cold is set (and alertDays
// > 0), list and get transparently merge archived alerts from cold storage for
// queries that reach older than the boundary (now − alertDays).
type Handler struct {
	pool      *db.Pool
	cold      *coldstore.Store
	alertDays int
}

// NewHandler builds the alert handler. Pass a nil cold store to disable the
// hot+cold merge (alerts then behave hot-only, as before).
func NewHandler(pool *db.Pool, cold *coldstore.Store, alertDays int) *Handler {
	return &Handler{pool: pool, cold: cold, alertDays: alertDays}
}

// Routes mounts alert endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/alert-taxonomy", h.taxonomy)
	r.Get("/v1/alerts/analytics", h.analytics)
	r.Get("/v1/alerts", h.list)
	r.Get("/v1/alerts/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleOperator))
		mr.Patch("/v1/alerts/{id}/check", h.check)
		// Distinct prefix — "/v1/alerts/check-all" collides with the /v1/alerts/{id}
		// param route in chi and 405s.
		mr.Post("/v1/notifications/mark-all-read", h.checkAll)
	})
}

// checkAll godoc
//
//	@Summary	Mark all unchecked alerts as checked (mark all read)
//	@Tags		alerts
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	map[string]int64
//	@Router		/v1/notifications/mark-all-read [post]
func (h *Handler) checkAll(w http.ResponseWriter, r *http.Request) {
	tid := auth.TenantID(r.Context())
	var n int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		var err error
		n, err = store.New(tx).CheckAllAlerts(r.Context())
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int64{"checked": n})
}

// taxonomy godoc
//
//	@Summary	The master alert taxonomy
//	@Tags		alerts
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	alert.AlertType
//	@Router		/v1/alert-taxonomy [get]
func (h *Handler) taxonomy(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, Taxonomy)
}

func tsFromQuery(r *http.Request, key string) pgtype.Timestamptz {
	if v := r.URL.Query().Get(key); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return pgtype.Timestamptz{}
}

// Analytics is the Safety-tab dashboard payload (all real, from the alerts table).
type Analytics struct {
	ByType     []Bucket    `json:"by_type"`
	BySeverity []Bucket    `json:"by_severity"`
	ByHour     []HourCount `json:"by_hour"`
	ByDay      []DayCount  `json:"by_day"`
	TopVeh     []VehCount  `json:"top_vehicles"`
	Total      int64       `json:"total"`
}

// Bucket is a labeled count.
type Bucket struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// HourCount is an hour-of-day count.
type HourCount struct {
	Hour  int32 `json:"hour"`
	Count int64 `json:"count"`
}

// DayCount is a per-day count.
type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// VehCount is a per-vehicle count.
type VehCount struct {
	VehicleID *int64 `json:"vehicle_id"`
	Plate     string `json:"license_plate"`
	Count     int64  `json:"count"`
}

// analytics godoc
//
//	@Summary	Alert analytics for the Safety dashboard
//	@Tags		alerts
//	@Security	BearerAuth
//	@Produce	json
//	@Param		from	query		string	false	"RFC3339"
//	@Param		to		query		string	false	"RFC3339"
//	@Success	200	{object}	alert.Analytics
//	@Router		/v1/alerts/analytics [get]
func (h *Handler) analytics(w http.ResponseWriter, r *http.Request) {
	from, to := tsFromQuery(r, "from"), tsFromQuery(r, "to")
	tid := auth.TenantID(r.Context())
	var out Analytics
	out.ByType, out.BySeverity, out.ByHour, out.ByDay, out.TopVeh = []Bucket{}, []Bucket{}, []HourCount{}, []DayCount{}, []VehCount{}
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		types, err := st.AlertCountByType(r.Context(), store.AlertCountByTypeParams{From: from, To: to})
		if err != nil {
			return err
		}
		for _, t := range types {
			code := deref(t.AlertCode)
			out.ByType = append(out.ByType, Bucket{Key: code, Label: LabelFor(code), Count: t.Count})
			out.Total += t.Count
		}
		sev, err := st.AlertCountBySeverity(r.Context(), store.AlertCountBySeverityParams{From: from, To: to})
		if err != nil {
			return err
		}
		for _, s := range sev {
			out.BySeverity = append(out.BySeverity, Bucket{Key: deref(s.Severity), Label: deref(s.Severity), Count: s.Count})
		}
		hours, err := st.AlertCountByHour(r.Context(), store.AlertCountByHourParams{From: from, To: to})
		if err != nil {
			return err
		}
		for _, hc := range hours {
			out.ByHour = append(out.ByHour, HourCount{Hour: hc.Hour, Count: hc.Count})
		}
		days, err := st.AlertCountByDay(r.Context(), store.AlertCountByDayParams{From: from, To: to})
		if err != nil {
			return err
		}
		for _, d := range days {
			out.ByDay = append(out.ByDay, DayCount{Day: d.Day.Time.Format("2006-01-02"), Count: d.Count})
		}
		top, err := st.AlertTopVehicles(r.Context(), store.AlertTopVehiclesParams{From: from, To: to})
		if err != nil {
			return err
		}
		for _, v := range top {
			out.TopVeh = append(out.TopVeh, VehCount{VehicleID: v.VehicleID, Plate: deref(v.LicensePlate), Count: v.Count})
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// list godoc
//
//	@Summary	List alerts (Notification feed)
//	@Tags		alerts
//	@Security	BearerAuth
//	@Produce	json
//	@Param		q				query		string	false	"search"
//	@Param		check_status	query		string	false	"unchecked|followed_up|checked|false_alert (or 'open' = unchecked+followed_up)"
//	@Param		device_source	query		string	false	"gps|camera"
//	@Param		severity		query		string	false	"urgent|important|info"
//	@Param		alert_code		query		string	false	"taxonomy code"
//	@Param		vehicle_id		query		int		false	"vehicle filter"
//	@Param		from			query		string	false	"RFC3339"
//	@Param		to				query		string	false	"RFC3339"
//	@Success	200	{object}	httpx.List[alert.DTO]
//	@Router		/v1/alerts [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	q := r.URL.Query()
	var vehicleID *int64
	if v := q.Get("vehicle_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			vehicleID = &id
		}
	}
	from, to := tsFromQuery(r, "from"), tsFromQuery(r, "to")
	tid := auth.EffectiveTenant(r) // platform_admin may pass ?account_id to read another account

	// Hot query bounds. When the range reaches into cold storage, the hot side is
	// clamped to the boundary so hot ([boundary,to]) and cold ([from,boundary))
	// are disjoint — no dedup needed, and pagination is a simple concatenation
	// (hot rows are strictly newer than cold rows).
	useCold := h.coldEnabled(from)
	hotFrom := from
	if useCold {
		hotFrom = pgtype.Timestamptz{Time: h.boundary(), Valid: true}
	}

	listParams := store.ListAlertsParams{
		Q: conv.StrPtr(p.Q), CheckStatus: conv.StrPtr(q.Get("check_status")),
		DeviceSource: conv.StrPtr(q.Get("device_source")), Severity: conv.StrPtr(q.Get("severity")),
		AlertCode: conv.StrPtr(q.Get("alert_code")), VehicleID: vehicleID,
		From: hotFrom, To: to, Lim: int32(p.Limit), Off: int32(p.Offset),
	}
	countParams := store.CountAlertsParams{
		Q: conv.StrPtr(p.Q), CheckStatus: conv.StrPtr(q.Get("check_status")),
		DeviceSource: conv.StrPtr(q.Get("device_source")), Severity: conv.StrPtr(q.Get("severity")),
		AlertCode: conv.StrPtr(q.Get("alert_code")), VehicleID: vehicleID, From: hotFrom, To: to,
	}

	if !useCold {
		var items []DTO
		var total int64
		err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
			st := store.New(tx)
			rows, err := st.ListAlerts(r.Context(), listParams)
			if err != nil {
				return err
			}
			items = make([]DTO, 0, len(rows))
			for _, a := range rows {
				items = append(items, fromList(a))
			}
			total, err = st.CountAlerts(r.Context(), countParams)
			return err
		})
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
		return
	}

	// Merged hot + cold. Cold rows are fetched, filtered and sorted once; the
	// page is then sliced across the two tiers.
	filter := alertFilter{
		q: p.Q, checkStatus: q.Get("check_status"), deviceSource: q.Get("device_source"),
		severity: q.Get("severity"), alertCode: q.Get("alert_code"), vehicleID: vehicleID,
	}
	coldRows, err := h.coldAlerts(r.Context(), tid, from.Time, to, filter)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "cold read: "+err.Error())
		return
	}
	var hotTotal int64
	if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		var e error
		hotTotal, e = store.New(tx).CountAlerts(r.Context(), countParams)
		return e
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	total := hotTotal + int64(len(coldRows))

	items := make([]DTO, 0, p.Limit)
	// Hot slice: [offset, offset+limit) intersected with [0, hotTotal).
	if int64(p.Offset) < hotTotal {
		hotLim := p.Limit
		if rem := hotTotal - int64(p.Offset); rem < int64(hotLim) {
			hotLim = int(rem)
		}
		hp := listParams
		hp.Off, hp.Lim = int32(p.Offset), int32(hotLim)
		if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
			rows, e := store.New(tx).ListAlerts(r.Context(), hp)
			if e != nil {
				return e
			}
			for _, a := range rows {
				items = append(items, fromList(a))
			}
			return nil
		}); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	// Cold slice fills the remainder of the page.
	if remaining := p.Limit - len(items); remaining > 0 {
		coldStart := int64(p.Offset) - hotTotal
		if coldStart < 0 {
			coldStart = 0
		}
		if coldStart < int64(len(coldRows)) {
			end := coldStart + int64(remaining)
			if end > int64(len(coldRows)) {
				end = int64(len(coldRows))
			}
			items = append(items, coldRows[coldStart:end]...)
		}
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// get godoc
//
//	@Summary	Alert detail with surrounding same-vehicle history
//	@Tags		alerts
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id			path		int		true	"alert id"
//	@Param		occurred_at	query		string	false	"RFC3339 hint — enables cold-storage lookup for an archived alert"
//	@Success	200	{object}	alert.Detail
//	@Failure	404	{object}	map[string]string
//	@Router		/v1/alerts/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	tid := auth.TenantID(r.Context())
	var detail Detail
	found := false
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		a, err := st.GetAlert(r.Context(), id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil // fall through to the cold lookup
			}
			return err
		}
		found = true
		detail.Alert = fromGet(a)
		if a.VehicleID != nil && a.OccurredAt.Valid {
			center := a.OccurredAt.Time
			hist, err := st.AlertHistoryForVehicle(r.Context(), store.AlertHistoryForVehicleParams{
				VehicleID: a.VehicleID,
				From:      pgtype.Timestamptz{Time: center.Add(-time.Hour), Valid: true},
				To:        pgtype.Timestamptz{Time: center.Add(time.Hour), Valid: true},
			})
			if err != nil {
				return err
			}
			for _, hi := range hist {
				detail.History = append(detail.History, HistoryItem{
					ID: hi.ID, AlertCode: hi.AlertCode, AlertName: hi.AlertName,
					Severity: hi.Severity, OccurredAt: conv.Time(hi.OccurredAt), Lat: hi.Lat, Lon: hi.Lon,
				})
			}
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Cold fallback: an archived alert is gone from the hot table. With an
	// occurred_at hint we can read just that day's partition (surrounding history
	// isn't reconstructed for archived alerts — the row itself is what matters).
	if !found && h.cold != nil {
		if hint := tsFromQuery(r, "occurred_at"); hint.Valid {
			dto, ok, cerr := h.coldAlertByID(r.Context(), tid, id, hint.Time)
			if cerr != nil {
				httpx.Error(w, http.StatusInternalServerError, "cold read: "+cerr.Error())
				return
			}
			if ok {
				detail.Alert = dto
				found = true
			}
		}
	}
	if !found {
		httpx.Error(w, http.StatusNotFound, "alert not found")
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

// check godoc
//
//	@Summary	Set an alert's lifecycle status (unchecked=open / followed_up / checked=resolved / false_alert)
//	@Tags		alerts
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"alert id"
//	@Param		body	body		alert.CheckRequest	true	"status"
//	@Success	200		{object}	alert.DTO
//	@Router		/v1/alerts/{id}/check [patch]
func (h *Handler) check(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req CheckRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	switch req.CheckStatus {
	case "unchecked", "followed_up", "checked", "false_alert":
	default:
		httpx.Error(w, http.StatusBadRequest, "invalid check_status")
		return
	}
	tid := auth.TenantID(r.Context())
	var by *int64
	if uid := auth.UserID(r.Context()); uid != 0 {
		by = &uid
	}
	var dto DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		a, err := store.New(tx).UpdateAlertCheck(r.Context(), store.UpdateAlertCheckParams{ID: id, CheckStatus: req.CheckStatus, Note: req.Note, CheckedBy: by})
		if err != nil {
			return err
		}
		dto = DTO{
			ID: a.ID, VehicleID: a.VehicleID, AlertCode: a.AlertCode, AlertName: a.AlertName,
			NameEN: nameEN(a.AlertCode), Severity: a.Severity, CheckStatus: a.CheckStatus, Note: a.Note,
			OccurredAt: conv.Time(a.OccurredAt), CheckedAt: conv.Time(a.CheckedAt), CheckedBy: a.CheckedBy,
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}
