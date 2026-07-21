package insight

import (
	"fmt"
	"math"
	"net/http"
	"sort"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Behaviour-change detection: each vehicle compared against ITS OWN recent past.
//
// Comparing vehicles to each other (peers.go) finds outliers; comparing a
// vehicle to itself finds CHANGES, which is usually the more actionable of the
// two. A truck that has always idled heavily is a known quantity; the same
// truck idling twice as much as last week is a new problem, and nobody spots it
// in a table of totals.
//
// WHY THIS COMPARES SPREAD, NOT JUST AVERAGES. The first version of this file
// flagged any vehicle whose 3-day mean differed from its baseline mean by more
// than 40%, and on real data it reported eight vehicles as "up 4x" while the
// fleet as a whole was flat. Every one was a false alarm. A typical flagged
// truck had these daily distances:
//
//	85 · 41 · 233 · 62 · 26 · 28 · 409 · 21 · 462 km
//
// It alternates short local runs with 400 km hauls. Its "mean" is a number no
// individual day resembles, so whether the recent window looks high or low is
// decided by which kind of day happened to land in it. The arithmetic was right
// and the finding was worthless.
//
// So a change is reported only when it is large relative to HOW MUCH THAT
// VEHICLE NORMALLY VARIES — a z-score against its own daily spread. For the
// truck above, a jump to 297 km/day sits well inside its normal scatter and
// stays silent; for a truck that reliably does 80 km every day, the same jump
// is enormous and gets reported. This is the whole difference between a widget
// people trust and one they learn to ignore.

// Anomaly is one vehicle behaving unlike itself.
type Anomaly struct {
	VehicleID    int64    `json:"vehicle_id"`
	LicensePlate string   `json:"license_plate"`
	FleetGroup   string   `json:"fleet_group"`
	Severity     Severity `json:"severity"`
	// Metric is which measure moved: idle_pct | distance | moving_hours | stopped.
	Metric string `json:"metric"`
	// Headline is a ready-to-display sentence, built server-side so every client
	// and the AI narrator describe a finding identically.
	Headline  string  `json:"headline"`
	Recent    float64 `json:"recent"`
	Baseline  float64 `json:"baseline"`
	ChangePct float64 `json:"change_pct"`
	// Sigma is how many standard deviations of this vehicle's OWN daily spread
	// the change represents. Exposed because it is the number that decides
	// whether a finding is real, and hiding it would make the ranking look
	// arbitrary beside the more eye-catching percentage.
	Sigma float64 `json:"sigma"`
	Unit  string  `json:"unit"`
}

// AnomalyResponse wraps findings with how much history backed them.
//
// An envelope rather than a bare array so an empty result can explain itself:
// "nothing changed" and "not enough history to tell" are entirely different
// messages, and a client receiving [] cannot distinguish them.
type AnomalyResponse struct {
	Items        []Anomaly `json:"items"`
	BaselineDays int       `json:"baseline_days"`
	Note         string    `json:"note,omitempty"`
}

// Significance settings.
//
// sigmaThreshold at 2.0 is the usual "outside normal variation" bar: on a
// roughly bell-shaped spread it fires on about 5% of ordinary fluctuations, so
// a 1,900-vehicle fleet still yields a readable handful rather than a wall.
//
// The minimum absolute movements exist because a z-score alone misfires on
// vehicles that barely vary: a truck doing exactly 40 km every day has almost
// no deviation, so 44 km is "10 sigma" and technically extreme. Nobody cares
// about a 4 km change. A finding must clear BOTH bars.
const (
	sigmaThreshold = 2.0
	minKmMove      = 40.0 // km/day
	minHoursMove   = 1.5  // h/day
	minIdlePoints  = 12.0 // percentage points
)

// anomalies godoc
//
//	@Summary		Vehicles behaving differently from their own normal
//	@Description	Compares each vehicle's last 3 complete days against its own prior daily history, reporting a change only when it exceeds that vehicle's normal day-to-day variation (z-score >= 2) AND moves by a materially useful amount. Today is excluded because it is still being written. At most one finding per vehicle, each with a ready-made headline.
//	@Tags			insights
//	@Security		BearerAuth
//	@Produce		json
//	@Param			limit	query		int	false	"max findings (default 20)"
//	@Success		200		{object}	insight.AnomalyResponse
//	@Failure		503		{object}	map[string]string
//	@Router			/v1/insights/anomalies [get]
func (h *Handler) anomalies(w http.ResponseWriter, r *http.Request) {
	if !h.requireLive(w) {
		return
	}
	tenantID := auth.TenantID(r.Context())
	limit := intParam(r, "limit", 20, 1, 200)

	series, err := h.live.FleetDailySeries(r.Context(), tenantID, totalWindow+1)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "fleet history unavailable: "+err.Error())
		return
	}

	// Which days are usable at all. The rollup's own first days carry one or two
	// devices rather than nineteen hundred — that is the collector warming up,
	// not a quiet fleet — and averaging them in drags every baseline toward
	// zero, which is what makes a flat fleet look like it is surging.
	usable := usableDays(series)
	if len(usable) < minBaselineDays+recentWindow {
		httpx.JSON(w, http.StatusOK, AnomalyResponse{
			Items:        []Anomaly{},
			BaselineDays: len(usable),
			Note: fmt.Sprintf(
				"Only %d days of complete fleet history are available; behaviour comparisons need at least %d. This fills in as the daily rollup accumulates.",
				len(usable), minBaselineDays+recentWindow),
		})
		return
	}
	recentSet := map[string]bool{}
	for _, d := range usable[len(usable)-recentWindow:] {
		recentSet[d] = true
	}
	usableSet := map[string]bool{}
	for _, d := range usable {
		usableSet[d] = true
	}

	// At most ONE finding per vehicle: distance and driving hours are nearly the
	// same signal, so reporting both turns six findings about six vehicles into
	// six findings about three.
	best := map[int64]Anomaly{}
	keep := func(a Anomaly) {
		if cur, seen := best[a.VehicleID]; !seen || anomalyWeight(a) > anomalyWeight(cur) {
			best[a.VehicleID] = a
		}
	}

	for _, v := range series {
		var baseKm, baseMoving, baseIdlePts []float64
		var recKm, recMoving, recIdlePts []float64

		for _, d := range v.Days {
			if !usableSet[d.Day] {
				continue
			}
			// Days the vehicle did not move are excluded from RATE statistics —
			// a parked Sunday is not a slow driving day — but their absence is
			// what makes "stopped entirely" detectable below.
			if d.MovingH <= 0 {
				continue
			}
			idlePts := idleShare(d.MovingH, d.IdleH)
			if recentSet[d.Day] {
				recKm = append(recKm, d.DistanceKm)
				recMoving = append(recMoving, d.MovingH)
				recIdlePts = append(recIdlePts, idlePts)
			} else {
				baseKm = append(baseKm, d.DistanceKm)
				baseMoving = append(baseMoving, d.MovingH)
				baseIdlePts = append(baseIdlePts, idlePts)
			}
		}

		if len(baseKm) < minBaselineDays {
			continue
		}
		plate := v.LicensePlate
		group := strOr(v.FleetGroup, "—")

		// Worked steadily, now stopped entirely. Reported alone, because every
		// rate comparison below is degenerate with an empty recent side.
		if len(recKm) == 0 {
			keep(Anomaly{
				VehicleID: v.VehicleID, LicensePlate: plate, FleetGroup: group,
				Severity: SevWarning, Metric: "stopped",
				Headline: fmt.Sprintf("%s has not moved in %d days, after working %d of the previous %d",
					plate, recentWindow, len(baseKm), len(usable)-recentWindow),
				Recent: 0, Baseline: round1(mean(baseKm)), ChangePct: -100, Sigma: 0, Unit: "km/day",
			})
			continue
		}

		if a, ok := test(v, "distance", "km/day", recKm, baseKm, minKmMove); ok {
			a.FleetGroup = group
			a.Headline = fmt.Sprintf("%s is covering %.0f km/day, %s from a usual %.0f",
				plate, a.Recent, direction(a.ChangePct), a.Baseline)
			keep(a)
		}
		if a, ok := test(v, "moving_hours", "hours/day", recMoving, baseMoving, minHoursMove); ok {
			a.FleetGroup = group
			a.Headline = fmt.Sprintf("%s is driving %.1f h/day, %s from a usual %.1f",
				plate, a.Recent, direction(a.ChangePct), a.Baseline)
			keep(a)
		}
		// Idle is only interesting when it RISES: a truck wasting less fuel than
		// usual is not something anyone needs to be told.
		if a, ok := test(v, "idle_pct", "percentage points", recIdlePts, baseIdlePts, minIdlePoints); ok && a.Recent > a.Baseline {
			a.FleetGroup = group
			a.Headline = fmt.Sprintf("%s is idling %.0f%% of engine-on time, up from a usual %.0f%%",
				plate, a.Recent, a.Baseline)
			keep(a)
		}
	}

	out := make([]Anomaly, 0, len(best))
	for _, a := range best {
		out = append(out, a)
	}
	// Most significant first, so a truncated list keeps what matters.
	sort.Slice(out, func(i, j int) bool {
		if severityRank[out[i].Severity] != severityRank[out[j].Severity] {
			return severityRank[out[i].Severity] > severityRank[out[j].Severity]
		}
		return out[i].Sigma > out[j].Sigma
	})
	if len(out) > limit {
		out = out[:limit]
	}
	httpx.JSON(w, http.StatusOK, AnomalyResponse{Items: out, BaselineDays: len(usable)})
}

// test decides whether a measure moved enough to be worth reporting.
//
// Both bars must be cleared: the move must be large relative to this vehicle's
// own daily spread (sigma), and large in absolute terms. Sigma alone flags a
// 4 km change on a metronomic vehicle; an absolute threshold alone flags every
// long-haul truck every week.
func test(v liveview.VehicleSeries, metric, unit string, recent, base []float64, minMove float64) (Anomaly, bool) {
	if len(recent) == 0 || len(base) < minBaselineDays {
		return Anomaly{}, false
	}
	recMean, baseMean := mean(recent), mean(base)
	move := math.Abs(recMean - baseMean)
	if move < minMove {
		return Anomaly{}, false
	}
	sd := stddev(base)
	// A vehicle with almost no spread would divide by ~zero. Floor the deviation
	// at a tenth of its own mean, so such a vehicle needs a visibly large move
	// rather than becoming infinitely sensitive.
	if floor := baseMean * 0.1; sd < floor {
		sd = floor
	}
	if sd <= 0 {
		return Anomaly{}, false
	}
	sigma := move / sd
	if sigma < sigmaThreshold {
		return Anomaly{}, false
	}
	change, _ := pct(recMean, baseMean)
	return Anomaly{
		VehicleID: v.VehicleID, LicensePlate: v.LicensePlate,
		Severity:  sevFor(sigma, sigmaThreshold*1.5),
		Metric:    metric,
		Recent:    round1(recMean),
		Baseline:  round1(baseMean),
		ChangePct: round1(change),
		Sigma:     round1(sigma),
		Unit:      unit,
	}, true
}

// usableDays returns the sorted days on which the fleet actually reported.
//
// A day counts when at least half the median day's vehicles appear in it, which
// excludes both the rollup's warm-up days and any day an upstream outage
// emptied. Today is dropped outright: it is still being written, so every
// measure taken from it is a fraction of its final value — and it sits on the
// recent side of every comparison, where that error does the most damage.
func usableDays(series []liveview.VehicleSeries) []string {
	count := map[string]int{}
	for _, v := range series {
		for _, d := range v.Days {
			if d.MovingH > 0 || d.IdleH > 0 || d.DistanceKm > 0 {
				count[d.Day]++
			}
		}
	}
	if len(count) == 0 {
		return nil
	}
	days := make([]string, 0, len(count))
	vals := make([]float64, 0, len(count))
	for d, n := range count {
		days = append(days, d)
		vals = append(vals, float64(n))
	}
	sort.Strings(days)
	med := median(vals)
	// Drop the newest day (today, still partial) before filtering.
	days = days[:len(days)-1]

	out := make([]string, 0, len(days))
	for _, d := range days {
		if float64(count[d]) >= med/2 {
			out = append(out, d)
		}
	}
	return out
}

func direction(changePct float64) string {
	if changePct < 0 {
		return "down"
	}
	return "up"
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// stddev is the sample standard deviation (n-1), the right estimator when the
// days in hand are a sample of the vehicle's behaviour rather than all of it.
func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	var sum float64
	for _, x := range xs {
		d := x - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(xs)-1))
}

// idleShare is idle as a fraction of engine-on time, in percent. Matches the
// definition the rollup reports fleet-wide, so the two mean the same thing.
func idleShare(movingH, idleH float64) float64 {
	on := movingH + idleH
	if on <= 0.01 {
		return 0
	}
	return idleH / on * 100
}

// anomalyWeight decides which single finding best describes a vehicle.
//
// A vehicle that stopped entirely outranks any rate change, and rising idle
// outranks a change in distance: idle is waste someone can act on, whereas
// distance moving is often just the schedule doing its job.
func anomalyWeight(a Anomaly) float64 {
	switch a.Metric {
	case "stopped":
		return 10000
	case "idle_pct":
		return 5000 + a.Sigma
	default:
		return a.Sigma
	}
}

// sevFor escalates a finding once it is well past the reporting bar.
func sevFor(magnitude, criticalAt float64) Severity {
	if magnitude >= criticalAt {
		return SevCritical
	}
	return SevWarning
}
