// Package insight turns fleet data into findings.
//
// WHY THIS EXISTS. Every widget on the dashboard answers "how much?" — idle
// hours, distance, alert counts. None answers "compared to what?", which is the
// difference between data and a finding. "340 idle hours" is a number; "this
// truck idles three times more than it did last week" is something a person can
// act on. That comparison is all this package adds, in three flavours:
//
//	against its own past  -> anomaly.go   (something changed)
//	against its peers     -> peers.go     (something is unusual)
//	against a threshold   -> queue.go     (something needs doing)
//
// WHERE THE DATA COMES FROM. Deliberately no new storage. The daily rollup is
// already served by liveview.FleetDaily, live state by liveview.List, and the
// rest — alerts, documents, service schedules — is local and RLS-scoped. This
// package only joins and compares what exists.
package insight

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Severity orders a finding's urgency. Kept as strings rather than an int so the
// client can colour by name without a lookup table.
type Severity string

const (
	SevCritical Severity = "critical"
	SevWarning  Severity = "warning"
	SevInfo     Severity = "info"
)

// severityRank orders findings for the client. Higher sorts first.
var severityRank = map[Severity]int{SevCritical: 3, SevWarning: 2, SevInfo: 1}

// Handler serves the insight endpoints.
type Handler struct {
	pool *pgxpool.Pool
	live *liveview.Service
}

// NewHandler builds the handler. liveview supplies both the daily rollup and
// current vehicle state; without it the endpoints that need history return an
// explanatory error rather than failing the whole deployment.
func NewHandler(pool *pgxpool.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

// Routes mounts the insight endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/insights/action-queue", h.actionQueue)
	r.Get("/v1/insights/anomalies", h.anomalies)
	r.Get("/v1/insights/utilisation-peers", h.utilisationPeers)
	r.Get("/v1/insights/battery-watch", h.batteryWatch)
	r.Post("/v1/insights/battery-watch/{vehicle_id}/schedule-service", h.scheduleBatteryService)
}

// requireLive guards the endpoints that need the telemetry rollup.
func (h *Handler) requireLive(w http.ResponseWriter) bool {
	if h.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"fleet history is not available on this deployment")
		return false
	}
	return true
}

// baselineWindow is how many days of history a finding is measured against, and
// recentWindow is what counts as "lately".
//
// Three days is short enough to catch a change while it still matters and long
// enough that one unusual day does not trigger it — a truck off the road on a
// single Tuesday is not a trend. The 17-day outer window is bounded by what the
// rollup keeps; the baseline is therefore the fourteen days before the recent
// three.
const (
	recentWindow   = 3
	baselineWindow = 14
	totalWindow    = recentWindow + baselineWindow
)

// minBaselineDays is how many days a vehicle must actually have reported in the
// baseline period before a comparison is drawn against it.
//
// Without this, a vehicle that reported twice a fortnight ago produces a
// baseline of two days and then "changes" by 400% — arithmetically true and
// completely meaningless. Findings built on almost no history are worse than no
// findings, because they are indistinguishable from real ones.
const minBaselineDays = 5

// fleetDaily fetches the rollup for a window, tolerating a shorter one.
func (h *Handler) fleetDaily(ctx context.Context, tenantID int64, days int) (*liveview.FleetHistory, error) {
	return h.live.FleetDaily(ctx, tenantID, days)
}

// pct returns the relative change from base to now, guarding division by zero.
// Returns ok=false when there is no meaningful base to compare against.
func pct(now, base float64) (float64, bool) {
	if base <= 0.01 {
		return 0, false
	}
	return (now - base) / base * 100, true
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

func strOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}
