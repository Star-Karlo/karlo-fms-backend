package dashcam

import "fmt"

// alarmMeta turns a VSS device-event code (API PDF §8.9 / the product event
// matrix) into an alert code, a human name, and a severity — the shape FMS
// alerts use.
//
// Only the events worth raising an alert for are named. The list is deliberately
// small: DMS/ADAS safety events and the high-value vehicle events. An unknown
// code still raises a generic alert (better a vague alert than a silent one),
// tagged with the raw number so it can be identified and named later.
//
// Codes follow the vendor's first-level event types. Where the vendor splits a
// family into sub-types (DMS/ADAS carry a `tp` subtype in the detail), the
// first-level code is enough to route and rank it.
func alarmMeta(code int) (alertCode, name, severity string) {
	if m, ok := knownAlarms[code]; ok {
		return m.code, m.name, m.severity
	}
	return "DASHCAM_EVENT", fmt.Sprintf("Camera event (code %d)", code), "important"
}

type meta struct {
	code, name, severity string
}

// severity: "urgent" for anything about a collision or a distracted/impaired
// driver; "important" for the rest. FMS's alert UI already colours by these.
var knownAlarms = map[int]meta{
	// Driver monitoring (DMS) — the driver themselves.
	100: {"DMS_FATIGUE", "Driver fatigue (DMS)", "urgent"},
	101: {"DMS_DISTRACTION", "Driver distraction (DMS)", "urgent"},
	102: {"DMS_PHONE", "Phone use while driving (DMS)", "urgent"},
	103: {"DMS_SMOKING", "Smoking while driving (DMS)", "important"},
	104: {"DMS_NO_DRIVER", "Driver absent (DMS)", "urgent"},
	105: {"DMS_CAMERA_BLOCKED", "Driver camera obstructed (DMS)", "important"},

	// Advanced driver assistance (ADAS) — the road ahead.
	200: {"ADAS_FCW", "Forward collision warning (ADAS)", "urgent"},
	201: {"ADAS_LDW", "Lane departure (ADAS)", "important"},
	202: {"ADAS_HMW", "Following too close (ADAS)", "important"},
	203: {"ADAS_PCW", "Pedestrian collision warning (ADAS)", "urgent"},

	// Vehicle behaviour the MDVR reports directly.
	24: {"HARSH_ACCEL", "Harsh acceleration", "important"},
	25: {"HARSH_BRAKE", "Harsh braking", "important"},
	26: {"LOW_SPEED", "Low speed", "important"},
	27: {"HIGH_SPEED", "Speeding", "important"},

	// Emergency / panic and collision.
	1: {"PANIC", "Panic / emergency button", "urgent"},
}
