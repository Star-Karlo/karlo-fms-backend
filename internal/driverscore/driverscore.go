// Package driverscore turns the daily telemetry rollup into monthly driver
// safety scores, and keeps them current as days complete.
//
// WHAT THE SCORE IS BUILT FROM, and why only these three things.
//
// The trackers report a position roughly every 90 seconds while a vehicle
// moves. That interval decides what can honestly be measured:
//
//   - SPEED is a state that persists between fixes, so "was over the limit"
//     is measurable. A daily peak speed over the fleet policy is a real event.
//   - IDLING is a duration the rollup already sums, so idle-as-a-share-of-
//     engine-on is exact.
//   - HOURS DRIVEN per day is a duration too, so an over-long shift — a
//     fatigue proxy — is measurable.
//
// Harsh braking, harsh acceleration and harsh cornering are NOT measurable
// from this feed and are deliberately left out. A hard brake happens over two
// or three seconds; at 90-second sampling it is invisible, and "60 then 20 a
// minute and a half later" is indistinguishable from a gentle slowdown at a
// junction. Deriving harsh events from that would invent numbers and punish
// drivers for traffic lights. Those behaviours score properly only once the
// devices are configured to emit them natively, which is a tracker-config
// change, not a limitation of this code — the weights carry zero-weighted slots
// for them so enabling that is a config edit.
//
// FAIRNESS RULES, both load-bearing:
//
//  1. EXPOSURE-NORMALISED. A driver doing 400 km a day will always rack up more
//     speeding minutes than one doing 50, so every penalty is a RATE — a share
//     of that driver's own days or driving time — never a raw count. A driver
//     below MinDistanceKm for the month is not scored at all; there is too
//     little to judge.
//
//  2. FORWARD-ONLY ATTRIBUTION. current_driver_id is a snapshot of who is on a
//     truck now. Charging last month's telemetry to today's driver would blame
//     the wrong person, so the collector attributes each day to whoever is
//     assigned AT COLLECTION TIME and history accrues from the day it first
//     runs — it does not back-fill.
package driverscore

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/db"
)

// Policy holds the thresholds the score is judged against. Fleet-wide for now;
// a per-vehicle or per-road limit would replace speedLimitKmh here.
type Policy struct {
	// SpeedLimitKmh is the peak-speed line. A day whose maximum exceeds it
	// counts as a speeding day. 80 is a common heavy-vehicle policy limit.
	SpeedLimitKmh float64
	// IdleTargetPct is the idle share treated as acceptable; penalty scales from
	// there up to IdleFullPct.
	IdleTargetPct float64
	IdleFullPct   float64
	// LongShiftHours is when a day's driving starts counting as fatigue, and
	// LongShiftFullHours is where that penalty saturates.
	LongShiftHours     float64
	LongShiftFullHours float64
	// MinDistanceKm gates scoring: below this over the month, not enough to judge.
	MinDistanceKm float64
	// HarshPer100kmFull is the harsh-event rate — weighted severity points per
	// 100 km, where yellow=1, red=2, black=4 — at which a harsh pillar's penalty
	// saturates. A starting value; tune it once real event volume is known.
	HarshPer100kmFull float64
}

// DefaultPolicy is a sensible starting point for an Indonesian truck fleet.
var DefaultPolicy = Policy{
	SpeedLimitKmh:      80,
	IdleTargetPct:      20,
	IdleFullPct:        45,
	LongShiftHours:     10,
	LongShiftFullHours: 14,
	MinDistanceKm:      300,
	HarshPer100kmFull:  8,
}

// Weights are the penalty each behaviour can subtract at its worst. They mirror
// internal/scorecard's default so the number the collector writes and the
// number the scorecard endpoint shows come from the same place.
const (
	weightOverspeed = 45.0
	weightIdle      = 30.0
	weightLongShift = 25.0
	// Harsh-driving pillars, fed by the MDVR G-sensor (driving_events). These
	// are the slots this package reserved: braking / acceleration / cornering.
	// Additive on top of the GPS pillars and the final score floors at 0, so a
	// clean month (no events) changes nothing. Once real event volume is known,
	// promote all six weights into Policy and rebalance to a 100-point budget.
	weightHarshBrake  = 15.0
	weightHarshAccel  = 12.0
	weightHarshCorner = 13.0
)

// 5-pillar driving-behaviour score (replaces the blended model). Each pillar is
// worth pillarBudget points; the penalty is the driver's exposure-normalised
// weighted-event rate (severity points per 100 km, yellow=1/red=2/black=4)
// saturating at these full-scale rates. Starting values — tune once real event
// volume is known. Stopping/Turning/Steering are rarer, so they saturate lower.
const (
	pillarBudget   = 20.0
	fullBraking    = 8.0
	fullStopping   = 4.0
	fullTurning    = 6.0
	fullSteering   = 6.0
	fullSmoothness = 12.0
)

// pillarPenalty scales a pillar (0..pillarBudget) by the driver's own exposure:
// weighted severity points per 100 km, saturating at full.
func (c *Collector) pillarPenalty(pd *perDriver, code string, full float64) float64 {
	if pd.distanceKm <= 0 || full <= 0 {
		return 0
	}
	rate := pd.pillar[code] / (pd.distanceKm / 100.0)
	p := pillarBudget * rate / full
	if p > pillarBudget {
		p = pillarBudget
	}
	return p
}

// Collector recomputes scores on a schedule.
type Collector struct {
	pool   *db.Pool
	live   *liveview.Service
	policy Policy
}

func New(pool *db.Pool, live *liveview.Service, p Policy) *Collector {
	return &Collector{pool: pool, live: live, policy: p}
}

// Run recomputes at startup and then daily. Scores lag the fleet by at most a
// day, which is right for a monthly measure — nobody grades a driver on this
// morning.
func (c *Collector) Run(ctx context.Context) {
	if c.live == nil {
		log.Printf("driverscore: fleet history unavailable; scoring disabled")
		return
	}
	c.runOnce(ctx)
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.runOnce(ctx)
		}
	}
}

// RunTenantNow recomputes one tenant's current month on demand.
//
// Wired to the "Recalculate scores" button. Exists because the nightly cadence
// is right for a settled month but wrong right after a change — assigning
// drivers or fixing the registry should show in the scores now, not tomorrow.
// It is the whole recompute, not a delta, so it is safe to press repeatedly.
func (c *Collector) RunTenantNow(ctx context.Context, tenantID int64) error {
	if c.live == nil {
		return fmt.Errorf("fleet history is unavailable on this deployment")
	}
	return c.scoreTenant(ctx, tenantID)
}

func (c *Collector) runOnce(ctx context.Context) {
	var tenants []int64
	if err := db.SystemTx(ctx, c.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM tenants`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			tenants = append(tenants, id)
		}
		return rows.Err()
	}); err != nil {
		log.Printf("driverscore: list tenants: %v", err)
		return
	}
	for _, tid := range tenants {
		if err := c.scoreTenant(ctx, tid); err != nil {
			log.Printf("driverscore: tenant %d: %v", tid, err)
		}
	}
}

// perDriver accumulates one driver's month while scanning vehicle-days.
type perDriver struct {
	days          int
	speedingDays  int
	longShiftLoad float64 // summed per-day fatigue severity [0,1]
	movingH       float64
	idleH         float64
	distanceKm    float64
	// The specific days each issue occurred, so the detail view can answer
	// "when" rather than only "how many". Kept small — a month is at most 31
	// entries — and carried into the event metadata.
	speedingDates  []string
	longShiftDates []string
	// Harsh G-sensor events this month: kind ("braking"|"acceleration"|
	// "cornering") -> severity ("yellow"|"red"|"black") -> count.
	harsh map[string]map[string]int
	// 5-pillar accumulators: pillar code -> weighted severity points / raw count.
	pillar  map[string]float64
	pillarN map[string]int
}

func newPerDriver() *perDriver {
	return &perDriver{harsh: map[string]map[string]int{}, pillar: map[string]float64{}, pillarN: map[string]int{}}
}

// severityPoints weights a harsh event by how dangerous it was, so one black
// (>0.8 g) event counts as much as four yellow ones.
func severityPoints(sev string) float64 {
	switch sev {
	case "black":
		return 4
	case "red":
		return 2
	default:
		return 1 // yellow, or anything unclassified
	}
}

// harshPoints totals a pillar's weighted severity points and raw event count.
func (pd *perDriver) harshPoints(kind string) (points float64, count int) {
	for sev, n := range pd.harsh[kind] {
		points += severityPoints(sev) * float64(n)
		count += n
	}
	return points, count
}

// scoreTenant recomputes the CURRENT month for one tenant.
//
// The window is the whole month so far, so a day completing simply lifts the
// score's denominator by one — "keep calculating" falls out of recomputing the
// current period each night rather than appending deltas.
func (c *Collector) scoreTenant(ctx context.Context, tenantID int64) error {
	now := time.Now()
	period := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	// Days elapsed this month, capped at the rollup's reach.
	daysThisMonth := now.Day()
	if daysThisMonth > 35 {
		daysThisMonth = 35
	}

	series, err := c.live.FleetDailySeries(ctx, tenantID, daysThisMonth)
	if err != nil {
		return fmt.Errorf("daily series: %w", err)
	}

	// Which driver is on which vehicle right now. Snapshot, by design.
	driverOf := map[int64]int64{}
	if err := db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, current_driver_id FROM vehicles WHERE current_driver_id IS NOT NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var vid, did int64
			if err := rows.Scan(&vid, &did); err != nil {
				return err
			}
			driverOf[vid] = did
		}
		return rows.Err()
	}); err != nil {
		return fmt.Errorf("driver assignments: %w", err)
	}

	monthPrefix := period.Format("2006-01")
	acc := map[int64]*perDriver{}
	for _, v := range series {
		did, ok := driverOf[v.VehicleID]
		if !ok {
			continue // unassigned vehicle: nobody to attribute it to
		}
		pd := acc[did]
		if pd == nil {
			pd = newPerDriver()
			acc[did] = pd
		}
		for _, d := range v.Days {
			// Only days inside the current month, and only days the vehicle
			// actually moved — a parked day is not a driving day and would
			// dilute every rate toward zero.
			if !strings.HasPrefix(d.Day, monthPrefix) || d.MovingH <= 0 {
				continue
			}
			pd.days++
			pd.movingH += d.MovingH
			pd.idleH += d.IdleH
			pd.distanceKm += d.DistanceKm
			if d.MaxSpeed > c.policy.SpeedLimitKmh {
				pd.speedingDays++
				pd.speedingDates = append(pd.speedingDates, d.Day)
			}
			if sev := severity(d.MovingH, c.policy.LongShiftHours, c.policy.LongShiftFullHours); sev > 0 {
				pd.longShiftLoad += sev
				pd.longShiftDates = append(pd.longShiftDates, d.Day)
			}
		}
	}

	// Fold in harsh G-sensor events for the same month, attributed to the CURRENT
	// driver of each vehicle — the same forward-only rule the GPS pillars use, so
	// both pillars land on the same person. A vehicle with events but no moving
	// days is not in acc and is skipped (that driver is below the distance gate).
	nextMonth := period.AddDate(0, 1, 0)
	if err := db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		// Bucket each harsh kind by speed so the 5 pillars can be separated:
		// Stopping = braking at ~0 km/h (abrupt stop), Turning = cornering at
		// speed (rollover risk), Steering = cornering at low speed (swerve).
		rows, err := tx.Query(ctx, `
SELECT vehicle_id, kind, severity,
       count(*)                                             AS n_all,
       count(*) FILTER (WHERE coalesce(speed_kph,0) < 5)    AS n_slow,
       count(*) FILTER (WHERE coalesce(speed_kph,0) >= 30)  AS n_fast
FROM driving_events
WHERE occurred_at >= $1 AND occurred_at < $2 AND vehicle_id IS NOT NULL
GROUP BY vehicle_id, kind, severity`, period, nextMonth)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var vid int64
			var kind, sev string
			var nAll, nSlow, nFast int
			if err := rows.Scan(&vid, &kind, &sev, &nAll, &nSlow, &nFast); err != nil {
				return err
			}
			did, ok := driverOf[vid]
			if !ok {
				continue
			}
			pd := acc[did]
			if pd == nil {
				continue
			}
			pts := severityPoints(sev)
			add := func(code string, n int) {
				pd.pillar[code] += pts * float64(n)
				pd.pillarN[code] += n
			}
			switch kind {
			case "braking":
				add("BRAKING", nAll)   // all harsh braking
				add("STOPPING", nSlow) // braking at ~0 km/h = abrupt stop
				add("SMOOTHNESS", nAll)
			case "acceleration":
				add("SMOOTHNESS", nAll) // jerky pedal use
			case "cornering":
				add("TURNING", nFast)          // high-speed corner (rollover risk)
				add("STEERING", nAll-nFast)    // low-speed corner / swerve (approx)
			case "road_shock":
				// vertical shock (Z) is not one of the 5 pillars; it is shown in
				// the harsh-driving history, not scored here.
			}
		}
		return rows.Err()
	}); err != nil {
		// Non-fatal: a driver with no readable events simply scores clean.
		log.Printf("driverscore: tenant %d harsh events: %v", tenantID, err)
	}

	// Rewrite the period's events atomically: delete then insert, so a driver
	// who stopped speeding this month loses the penalty rather than keeping a
	// stale one, and a re-run is idempotent.
	return db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM driver_score_events WHERE tenant_id = $1 AND period = $2`,
			tenantID, period); err != nil {
			return err
		}
		for did, pd := range acc {
			if pd.distanceKm < c.policy.MinDistanceKm {
				continue // too little driving to judge fairly
			}
			// 5-pillar driving-behaviour score (each pillar worth 20). trip_km
			// carries the exposure the scorecard reads; the five pillar rows are
			// always written (0 when clean) so the scorecard shows every pillar.
			rows := []struct {
				code  string
				occ   int
				score float64
			}{
				{"trip_km", 0, pd.distanceKm},
				{"BRAKING", pd.pillarN["BRAKING"], c.pillarPenalty(pd, "BRAKING", fullBraking)},
				{"STOPPING", pd.pillarN["STOPPING"], c.pillarPenalty(pd, "STOPPING", fullStopping)},
				{"TURNING", pd.pillarN["TURNING"], c.pillarPenalty(pd, "TURNING", fullTurning)},
				{"STEERING", pd.pillarN["STEERING"], c.pillarPenalty(pd, "STEERING", fullSteering)},
				{"SMOOTHNESS", pd.pillarN["SMOOTHNESS"], c.pillarPenalty(pd, "SMOOTHNESS", fullSmoothness)},
			}
			for _, r := range rows {
				if _, err := tx.Exec(ctx, `
INSERT INTO driver_score_events (tenant_id, driver_id, event_type, occurrences, score, period, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
					tenantID, did, r.code, r.occ, round1(r.score), period,
					metadataFor(r.code, pd, c.policy)); err != nil {
					return err
				}
			}
		}
		log.Printf("driverscore: tenant %d, %s — scored %d drivers", tenantID, monthPrefix, len(acc))
		return nil
	})
}

// overspeedPenalty scales with the SHARE of driving days spent over the limit.
// Every day over = the full weight; never over = zero.
func (c *Collector) overspeedPenalty(pd *perDriver) float64 {
	if pd.days == 0 {
		return 0
	}
	return weightOverspeed * float64(pd.speedingDays) / float64(pd.days)
}

// idlePenalty scales with how far the month's idle share sits above target.
func (c *Collector) idlePenalty(pd *perDriver) float64 {
	on := pd.movingH + pd.idleH
	if on <= 0 {
		return 0
	}
	idlePct := 100 * pd.idleH / on
	return weightIdle * severity(idlePct, c.policy.IdleTargetPct, c.policy.IdleFullPct)
}

// longShiftPenalty is the average per-day fatigue severity times the weight.
func (c *Collector) longShiftPenalty(pd *perDriver) float64 {
	if pd.days == 0 {
		return 0
	}
	return weightLongShift * pd.longShiftLoad / float64(pd.days)
}

// harshPenalty scales a harsh pillar with the driver's own exposure: weighted
// severity points per 100 km, saturating at HarshPer100kmFull. Rate-normalised
// like every other penalty, so a long-haul driver is not punished for driving
// more — only for a higher rate of harsh events.
func (c *Collector) harshPenalty(pd *perDriver, kind string, weight float64) float64 {
	if pd.distanceKm <= 0 {
		return 0
	}
	points, _ := pd.harshPoints(kind)
	rate := 100 * points / pd.distanceKm
	return weight * severity(rate, 0, c.policy.HarshPer100kmFull)
}

// severity maps a value onto [0,1]: nothing below `start`, full at `full`,
// linear between. Used for idle share and shift length so both read the same.
func severity(v, start, full float64) float64 {
	if full <= start || v <= start {
		return 0
	}
	if v >= full {
		return 1
	}
	return (v - start) / (full - start)
}

func idlePctOf(pd *perDriver) float64 {
	on := pd.movingH + pd.idleH
	if on <= 0 {
		return 0
	}
	return 100 * pd.idleH / on
}

// metadataFor records the numbers behind a penalty, so a driver can be shown
// WHY they lost points rather than just how many.
func metadataFor(code string, pd *perDriver, p Policy) map[string]any {
	switch code {
	case "OVERSPEED":
		return map[string]any{"speeding_days": pd.speedingDays, "days": pd.days,
			"limit_kmh": p.SpeedLimitKmh, "dates": pd.speedingDates}
	case "EXCEEDED_IDLE":
		return map[string]any{"idle_pct": round1(idlePctOf(pd)), "target_pct": p.IdleTargetPct}
	case "LONG_SHIFT":
		avg := 0.0
		if pd.days > 0 {
			avg = pd.movingH / float64(pd.days)
		}
		return map[string]any{"avg_driving_h": round1(avg), "threshold_h": p.LongShiftHours,
			"dates": pd.longShiftDates}
	case "HARSH_BRAKE":
		return harshMeta(pd, "braking")
	case "HARSH_ACCEL":
		return harshMeta(pd, "acceleration")
	case "HARSH_CORNER":
		return harshMeta(pd, "cornering")
	default:
		return map[string]any{"distance_km": round1(pd.distanceKm), "days": pd.days}
	}
}

// harshMeta records the numbers behind a harsh pillar: the by-severity counts
// and the exposure-normalised rate, so the scorecard can explain the penalty.
func harshMeta(pd *perDriver, kind string) map[string]any {
	m := pd.harsh[kind]
	points, count := pd.harshPoints(kind)
	per100 := 0.0
	if pd.distanceKm > 0 {
		per100 = round1(100 * points / pd.distanceKm)
	}
	return map[string]any{
		"events":           count,
		"yellow":           m["yellow"],
		"red":              m["red"],
		"black":            m["black"],
		"points_per_100km": per100,
		"distance_km":      round1(pd.distanceKm),
	}
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
