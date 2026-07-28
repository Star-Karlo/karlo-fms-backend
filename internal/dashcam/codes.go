package dashcam

import "fmt"

// alarmMeta turns a VSS device-event code into an alert code, a human name, and
// a severity — the shape FMS alerts use.
//
// The codes are Howen's "alarm of event" (AOE) active-safety numbers. The 11x
// range is ADAS (the road-facing lens) and the 12x range is DMS (the
// driver-facing lens). This table is the STANDARD Howen VSS mapping; the exact
// numbers are being verified against the live fleet, so alarmMeta keeps the raw
// code in the display name ("Yawn (DMS) · 126") until each one is confirmed. An
// unknown code still raises a generic, code-tagged alert rather than nothing.
func alarmMeta(code int) (alertCode, name, severity string) {
	if m, ok := knownAlarms[code]; ok {
		// Keep the raw code visible for now so the fleet can confirm each name
		// against the Howen portal. Drop the "· NNN" suffix here once verified.
		return m.code, fmt.Sprintf("%s · %d", m.name, code), m.severity
	}
	return "DASHCAM_EVENT", fmt.Sprintf("Camera event (code %d)", code), "important"
}

type meta struct {
	code, name, severity string
}

// severity: "urgent" for impairment or a collision risk; "important" otherwise.
// FMS's alert UI already colours by these.
//
// alert codes reuse the taxonomy (internal/alert) where an entry exists, so the
// notification feed, Control Tower and the camera frequency-rule engine all
// group the same event the same way.
var knownAlarms = map[int]meta{
	// ── ADAS — the road-facing lens (11x) ───────────────────────────────────
	110: {"ADAS_FCW", "Forward collision warning (ADAS)", "urgent"},
	111: {"ADAS_LDW", "Lane departure (ADAS)", "important"},
	112: {"ADAS_HMW", "Following too close (ADAS)", "important"},
	113: {"ADAS_PCW", "Pedestrian collision warning (ADAS)", "urgent"},
	114: {"ADAS_LANE_CHANGE", "Frequent lane change (ADAS)", "important"},
	115: {"ADAS_ROAD_SIGN", "Road-sign / speed-sign alert (ADAS)", "important"},
	116: {"ADAS_OBSTACLE", "Obstacle / blind spot (ADAS)", "important"},
	117: {"ADAS_EVENT", "Road-safety event (ADAS)", "important"},

	// ── DMS — the driver-facing lens (12x) ──────────────────────────────────
	120: {"DMS_NO_DRIVER", "Driver absent (DMS)", "urgent"},
	121: {"DMS_FATIGUE", "Driver fatigue / drowsiness (DMS)", "urgent"},
	122: {"DMS_PHONE", "Phone use while driving (DMS)", "urgent"},
	123: {"DMS_SMOKING", "Smoking while driving (DMS)", "important"},
	124: {"DMS_DISTRACTION", "Driver distraction (DMS)", "urgent"},
	125: {"DMS_CAMERA_BLOCKED", "Driver camera obstructed (DMS)", "important"},
	126: {"DMS_YAWN", "Yawning (DMS)", "important"},
	127: {"DMS_SEATBELT", "Seatbelt not fastened (DMS)", "important"},
	128: {"DMS_EYE_CLOSED", "Eyes closed (DMS)", "urgent"},
	129: {"DMS_EVENT", "Driver-monitoring event (DMS)", "important"},

	// ── Legacy first-level codes (kept for any device still on the old map) ──
	100: {"DMS_FATIGUE", "Driver fatigue (DMS)", "urgent"},
	101: {"DMS_DISTRACTION", "Driver distraction (DMS)", "urgent"},
	102: {"DMS_PHONE", "Phone use while driving (DMS)", "urgent"},
	103: {"DMS_SMOKING", "Smoking while driving (DMS)", "important"},
	104: {"DMS_NO_DRIVER", "Driver absent (DMS)", "urgent"},
	105: {"DMS_CAMERA_BLOCKED", "Driver camera obstructed (DMS)", "important"},
	200: {"ADAS_FCW", "Forward collision warning (ADAS)", "urgent"},
	201: {"ADAS_LDW", "Lane departure (ADAS)", "important"},
	202: {"ADAS_HMW", "Following too close (ADAS)", "important"},
	203: {"ADAS_PCW", "Pedestrian collision warning (ADAS)", "urgent"},

	// ── Vehicle behaviour the MDVR reports directly ─────────────────────────
	24: {"HARSH_ACCEL", "Harsh acceleration", "important"},
	25: {"HARSH_BRAKE", "Harsh braking", "important"},
	26: {"LOW_SPEED", "Low speed", "important"},
	27: {"HIGH_SPEED", "Speeding", "important"},

	// ── Emergency / panic ───────────────────────────────────────────────────
	1: {"PANIC", "Panic / emergency button", "urgent"},
}
