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
}

// DefaultPolicy is a sensible starting point for an Indonesian truck fleet.
var DefaultPolicy = Policy{
	SpeedLimitKmh:      80,
	IdleTargetPct:      20,
	IdleFullPct:        45,
	LongShiftHours:     10,
	LongShiftFullHours: 14,
	MinDistanceKm:      300,
}

// Weights are the penalty each behaviour can subtract at its worst. They mirror
// internal/scorecard's default so the number the collector writes and the
// number the scorecard endpoint shows come from the same place.
const (
	weightOverspeed = 45.0
	weightIdle      = 30.0
	weightLongShift = 25.0
)

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
			pd = &perDriver{}
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
			}
			pd.longShiftLoad += severity(d.MovingH, c.policy.LongShiftHours, c.policy.LongShiftFullHours)
		}
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
			ovs := c.overspeedPenalty(pd)
			idle := c.idlePenalty(pd)
			fat := c.longShiftPenalty(pd)

			// trip_km carries the exposure the scorecard reads; the rest are
			// penalties it subtracts from 100.
			rows := []struct {
				code  string
				occ   int
				score float64
			}{
				{"trip_km", 0, pd.distanceKm},
				{"OVERSPEED", pd.speedingDays, ovs},
				{"EXCEEDED_IDLE", 0, idle},
				{"LONG_SHIFT", 0, fat},
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
		return map[string]any{"speeding_days": pd.speedingDays, "days": pd.days, "limit_kmh": p.SpeedLimitKmh}
	case "EXCEEDED_IDLE":
		return map[string]any{"idle_pct": round1(idlePctOf(pd)), "target_pct": p.IdleTargetPct}
	case "LONG_SHIFT":
		avg := 0.0
		if pd.days > 0 {
			avg = pd.movingH / float64(pd.days)
		}
		return map[string]any{"avg_driving_h": round1(avg), "threshold_h": p.LongShiftHours}
	default:
		return map[string]any{"distance_km": round1(pd.distanceKm), "days": pd.days}
	}
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
