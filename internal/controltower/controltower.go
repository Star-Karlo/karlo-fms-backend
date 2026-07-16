// Package controltower assembles the fleet-triage view: vehicles grouped by
// driving state, each annotated with its unresolved alerts, plus the
// checked/unchecked summary counters.
package controltower

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

var sevRank = map[string]int{"urgent": 3, "important": 2, "info": 1, "": 0}

// overspeedLimit (km/h) above which a live vehicle is flagged as reckless.
// 60 is a common heavy-truck safety limit on Indonesian roads.
const overspeedLimit = 60.0

// AlertLine is a compact unchecked-alert entry shown on a vehicle card.
type AlertLine struct {
	ID         int64      `json:"id"`
	AlertCode  *string    `json:"alert_code"`
	AlertName  *string    `json:"alert_name"`
	Severity   *string    `json:"severity"`
	OccurredAt *time.Time `json:"occurred_at"`
}

// VehicleCard is one vehicle in a driving-state group.
type VehicleCard struct {
	VehicleID     int64       `json:"vehicle_id"`
	LicensePlate  string      `json:"license_plate"`
	DriveState    string      `json:"drive_state"`
	WorstSeverity string      `json:"worst_severity"`
	UncheckedN    int         `json:"unchecked_count"`
	Alerts        []AlertLine `json:"alerts"`
	MoreCount     int         `json:"more_count"`
}

// Summary holds the header counters.
type Summary struct {
	UncheckedVehicles int `json:"unchecked_vehicles"`
	UncheckedAlerts   int `json:"unchecked_alerts"`
	CheckedAlerts     int `json:"checked_alerts"`
	OverspeedingNow   int `json:"overspeeding_now"`
}

// Response is the full Control Tower payload.
type Response struct {
	Summary Summary                  `json:"summary"`
	Groups  map[string][]VehicleCard `json:"groups"` // keyed by drive_state
}

// Service composes live-view (drive state) with alert aggregation.
type Service struct {
	pool *db.Pool
	live *liveview.Service
}

// NewService builds the control-tower service.
func NewService(pool *db.Pool, live *liveview.Service) *Service {
	return &Service{pool: pool, live: live}
}

const maxLinesPerCard = 3

// Get builds the control-tower view for a time window.
func (s *Service) Get(ctx context.Context, tenantID int64, from, to time.Time) (*Response, error) {
	// Vehicles + their current drive state (telemetry-backed, degrades to offline).
	vehicles, err := s.live.List(ctx, tenantID, nil)
	if err != nil {
		return nil, err
	}

	// Alerts in the window, grouped by vehicle.
	var rows []store.AlertsInWindowRow
	err = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).AlertsInWindow(ctx, store.AlertsInWindowParams{
			From: pgtype.Timestamptz{Time: from, Valid: true},
			To:   pgtype.Timestamptz{Time: to, Valid: true},
		})
		return e
	})
	if err != nil {
		return nil, err
	}

	unchecked := map[int64][]AlertLine{}
	var totalUnchecked, totalChecked int
	for _, a := range rows {
		if a.CheckStatus == "unchecked" {
			totalUnchecked++
			if a.VehicleID != nil {
				unchecked[*a.VehicleID] = append(unchecked[*a.VehicleID], AlertLine{
					ID: a.ID, AlertCode: a.AlertCode, AlertName: a.AlertName,
					Severity: a.Severity, OccurredAt: conv.Time(a.OccurredAt),
				})
			}
		} else {
			totalChecked++
		}
	}

	groups := map[string][]VehicleCard{"moving": {}, "idle": {}, "parking": {}, "offline": {}}
	uncheckedVehicles := 0
	overspeedingNow := 0
	for _, v := range vehicles {
		lines := unchecked[v.VehicleID]
		// Real-time overspeed flag derived from live GPS speed (no stored alert).
		if v.Position != nil && v.Position.Speed != nil && *v.Position.Speed > overspeedLimit {
			overspeedingNow++
			name := fmt.Sprintf("Overspeed — %d km/h", int(*v.Position.Speed))
			code, sev := "OVERSPEED", "urgent"
			at := time.Now()
			lines = append([]AlertLine{{ID: -v.VehicleID, AlertCode: &code, AlertName: &name, Severity: &sev, OccurredAt: &at}}, lines...)
		}
		card := VehicleCard{
			VehicleID: v.VehicleID, LicensePlate: v.LicensePlate, DriveState: v.DriveState,
			UncheckedN: len(lines),
		}
		if len(lines) > 0 {
			uncheckedVehicles++
			// worst severity
			worst := ""
			for _, l := range lines {
				sv := ""
				if l.Severity != nil {
					sv = *l.Severity
				}
				if sevRank[sv] > sevRank[worst] {
					worst = sv
				}
			}
			card.WorstSeverity = worst
			if len(lines) > maxLinesPerCard {
				card.Alerts = lines[:maxLinesPerCard]
				card.MoreCount = len(lines) - maxLinesPerCard
			} else {
				card.Alerts = lines
			}
		}
		groups[v.DriveState] = append(groups[v.DriveState], card)
	}
	// Stable order: vehicles with alerts first, then by plate.
	for k := range groups {
		g := groups[k]
		sort.SliceStable(g, func(i, j int) bool {
			if (g[i].UncheckedN > 0) != (g[j].UncheckedN > 0) {
				return g[i].UncheckedN > 0
			}
			return g[i].LicensePlate < g[j].LicensePlate
		})
		groups[k] = g
	}

	return &Response{
		Summary: Summary{UncheckedVehicles: uncheckedVehicles, UncheckedAlerts: totalUnchecked, CheckedAlerts: totalChecked, OverspeedingNow: overspeedingNow},
		Groups:  groups,
	}, nil
}

// Handler exposes the control-tower endpoint.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the control-tower endpoint.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/control-tower", h.get)
}

// get godoc
//
//	@Summary	Control Tower: vehicles grouped by drive state with unresolved alerts
//	@Tags		control-tower
//	@Security	BearerAuth
//	@Produce	json
//	@Param		from	query		string	false	"RFC3339 window start (default 24h ago)"
//	@Param		to		query		string	false	"RFC3339 window end (default now)"
//	@Success	200	{object}	controltower.Response
//	@Router		/v1/control-tower [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	resp, err := h.svc.Get(r.Context(), auth.TenantID(r.Context()), from, to)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
