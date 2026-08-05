package liveview

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Harsh-driving history for a trip, from the accelerometer (driving_events:
// tracker Green Driving / raw-axis, and camera G-sensor). Each event is an
// axis manoeuvre with a peak g; the per-axis summary frames the risk the way
// operations reads it — X = accel/braking (fuel leak), Y = cornering (load
// shift / rollover), Z = vertical shock (suspension / tyre wear).

// G-force risk bands (management-facing), aligned with the scoring thresholds:
//
//	< 0.2 safe · ≥ 0.3 warning · ≥ 0.4 caution · ≥ 0.8 critical
func gBand(g *float64) string {
	if g == nil {
		return "warning"
	}
	switch {
	case *g >= 0.8:
		return "critical"
	case *g >= 0.4:
		return "caution"
	case *g >= 0.3:
		return "warning"
	default:
		return "safe"
	}
}

// axisLabel is the risk framing for each accelerometer axis.
func axisLabel(axis string) string {
	switch axis {
	case "X":
		return "Acceleration & Braking"
	case "Y":
		return "Cornering (lateral)"
	case "Z":
		return "Vertical Shock"
	}
	return axis
}

// DrivingEvent is one harsh manoeuvre.
type DrivingEvent struct {
	Kind       string    `json:"kind"` // braking | acceleration | cornering | road_shock
	Axis       string    `json:"axis"` // X | Y | Z
	Gforce     *float64  `json:"gforce"`
	Severity   string    `json:"severity"` // yellow | red | black (stored)
	Band       string    `json:"band"`     // safe | warning | caution | critical
	OccurredAt time.Time `json:"occurred_at"`
	Lat        *float64  `json:"lat"`
	Lon        *float64  `json:"lon"`
	Speed      *float64  `json:"speed"`
}

// AxisSummary rolls up one axis over the window.
type AxisSummary struct {
	Axis     string   `json:"axis"`
	Label    string   `json:"label"`
	Total    int      `json:"total"`
	Warning  int      `json:"warning"`
	Caution  int      `json:"caution"`
	Critical int      `json:"critical"`
	MaxG     *float64 `json:"max_g"`
}

// DrivingHistory is the trip's harsh-event analysis.
type DrivingHistory struct {
	Total  int            `json:"total"`
	ByAxis []AxisSummary  `json:"by_axis"` // always X, Y, Z (zeroed if none)
	Events []DrivingEvent `json:"events"`
}

// drivingEvents godoc
//
//	@Summary		Harsh-driving events for a trip (accelerometer)
//	@Description	Axis X/Y/Z harsh events (accel/brake, cornering, vertical shock) over the window, each with a peak g and risk band, plus a per-axis summary.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			vehicle_id	path		int		true	"vehicle id"
//	@Param			from		query		string	true	"RFC3339"
//	@Param			to			query		string	true	"RFC3339"
//	@Success		200	{object}	liveview.DrivingHistory
//	@Router			/v1/vehicles/{vehicle_id}/driving-events [get]
func (h *Handler) drivingEvents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}
	out, err := h.svc.DrivingHistory(r.Context(), auth.TenantID(r.Context()), id, from, to)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "driving events failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// DrivingHistory reads harsh-driving events for a vehicle over [from,to].
func (s *Service) DrivingHistory(ctx context.Context, tenantID, vehicleID int64, from, to time.Time) (*DrivingHistory, error) {
	out := &DrivingHistory{Events: []DrivingEvent{}}
	// A fixed X/Y/Z frame so the UI can always render three rows.
	sum := map[string]*AxisSummary{
		"X": {Axis: "X", Label: axisLabel("X")},
		"Y": {Axis: "Y", Label: axisLabel("Y")},
		"Z": {Axis: "Z", Label: axisLabel("Z")},
	}
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT kind, axis, gforce, severity, occurred_at, lat, lon, speed_kph
FROM driving_events
WHERE vehicle_id = $1 AND occurred_at >= $2 AND occurred_at <= $3
ORDER BY occurred_at`, vehicleID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e DrivingEvent
			var axis *string
			if err := rows.Scan(&e.Kind, &axis, &e.Gforce, &e.Severity, &e.OccurredAt, &e.Lat, &e.Lon, &e.Speed); err != nil {
				return err
			}
			if axis != nil {
				e.Axis = *axis
			}
			e.Band = gBand(e.Gforce)
			out.Events = append(out.Events, e)
			out.Total++
			if a, ok := sum[e.Axis]; ok {
				a.Total++
				switch e.Band {
				case "critical":
					a.Critical++
				case "caution":
					a.Caution++
				default:
					a.Warning++
				}
				if e.Gforce != nil && (a.MaxG == nil || *e.Gforce > *a.MaxG) {
					g := *e.Gforce
					a.MaxG = &g
				}
			}
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	out.ByAxis = []AxisSummary{*sum["X"], *sum["Y"], *sum["Z"]}
	return out, nil
}
