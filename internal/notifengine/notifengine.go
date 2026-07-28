// Package notifengine evaluates active notification rules against live telemetry
// for the vehicles each rule is attached to, and persists alerts (notifications).
//
// It runs as a background ticker inside the API process. Only rule types that can
// be derived from the ~60s live feed are evaluated today; OVERSPEED is the first.
// Other alert codes can be attached but stay dormant until an evaluator + data
// source exist (idle/parking need dwell tracking; camera/geofence/fuel need data
// we don't yet ingest).
package notifengine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// cooldownMin suppresses repeat alerts for the same (vehicle, code) so a truck
// that keeps speeding produces one notification per window, not one per tick.
const cooldownMin = 30

// defaultOverspeed is used when an OVERSPEED rule has no explicit threshold.
const defaultOverspeed = 60.0

// Engine periodically evaluates rules and writes alerts.
type Engine struct {
	pool     *db.Pool
	tel      *telemetry.Client
	interval time.Duration
}

// New builds the engine. interval ~= the live poll cadence.
func New(pool *db.Pool, tel *telemetry.Client, interval time.Duration) *Engine {
	return &Engine{pool: pool, tel: tel, interval: interval}
}

// Run ticks until ctx is cancelled. Evaluates once immediately, then on interval.
func (e *Engine) Run(ctx context.Context) {
	log.Printf("notifengine: started (interval=%s, cooldown=%dm)", e.interval, cooldownMin)
	t := time.NewTicker(e.interval)
	defer t.Stop()
	for {
		if err := e.tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("notifengine: tick error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick evaluates every tenant that has active attached rules.
func (e *Engine) tick(ctx context.Context) error {
	var tenantIDs []int64
	if err := db.SystemTx(ctx, e.pool, func(tx pgx.Tx) error {
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
			tenantIDs = append(tenantIDs, id)
		}
		return rows.Err()
	}); err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}

	created := 0
	for _, tid := range tenantIDs {
		n, err := e.evalTenant(ctx, tid)
		if err != nil {
			log.Printf("notifengine: tenant %d: %v", tid, err)
			continue
		}
		created += n
	}
	if created > 0 {
		log.Printf("notifengine: created %d alert(s)", created)
	}
	return nil
}

// evalTenant loads a tenant's active rule targets, fetches live positions for the
// attached vehicles, and writes alerts for violations.
func (e *Engine) evalTenant(ctx context.Context, tenantID int64) (int, error) {
	var targets []store.ListActiveRuleTargetsRow
	if err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		var err error
		targets, err = store.New(tx).ListActiveRuleTargets(ctx)
		return err
	}); err != nil {
		return 0, err
	}
	if len(targets) == 0 {
		return 0, nil
	}

	created := 0

	// Frequency pass — DB-only, no telemetry needed. Camera/behaviour events
	// already land as one alert each (the dashcam poller); a frequency rule fires
	// when enough of them pile up in its window for an attached vehicle.
	for _, t := range targets {
		n, err := e.evalFrequency(ctx, tenantID, t)
		if err != nil {
			log.Printf("notifengine: frequency veh %d code %s: %v", t.VehicleID, t.AlertCode, err)
			continue
		}
		created += n
	}

	// Collect IMEIs of vehicles that carry an OVERSPEED rule, then fetch live.
	imeiSet := map[string]struct{}{}
	for _, t := range targets {
		if t.AlertCode == "OVERSPEED" && t.Imei != nil && *t.Imei != "" {
			imeiSet[*t.Imei] = struct{}{}
		}
	}
	if len(imeiSet) == 0 {
		return created, nil // no OVERSPEED rules; frequency pass already counted
	}
	imeis := make([]string, 0, len(imeiSet))
	for im := range imeiSet {
		imeis = append(imeis, im)
	}
	positions, err := e.tel.LiveMany(ctx, imeis, []string{"lat", "lon", "speed"})
	if err != nil {
		return created, fmt.Errorf("telemetry: %w", err)
	}
	byIMEI := make(map[string]telemetry.Position, len(positions))
	for _, p := range positions {
		byIMEI[p.IMEI] = p
	}

	for _, t := range targets {
		if t.AlertCode != "OVERSPEED" || t.Imei == nil {
			continue // only OVERSPEED is evaluable today
		}
		pos, ok := byIMEI[*t.Imei]
		if !ok || pos.Speed == nil {
			continue
		}
		limit := defaultOverspeed
		if f := conv.NumF(t.Threshold); f != nil && *f > 0 {
			limit = *f
		}
		if *pos.Speed <= limit {
			continue
		}
		ok, err := e.writeAlert(ctx, tenantID, t, *pos.Speed, limit, pos.Lat, pos.Lon)
		if err != nil {
			log.Printf("notifengine: write alert veh %d: %v", t.VehicleID, err)
			continue
		}
		if ok {
			created++
		}
	}
	return created, nil
}

// evalFrequency handles one frequency target: if at least `threshold` events of
// the source code fired for the vehicle within the window, and no rollup has
// fired this window already, it writes one escalation alert.
//
// The rollup uses a distinct code (source + "_FREQ") so it never counts toward
// its own rule, and its cooldown is the window itself — one escalation per
// window, not one per tick.
func (e *Engine) evalFrequency(ctx context.Context, tenantID int64, t store.ListActiveRuleTargetsRow) (int, error) {
	if t.WindowMinutes == nil || *t.WindowMinutes <= 0 {
		return 0, nil // not a frequency rule
	}
	window := *t.WindowMinutes
	thr := conv.NumF(t.Threshold)
	if thr == nil || *thr < 1 {
		return 0, nil // a frequency rule needs a count threshold >= 1
	}
	minCount := int64(*thr)
	rollup := alert.FrequencyCode(t.AlertCode)
	vid := t.VehicleID

	wrote := false
	err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// Count source events in the window.
		n, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{
			TenantID: tenantID, VehicleID: &vid, AlertCode: &t.AlertCode, CooldownMin: window,
		})
		if err != nil {
			return err
		}
		if n < minCount {
			return nil
		}
		// One escalation per window: skip if a rollup already fired inside it.
		recent, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{
			TenantID: tenantID, VehicleID: &vid, AlertCode: &rollup, CooldownMin: window,
		})
		if err != nil {
			return err
		}
		if recent > 0 {
			return nil
		}

		name := fmt.Sprintf("%s ×%d in %dm", alert.LabelFor(t.AlertCode), n, window)
		if t.NotificationName != nil && *t.NotificationName != "" {
			name = *t.NotificationName
		}
		sev := t.Urgency
		count := float64(n)
		// Carry the source's device_source so a camera-event frequency rollup is
		// filterable as a camera event.
		src := "gps"
		if at, ok := alert.Lookup(t.AlertCode); ok {
			src = at.Source
		}
		if _, err := q.CreateAlert(ctx, store.CreateAlertParams{
			TenantID: tenantID, VehicleID: &vid, AlertCode: &rollup, AlertName: &name, Severity: &sev,
			DeviceSource: &src, ActualValue: conv.Num(&count), LimitValue: conv.Num(thr),
		}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	if wrote {
		return 1, nil
	}
	return 0, nil
}

// writeAlert persists one overspeed alert unless a recent one already exists.
// Returns false when suppressed by the cooldown.
func (e *Engine) writeAlert(ctx context.Context, tenantID int64, t store.ListActiveRuleTargetsRow, speed, limit float64, lat, lon *float64) (bool, error) {
	code := t.AlertCode
	name := fmt.Sprintf("Overspeed — %d km/h (limit %d)", int(speed), int(limit))
	if t.NotificationName != nil && *t.NotificationName != "" {
		name = *t.NotificationName
	}
	sev := t.Urgency
	vid := t.VehicleID
	wrote := false
	err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		n, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{
			TenantID: tenantID, VehicleID: &vid, AlertCode: &code, CooldownMin: cooldownMin,
		})
		if err != nil {
			return err
		}
		if n > 0 {
			return nil // still within cooldown
		}
		if _, err := q.CreateAlert(ctx, store.CreateAlertParams{
			TenantID: tenantID, VehicleID: &vid, AlertCode: &code, AlertName: &name, Severity: &sev,
			ActualValue: conv.Num(&speed), LimitValue: conv.Num(&limit), Lat: lat, Lon: lon,
		}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}
