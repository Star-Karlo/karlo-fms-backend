// Package notifengine evaluates active notification rules against live telemetry
// for the vehicles each rule is attached to, and persists alerts (notifications).
//
// It runs as a background ticker inside the API process. Instantaneous threshold
// rules that can be read from the ~60s live feed are driven by the `liveMetrics`
// table: OVERSPEED (speed), ENGINE_OVERREV (RPM) and LOW_OIL_PRESSURE (both from
// the engine CAN metadata). Adding a code there + a taxonomy entry is all it
// takes to make a new "over/under X" rule fire. Other alert codes can be attached
// but stay dormant until an evaluator + data source exist (idle/parking need
// dwell tracking; camera/geofence come from other producers).
package notifengine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
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

// liveMetric describes a rule whose condition is a single live-telemetry value
// compared to the rule's threshold each tick. metaKeys empty => use the
// top-level Speed; otherwise the first present metadata key wins (engine CAN
// keys vary by adapter). below flips the comparison to "fire when under".
type liveMetric struct {
	metaKeys []string
	below    bool
	def      float64 // threshold when the rule leaves it blank/0
	unit     string
	label    string
}

// liveMetrics is the set of alert codes evaluable against the ~60s live feed.
// Adding a code here (plus a taxonomy entry) is all it takes to make an
// instantaneous threshold rule actually fire. Engine keys are read from the CAN
// metadata blob, so only trucks with a CAN adapter trip them.
var liveMetrics = map[string]liveMetric{
	"OVERSPEED":        {nil, false, defaultOverspeed, "km/h", "Speed"},
	"ENGINE_OVERREV":   {[]string{"Engine Speed (CAN)", "Engine Speed"}, false, 2500, "rpm", "Engine RPM"},
	"LOW_OIL_PRESSURE": {[]string{"Engine Oil Pressure (CAN)", "Engine Oil Pressure"}, true, 100, "kPa", "Oil pressure"},
}

// liveValue extracts the metric's current value from a live fix.
func liveValue(pos telemetry.Position, spec liveMetric) (float64, bool) {
	if spec.metaKeys == nil {
		if pos.Speed == nil {
			return 0, false
		}
		return *pos.Speed, true
	}
	return metaNum(pos.Metadata, spec.metaKeys...)
}

// metaNum pulls the first present key from a metadata blob as a float (values
// arrive as either JSON numbers or numeric strings, depending on the device).
func metaNum(md json.RawMessage, keys ...string) (float64, bool) {
	if len(md) == 0 {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal(md, &m); err != nil {
		return 0, false
	}
	for _, k := range keys {
		switch n := m[k].(type) {
		case float64:
			return n, true
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// Engine periodically evaluates rules and writes alerts.
type Engine struct {
	pool     *db.Pool
	tel      *telemetry.Client
	interval time.Duration
	// docLast: when the daily document-expiry pass last ran, per tenant.
	docLast map[int64]time.Time
	// sopSince: when each SOP condition was first observed, per vehicle (sop.go).
	sopSince map[sopKey]time.Time
}

// New builds the engine. interval ~= the live poll cadence.
func New(pool *db.Pool, tel *telemetry.Client, interval time.Duration) *Engine {
	return &Engine{pool: pool, tel: tel, interval: interval, docLast: map[int64]time.Time{}, sopSince: map[sopKey]time.Time{}}
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
		if dn, err := e.evalDocuments(ctx, tid); err != nil {
			log.Printf("notifengine: tenant %d documents: %v", tid, err)
		} else {
			created += dn
		}
	}
	// SOP pass, only for tenants that switched it on.
	var sopTenants []store.CompanySopSetting
	if err := db.SystemTx(ctx, e.pool, func(tx pgx.Tx) error {
		var err error
		sopTenants, err = store.New(tx).ListSopEnabledTenants(ctx)
		return err
	}); err != nil {
		log.Printf("notifengine: sop tenants: %v", err)
	}
	for _, cfg := range sopTenants {
		if e.tel == nil {
			break
		}
		if n, err := e.evalSOP(ctx, cfg.TenantID, cfg); err != nil {
			log.Printf("notifengine: tenant %d sop: %v", cfg.TenantID, err)
		} else {
			created += n
		}
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

	// Collect IMEIs of vehicles carrying any live-evaluable rule, then fetch live.
	imeiSet := map[string]struct{}{}
	for _, t := range targets {
		if _, ok := liveMetrics[t.AlertCode]; ok && t.Imei != nil && *t.Imei != "" {
			imeiSet[*t.Imei] = struct{}{}
		}
	}
	if len(imeiSet) == 0 {
		return created, nil // no live-threshold rules; frequency pass already counted
	}
	imeis := make([]string, 0, len(imeiSet))
	for im := range imeiSet {
		imeis = append(imeis, im)
	}
	// "metadata" carries the engine CAN keys (RPM, oil pressure); speed stays
	// top-level. Trucks without a CAN adapter simply have no metadata to read.
	positions, err := e.tel.LiveMany(ctx, imeis, []string{"lat", "lon", "speed", "metadata"})
	if err != nil {
		return created, fmt.Errorf("telemetry: %w", err)
	}
	byIMEI := make(map[string]telemetry.Position, len(positions))
	for _, p := range positions {
		byIMEI[p.IMEI] = p
	}

	for _, t := range targets {
		spec, ok := liveMetrics[t.AlertCode]
		if !ok || t.Imei == nil {
			continue
		}
		pos, ok := byIMEI[*t.Imei]
		if !ok {
			continue
		}
		val, ok := liveValue(pos, spec)
		if !ok {
			continue // this vehicle doesn't report the metric (no CAN, etc.)
		}
		limit := spec.def
		if f := conv.NumF(t.Threshold); f != nil && *f > 0 {
			limit = *f
		}
		breached := val > limit
		if spec.below {
			breached = val < limit
		}
		if !breached {
			continue
		}
		ok, err := e.writeAlert(ctx, tenantID, t, spec, val, limit, pos.Lat, pos.Lon)
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

// writeAlert persists one threshold alert unless a recent one already exists.
// Returns false when suppressed by the cooldown.
func (e *Engine) writeAlert(ctx context.Context, tenantID int64, t store.ListActiveRuleTargetsRow, spec liveMetric, val, limit float64, lat, lon *float64) (bool, error) {
	code := t.AlertCode
	dir := "over"
	if spec.below {
		dir = "under"
	}
	name := fmt.Sprintf("%s %d %s (%s limit %d)", spec.label, int(math.Round(val)), spec.unit, dir, int(math.Round(limit)))
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
			ActualValue: conv.Num(&val), LimitValue: conv.Num(&limit), Lat: lat, Lon: lon,
		}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}
