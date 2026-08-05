package dashcam

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Driving-behaviour ingest: turn the harsh G-sensor events among the dashcam
// alarms into tenant-scoped driving_events, the source the driver-score
// collector reads for the harsh braking / acceleration / cornering pillars.
//
// This lives beside the alarm poller (alarmpoller.go), which already maps each
// device to a vehicle+tenant and walks new alarms. For every alarm that is a
// driving-behaviour event, we also write one driving_events row.

// VSS device-event codes for driving behaviour. Accel/brake are confirmed from
// the product event matrix; cornering and road-shock are DEVICE-SPECIFIC and
// UNCONFIRMED — set them here the moment a real event's raw payload reveals the
// code the Gtrack/Howen MDVR uses. They sit at a sentinel that never matches, so
// nothing is silently misclassified while they are unknown, and enabling each
// pillar is then a one-line edit.
const (
	codeHarshAccel  = 24
	codeHarshBrake  = 25
	codeHarshCorner = -1 // TODO: confirm the harsh-cornering code (Y axis)
	codeRoadShock   = -1 // TODO: confirm the road-shock / vibration code (Z axis)
)

type behaviour struct {
	kind string // braking | acceleration | cornering | road_shock
	axis string // X | Y | Z
}

// behaviours maps a device-event code to the behaviour it represents. Codes
// still at the -1 sentinel (cornering, road-shock) are skipped so an unconfirmed
// code cannot collide with a real one or with each other.
var behaviours = func() map[int]behaviour {
	m := map[int]behaviour{}
	add := func(code int, b behaviour) {
		if code >= 0 {
			m[code] = b
		}
	}
	add(codeHarshAccel, behaviour{"acceleration", "X"})
	add(codeHarshBrake, behaviour{"braking", "X"})
	add(codeHarshCorner, behaviour{"cornering", "Y"})
	add(codeRoadShock, behaviour{"road_shock", "Z"})
	return m
}()

// isDrivingEvent reports whether a code is a driving-behaviour event we score.
func isDrivingEvent(code int) (behaviour, bool) {
	b, ok := behaviours[code]
	return b, ok
}

// G-force severity thresholds (g), from the fleet risk model:
//
//	< 0.3 safe/normal · ≥ 0.3 yellow · ≥ 0.4 red · ≥ 0.8 black
//
// The device only pushes an event once its own threshold is crossed, so a
// reported event is at least "yellow" even below 0.3 g.
const (
	gYellow = 0.3
	gRed    = 0.4
	gBlack  = 0.8
)

// classifySeverity ranks an event yellow/red/black. It uses the reported G
// magnitude when present; otherwise it falls back to the event code's inherent
// severity (urgent → red, else yellow) so behaviour still classifies before the
// G field is confirmed.
func classifySeverity(gforce *float64, code int) string {
	if gforce != nil {
		return severityFromGforce(*gforce)
	}
	if _, _, sev := alarmMeta(code); sev == "urgent" {
		return "red"
	}
	return "yellow"
}

// severityFromGforce ranks a magnitude on the g thresholds.
func severityFromGforce(g float64) string {
	switch {
	case g >= gBlack:
		return "black"
	case g >= gRed:
		return "red"
	default:
		return "yellow"
	}
}

// greenBehaviour maps a Teltonika Green Driving type (AVL 253) to its kind/axis.
// 1 harsh acceleration, 2 harsh braking, 3 harsh cornering.
func greenBehaviour(typ int) (behaviour, bool) {
	switch typ {
	case 1:
		return behaviour{"acceleration", "X"}, true
	case 2:
		return behaviour{"braking", "X"}, true
	case 3:
		return behaviour{"cornering", "Y"}, true
	case 4:
		return behaviour{"road_shock", "Z"}, true
	}
	return behaviour{}, false
}

// teltonikaValueToG converts a Teltonika Green Driving Value to g. For the
// accelerometer scenario the device sends g×100 (e.g. 45 → 0.45 g). A single
// documented constant so it is trivially recalibrated if a unit is confirmed
// different. Per-axis X/Y/Z g is the camera's job; this is the single event
// magnitude the tracker reports.
const teltonikaValueToG = 0.01

// greenGforce converts a raw Green Driving Value to g, or nil if none.
func greenGforce(value *float64) *float64 {
	if value == nil {
		return nil
	}
	g := *value * teltonikaValueToG
	return &g
}

// recordGreenEvent writes one driving_events row from a tracker Green Driving
// event. Idempotent on (tenant, source, green event id); source is 'teltonika'.
// event_code stores the raw Teltonika type (source-relative, not a VSS code).
func recordGreenEvent(ctx context.Context, pool *db.Pool, tenantID int64, link devLink, ev telemetry.GreenDrivingEvent, b behaviour) error {
	g := greenGforce(ev.Value)
	sev := "yellow"
	if g != nil {
		sev = severityFromGforce(*g)
	}
	occurred := parseTime(ev.OccurredAt)
	return db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO driving_events
  (tenant_id, source, vehicle_id, driver_id, vss_device_id, telemetry_alarm_id,
   event_code, kind, axis, gforce, severity, occurred_at, lat, lon, speed_kph)
VALUES ($1,'teltonika',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (tenant_id, source, telemetry_alarm_id) DO NOTHING`,
			tenantID, link.vehicleID, link.driverID, ev.IMEI, ev.ID,
			ev.EventType, b.kind, b.axis, g, sev, occurred, ev.Lat, ev.Lon, ev.Speed)
		return err
	})
}

// harshAlertCode maps a Green Driving event_type to its taxonomy alert code +
// English display name. ok=false for non-harsh types.
func harshAlertCode(eventType int) (code, name string, ok bool) {
	switch eventType {
	case 1:
		code = "HARSH_ACCEL"
	case 2:
		code = "HARSH_BRAKING"
	case 3:
		code = "SHARP_TURN"
	case 4:
		code = "HARSH_BUMP"
	default:
		return "", "", false
	}
	return code, alert.LabelFor(code), true
}

// greenNotifyCooldownMin dedups tracker-harsh notifications per vehicle+code, so
// a rough stretch of road doesn't flood the Notifications feed.
const greenNotifyCooldownMin = 5

// notifySevereGreen raises a Notification for a SEVERE tracker harsh event
// (g >= red). Gentle events still score via recordGreenEvent but stay off the
// bell; that keeps the feed meaningful. Best-effort and deduped within a
// cooldown window. Mirrors AlarmPoller's CreateAlert path so tracker harsh
// events land in the same Notifications feed as camera ones.
func notifySevereGreen(ctx context.Context, pool *db.Pool, tenantID int64, link devLink, ev telemetry.GreenDrivingEvent) error {
	g := greenGforce(ev.Value)
	if g == nil || *g < gRed { // only red/black count as severe
		return nil
	}
	code, label, ok := harshAlertCode(ev.EventType)
	if !ok {
		return nil
	}
	name := fmt.Sprintf("%s (%.2f g)", label, *g)
	sev := alert.SevImportant
	if *g >= gBlack {
		sev = alert.SevUrgent
	}
	veh := link.vehicleID
	src := alert.SourceGPS
	occurred := parseTime(ev.OccurredAt)
	return db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		recent, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{
			TenantID: tenantID, VehicleID: &veh, AlertCode: &code, CooldownMin: greenNotifyCooldownMin,
		})
		if err != nil {
			return err
		}
		if recent > 0 {
			return nil // already notified for this vehicle+code recently
		}
		_, err = q.CreateAlert(ctx, store.CreateAlertParams{
			TenantID:     tenantID,
			VehicleID:    &veh,
			AlertCode:    &code,
			AlertName:    &name,
			Severity:     &sev,
			DeviceSource: src,
			OccurredAt:   pgtype.Timestamptz{Time: occurred, Valid: true},
			Lat:          ev.Lat,
			Lon:          ev.Lon,
		})
		return err
	})
}

// recordDrivingEvent writes one driving_events row for a behaviour alarm, scoped
// to the tenant and attributed to the vehicle's driver at ingest time. Idempotent
// on the telemetry alarm id, so a re-poll never double-counts. Non-behaviour
// alarms are ignored by the caller; this assumes the code is a behaviour.
func recordDrivingEvent(ctx context.Context, pool *db.Pool, tenantID int64, link devLink, a telemetry.DashcamAlarm, b behaviour) error {
	sev := classifySeverity(a.Gforce, a.Code)
	occurred := parseTime(a.OccurredAt)
	return db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO driving_events
  (tenant_id, source, vehicle_id, driver_id, vss_device_id, telemetry_alarm_id,
   event_code, kind, axis, gforce, severity, occurred_at, lat, lon, speed_kph)
VALUES ($1,'camera',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (tenant_id, source, telemetry_alarm_id) DO NOTHING`,
			tenantID, link.vehicleID, link.driverID, a.DeviceID, a.ID,
			a.Code, b.kind, b.axis, a.Gforce, sev, occurred, a.Lat, a.Lon, a.Speed)
		return err
	})
}
