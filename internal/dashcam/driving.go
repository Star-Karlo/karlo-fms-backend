package dashcam

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
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
	}
	return behaviour{}, false
}

// recordGreenEvent writes one driving_events row from a tracker Green Driving
// event. Idempotent on (tenant, source, green event id); source is 'teltonika'.
// event_code stores the raw Teltonika type (source-relative, not a VSS code).
func recordGreenEvent(ctx context.Context, pool *db.Pool, tenantID int64, link devLink, ev telemetry.GreenDrivingEvent, b behaviour) error {
	sev := "yellow"
	if ev.Gforce != nil {
		sev = severityFromGforce(*ev.Gforce)
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
			ev.EventType, b.kind, b.axis, ev.Gforce, sev, occurred, ev.Lat, ev.Lon, ev.Speed)
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
