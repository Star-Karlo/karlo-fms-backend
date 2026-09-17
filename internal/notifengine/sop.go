package notifengine

// The SOP pass (feedback epic 1.1): "out of procedure" conditions that hold
// for a DURATION, against the company's own thresholds (internal/sop).
//
//   SOP_OVERSPEED   speed > limit for at least overspeed_min
//   SOP_LONG_STOP   not moving for at least stop_max_min (ignition on or off)
//   SOP_OFFLINE     no position for at least offline_min
//   SOP_SENSOR      a vehicle configured with a fuel sensor whose fixes have
//                   carried no fuel reading for at least sensor_timeout_min
//
// Unlike the threshold rules, which fire on one fix, these need memory: the
// engine keeps the moment each condition was FIRST seen per vehicle and
// raises the alert once the run is long enough; a raised alert resets the
// run so the next one needs another full window (plus the usual cooldown).
// State lives in sop_runs and is loaded per tenant on the first tick after a
// start, then written back after every tick, so a deploy no longer resets a
// run in progress.
//
// Runs for every active vehicle of a tenant that switched sop_enabled on.
// No rule attachment needed: an SOP is fleet-wide by definition. Alerts name
// the driver as well as the truck, because the follow-up is a phone call.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

const (
	codeSopOverspeed = "SOP_OVERSPEED"
	codeSopLongStop  = "SOP_LONG_STOP"
	codeSopOffline   = "SOP_OFFLINE"
	codeSopSensor    = "SOP_SENSOR"
	movingKmh        = 3.0 // below this the truck is "not moving"
)

type sopKey struct {
	vehicle int64
	code    string
}

// evalSOP evaluates the four conditions for one tenant. Returns alerts written.
func (e *Engine) evalSOP(ctx context.Context, tenantID int64, cfg store.CompanySopSetting) (int, error) {
	var vehicles []store.ListTenantVehiclesWithTrackerRow
	var drivers map[int64]string
	if err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		var err error
		vehicles, err = store.New(tx).ListTenantVehiclesWithTracker(ctx, tenantID)
		if err != nil {
			return err
		}
		drivers, err = driverNames(ctx, tx)
		return err
	}); err != nil {
		return 0, err
	}
	if len(vehicles) == 0 {
		return 0, nil
	}
	e.loadSOP(ctx, tenantID)
	defer e.saveSOP(ctx, tenantID)
	imeis := make([]string, 0, len(vehicles))
	for _, v := range vehicles {
		imeis = append(imeis, v.Imei)
	}
	positions, err := e.tel.LiveMany(ctx, imeis, []string{"lat", "lon", "speed", "ignition", "time", "metadata"})
	if err != nil {
		return 0, fmt.Errorf("telemetry: %w", err)
	}
	byIMEI := make(map[string]telemetry.Position, len(positions))
	for _, p := range positions {
		byIMEI[p.IMEI] = p
	}

	limit := 80.0
	if f := conv.NumF(cfg.SpeedLimitKmh); f != nil && *f > 0 {
		limit = *f
	}
	now := time.Now()
	created := 0
	for _, v := range vehicles {
		pos, ok := byIMEI[v.Imei]
		if !ok {
			continue // never reported; nothing to measure a run against
		}
		age := now.Sub(pos.Time)
		for _, c := range []string{codeSopOverspeed, codeSopLongStop, codeSopOffline, codeSopSensor} {
			e.tagTenant(sopKey{v.VehicleID, c}, tenantID)
		}
		driver := drivers[v.VehicleID]
		who := v.LicensePlate
		if driver != "" {
			who = v.LicensePlate + " · " + driver
		}

		// OFFLINE is the fix's own age — no run-tracking needed.
		if age >= time.Duration(cfg.OfflineMin)*time.Minute {
			n := fmt.Sprintf("%s offline for %s (SOP limit %d min)", who, humanDur(age), cfg.OfflineMin)
			if e.raise(ctx, tenantID, v.VehicleID, codeSopOffline, n, alert.SevImportant, age.Minutes(), float64(cfg.OfflineMin), pos) {
				created++
			}
		} else {
			e.sopSince[sopKey{v.VehicleID, codeSopOffline}] = time.Time{}
		}
		if age > 10*time.Minute {
			// A stale fix says nothing about speed or stop right now.
			continue
		}

		speed := 0.0
		if pos.Speed != nil {
			speed = *pos.Speed
		}
		// OVERSPEED run.
		if run := e.track(sopKey{v.VehicleID, codeSopOverspeed}, speed > limit, now); run >= time.Duration(cfg.OverspeedMin)*time.Minute {
			n := fmt.Sprintf("%s over %d km/h for %s (now %d km/h; SOP limit %d min)", who, int(limit), humanDur(run), int(math.Round(speed)), cfg.OverspeedMin)
			if e.raise(ctx, tenantID, v.VehicleID, codeSopOverspeed, n, alert.SevUrgent, run.Minutes(), float64(cfg.OverspeedMin), pos) {
				created++
				e.sopSince[sopKey{v.VehicleID, codeSopOverspeed}] = now
			}
		}
		// LONG STOP run.
		if run := e.track(sopKey{v.VehicleID, codeSopLongStop}, speed < movingKmh, now); run >= time.Duration(cfg.StopMaxMin)*time.Minute {
			ign := ""
			if pos.Ignition != nil {
				if *pos.Ignition {
					ign = ", engine on"
				} else {
					ign = ", engine off"
				}
			}
			place := strings.TrimSpace(strings.Join(nonEmpty(pos.Jalan, pos.Kecamatan, pos.Kota, pos.Kabupaten), ", "))
			if place != "" {
				place = " at " + place
			}
			n := fmt.Sprintf("%s stopped for %s%s%s (SOP limit %s)", who, humanDur(run), ign, place, humanDur(time.Duration(cfg.StopMaxMin)*time.Minute))
			if e.raise(ctx, tenantID, v.VehicleID, codeSopLongStop, n, alert.SevImportant, run.Minutes(), float64(cfg.StopMaxMin), pos) {
				created++
				e.sopSince[sopKey{v.VehicleID, codeSopLongStop}] = now
			}
		}
		// SENSOR run: configured with a fuel sensor, but the fix carries none.
		if v.HasFuelSensor {
			if run := e.track(sopKey{v.VehicleID, codeSopSensor}, !hasFuelReading(pos.Metadata), now); run >= time.Duration(cfg.SensorTimeoutMin)*time.Minute {
				n := fmt.Sprintf("%s fuel sensor not reporting for %s (SOP limit %d min)", who, humanDur(run), cfg.SensorTimeoutMin)
				if e.raise(ctx, tenantID, v.VehicleID, codeSopSensor, n, alert.SevImportant, run.Minutes(), float64(cfg.SensorTimeoutMin), pos) {
					created++
					e.sopSince[sopKey{v.VehicleID, codeSopSensor}] = now
				}
			}
		}
	}
	return created, nil
}

// loadSOP pulls a tenant's persisted runs into memory once per process.
func (e *Engine) loadSOP(ctx context.Context, tenantID int64) {
	if e.sopLoaded[tenantID] {
		return
	}
	_ = db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT vehicle_id, code, since FROM sop_runs WHERE tenant_id = $1`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k sopKey
			var since time.Time
			if err := rows.Scan(&k.vehicle, &k.code, &since); err != nil {
				return err
			}
			e.sopSince[k] = since
			e.sopTenant[k] = tenantID
		}
		return rows.Err()
	})
	e.sopLoaded[tenantID] = true
}

// saveSOP rewrites the tenant's runs from memory (a few rows per vehicle at
// most; a delete + insert is simpler than diffing and just as cheap).
func (e *Engine) saveSOP(ctx context.Context, tenantID int64) {
	err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM sop_runs WHERE tenant_id = $1`, tenantID); err != nil {
			return err
		}
		for k, since := range e.sopSince {
			if e.sopTenant[k] != tenantID || since.IsZero() {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO sop_runs (tenant_id, vehicle_id, code, since) VALUES ($1, $2, $3, $4)`, tenantID, k.vehicle, k.code, since); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("notifengine: save sop runs tenant %d: %v", tenantID, err)
	}
}

// track records when `cond` was first seen true for key and returns how long
// it has held; a false observation clears the run.
func (e *Engine) track(k sopKey, cond bool, now time.Time) time.Duration {
	if !cond {
		delete(e.sopSince, k)
		return 0
	}
	since, ok := e.sopSince[k]
	if !ok || since.IsZero() {
		e.sopSince[k] = now
		return 0
	}
	return now.Sub(since)
}

// tagTenant remembers which tenant a key belongs to, for saveSOP.
func (e *Engine) tagTenant(k sopKey, tenantID int64) { e.sopTenant[k] = tenantID }

func (e *Engine) raise(ctx context.Context, tenantID, vehicleID int64, code, name, sev string, actual, limit float64, pos telemetry.Position) bool {
	wrote := false
	err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		n, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{TenantID: tenantID, VehicleID: &vehicleID, AlertCode: &code, CooldownMin: cooldownMin})
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		src := alert.SourceSystem
		if _, err := q.CreateAlert(ctx, store.CreateAlertParams{
			TenantID: tenantID, VehicleID: &vehicleID, AlertCode: &code, AlertName: &name, Severity: &sev, DeviceSource: &src,
			ActualValue: conv.Num(&actual), LimitValue: conv.Num(&limit), Lat: pos.Lat, Lon: pos.Lon,
		}); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	if err != nil {
		log.Printf("notifengine: sop %s veh %d: %v", code, vehicleID, err)
		return false
	}
	return wrote
}

func driverNames(ctx context.Context, tx pgx.Tx) (map[int64]string, error) {
	rows, err := tx.Query(ctx, `SELECT v.id, e.full_name FROM vehicles v JOIN employees e ON e.id = v.current_driver_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func hasFuelReading(md json.RawMessage) bool {
	if len(md) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(md, &m); err != nil {
		return false
	}
	for k := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "fuel") || strings.Contains(lk, "lls") {
			return true
		}
	}
	return false
}

func nonEmpty(ss ...string) []string {
	out := ss[:0:0]
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func humanDur(d time.Duration) string {
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %02d min", m/60, m%60)
}
