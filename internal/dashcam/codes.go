package dashcam

import "fmt"

// alarmMeta turns a VSS device-event code into an alert code, a human name, and
// a severity — the shape FMS alerts use.
//
// THE NUMBERS ARE HOWEN'S "VSS API" EVENT CODES, taken from the vendor's own
// "Device Event Type Code" sheet. Not the H-Protocol main/sub codes on the same
// sheet, which are a different numbering (ec=30 with tp=67 is the H-Protocol
// form of what the VSS API sends as 123), and not the "standard Howen mapping"
// this table used to carry, which was a guess and was wrong in a way that
// produced a very specific complaint: a driver covered the lens and the alert
// said SMOKING.
//
// It said that because the old table had 123 = smoking. Per the sheet, 123 is
// "Camera cover" and smoking is 117 — which the old table had filed under ADAS
// as a generic road-safety event. Nearly every DMS code was off by a few
// places: 121 was labelled fatigue and is eyes-closing, 122 phone and is
// yawning, 125 camera-blocked and is seatbelt, 126 yawn and is driver-left-
// duty. And the "legacy" 200-203 block, labelled as ADAS collision warnings,
// is the GEOFENCE enter/exit/overspeed range. A truck leaving a geofence was
// being reported as a lane departure.
//
// The raw code stays in the display name ("Camera cover (DMS) · 123"). The
// sheet's own remark says compliance "might change by time", and the name is
// the only place the number is preserved, so an operator reading the feed can
// always see what the device actually sent.
func alarmMeta(code int) (alertCode, name, severity string) {
	if m, ok := knownAlarms[code]; ok {
		return m.code, fmt.Sprintf("%s · %d", m.name, code), m.severity
	}
	return "DASHCAM_EVENT", fmt.Sprintf("Camera event (code %d)", code), "important"
}

// isStatusEvent reports device housekeeping that must NOT become an alert.
//
// The device reports trip boundaries, ignition, GPS recovery and periodic
// position pings through the same event channel as safety alarms. Treating
// each as an alert would put "In trip" in a driver's notification feed every
// time they set off, and bury the yawn in between. They are dropped here, at
// the poller, rather than mapped to a code nobody wants to see.
func isStatusEvent(code int) bool {
	_, ok := statusEvents[code]
	return ok
}

var statusEvents = map[int]struct{}{
	0:   {}, // unknown
	19:  {}, // ACC off
	31:  {}, // ACC on (reported once at boot)
	41:  {}, // trip start
	42:  {}, // in trip
	43:  {}, // trip ends (periodic after ACC off)
	44:  {}, // GPS location recovered
	46:  {}, // none-trip position (periodic after trip ends)
	57:  {}, // roaming mode start
	58:  {}, // roaming mode end
	120: {}, // driver change / shift
	129: {}, // driver returns
	131: {}, // driver authentication succeeded
	240: {}, // wake-up: IO
	241: {}, // wake-up: collision  (the collision itself arrives as 217)
	242: {}, // wake-up: timed
	243: {}, // wake-up: remote
	768: {}, // trip notification
	769: {}, // tyre-pressure notification (TPMS alarm is 81)
	770: {}, // disk detection notification
	771: {}, // datahub status notification
}

type meta struct {
	code, name, severity string
}

// severity: "urgent" for impairment, a collision risk, or tampering;
// "important" otherwise. FMS's alert UI already colours by these.
//
// alert codes reuse the taxonomy (internal/alert) where an entry exists, so the
// notification feed, Control Tower and the camera frequency-rule engine all
// group the same event the same way. Codes with no taxonomy entry are
// descriptive and prefixed by domain; they render with the name below and
// group by code.
var knownAlarms = map[int]meta{
	// ── Video / recorder ────────────────────────────────────────────────────
	1:  {"CAM_VIDEO_LOST", "Video signal lost", "important"},
	2:  {"CAM_MOTION", "Motion detected", "important"},
	3:  {"DMS_CAMERA_BLOCKED", "Video cover (lens obstructed)", "important"},
	16: {"CAM_STORAGE", "Storage abnormal", "important"},
	45: {"CAM_VIDEO_ABNORMAL", "Video abnormal", "important"},
	78: {"CAM_SELFCHECK", "AI camera self-check abnormal", "important"},
	79: {"CAM_AI_WATCH", "AI watch abnormal", "important"},
	80: {"ADAS_CALIBRATE", "ADAS auto-calibrate", "important"},

	// ── Emergency / speed / behaviour reported by the MDVR itself ───────────
	5:  {"PANIC", "Emergency / panic button", "urgent"},
	6:  {"LOW_SPEED", "Low speed alarm", "important"},
	7:  {"OVERSPEED", "Over speed alarm", "important"},
	11: {"EXCEEDED_PARKING", "Parking over time", "important"},
	12: {"HARSH_BUMP", "Vibration (G-sensor)", "important"},
	15: {"DOOR_ABNORMAL", "Abnormal door open / close", "important"},
	17: {"DMS_FATIGUE", "Overtime driving (fatigue)", "urgent"},
	18: {"FUEL_ABNORMAL", "Abnormal fuel level", "important"},
	24: {"HARSH_ACCEL", "Harsh acceleration", "important"},
	25: {"HARSH_BRAKING", "Harsh braking", "important"},
	26: {"LOW_SPEED", "Low speed warning", "important"},
	27: {"OVERSPEED", "High speed warning", "important"},
	32: {"EXCEEDED_IDLE", "Idle", "important"},
	37: {"TOWING", "Towing", "urgent"},
	38: {"FREE_WHEELING", "Free wheeling", "important"},
	39: {"ENGINE_OVERREV", "RPM exceeds", "important"},
	40: {"VEHICLE_MOVE", "Vehicle move", "important"},
	48: {"OVERSPEED", "Excessive overspeed", "urgent"},
	61: {"ALCOHOL", "Alcohol detected", "urgent"},
	62: {"DMS_FATIGUE", "Total driving time exceeded", "urgent"},
	82: {"NIGHT_DRIVING", "Driving at night", "important"},

	// ── Device / power / connectivity ───────────────────────────────────────
	20:  {"GPS_ABNORMAL", "GPS module abnormal", "important"},
	21:  {"PANEL_OPEN", "Front panel open", "important"},
	33:  {"GPS_ABNORMAL", "GPS antenna break", "important"},
	34:  {"GPS_ABNORMAL", "GPS antenna short", "important"},
	36:  {"CANBUS_ABNORMAL", "CANBUS connection abnormal", "important"},
	47:  {"DEVICE_OFFLINE", "Main unit anomaly (not connected)", "important"},
	50:  {"SIM_LOST", "SIM card lost", "important"},
	76:  {"GPS_JAMMING", "GPS jamming", "urgent"},
	77:  {"GSM_JAMMING", "GSM jamming", "urgent"},
	81:  {"TPMS", "Tyre pressure alarm", "important"},
	230: {"BATTERY_DROP", "Low voltage", "important"},
	231: {"HIGH_VOLTAGE", "High voltage", "important"},
	232: {"POWER_OFF", "Power off", "urgent"},
	233: {"POWER_ON", "Power on", "important"},
	234: {"POWER_DISCONNECT", "Suspicious disconnection", "urgent"},
	235: {"ABNORMAL_SHUTDOWN", "Abnormal shutdown", "urgent"},
	236: {"POWER_ON", "Start up", "important"},

	// ── Driver identity ─────────────────────────────────────────────────────
	22:  {"CARD_SWIPE", "Card swipe", "important"},
	23:  {"CARD_SWIPE", "iButton", "important"},
	132: {"DMS_AUTH_FAILED", "Driver authentication failed", "urgent"},

	// ── ADAS — the road-facing lens (100–111) ───────────────────────────────
	100: {"ADAS_HMW", "Safe distance (ADAS)", "important"},
	102: {"ADAS_LDW", "Lane departure warning (ADAS)", "important"},
	103: {"ADAS_HMW", "Front vehicle too close (ADAS)", "important"},
	104: {"ADAS_PCW", "Pedestrian collision warning (ADAS)", "urgent"},
	106: {"ADAS_ROAD_SIGN", "Road-sign violation / speed-sign exceeded (ADAS)", "important"},
	107: {"ADAS_FRONT_START", "Front vehicle started (ADAS)", "important"},
	109: {"ADAS_ROAD_SIGN", "Stop sign recognised (ADAS)", "important"},
	110: {"ADAS_FCW", "Forward collision warning (ADAS)", "urgent"},
	111: {"ADAS_HMW", "Front vehicle too close (ADAS)", "important"},

	// ── DMS — the driver-facing lens (115–134, 147–148, 251–261) ────────────
	115: {"DMS_FATIGUE", "Fatigue driving (DMS)", "urgent"},
	116: {"DMS_PHONE", "Phone call while driving (DMS)", "urgent"},
	117: {"DMS_SMOKING", "Smoking while driving (DMS)", "important"},
	118: {"DMS_DISTRACTION", "Distracted driving (DMS)", "urgent"},
	119: {"DMS_ABNORMAL", "Driver abnormal (DMS)", "urgent"},
	121: {"DMS_EYE_CLOSED", "Eyes closing (DMS)", "urgent"},
	122: {"DMS_YAWN", "Yawning (DMS)", "important"},
	123: {"DMS_CAMERA_BLOCKED", "Camera cover (DMS)", "important"},
	124: {"DMS_DISTRACTION", "Distracted — glancing left/right (DMS)", "urgent"},
	125: {"DMS_SEATBELT", "Seatbelt not worn (DMS)", "important"},
	126: {"DMS_NO_DRIVER", "Driver left duty (DMS)", "urgent"},
	127: {"DMS_DRINKING", "Drinking water (DMS)", "important"},
	130: {"DMS_SUNGLASSES", "Infrared-blocking sunglasses (DMS)", "important"},
	133: {"DMS_NO_DRIVER", "No driver detected (DMS)", "urgent"},
	134: {"DMS_MASK", "Mask not worn (DMS)", "important"},
	147: {"DMS_EYE_CLOSED", "Severe eye closure (DMS)", "urgent"},
	148: {"DMS_YAWN", "Severe yawning (DMS)", "important"},
	251: {"DMS_EATING", "Eating (DMS)", "important"},
	252: {"DMS_FATIGUE", "Frequent drowsiness — nodding (DMS)", "urgent"},
	253: {"DMS_YAWN", "Frequent yawning (DMS)", "important"},
	254: {"DMS_YAWN", "Prolonged yawning (DMS)", "important"},
	255: {"DMS_FATIGUE", "Frequent blinking (DMS)", "important"},
	256: {"DMS_FATIGUE", "Prolonged blinking (DMS)", "important"},
	257: {"DMS_EYE_CLOSED", "Slightly closed eyes, level 1 (DMS)", "important"},
	258: {"DMS_EYE_CLOSED", "Slightly closed eyes, level 2 (DMS)", "urgent"},
	259: {"DMS_CO_DRIVER", "Co-driver detected (DMS)", "important"},
	260: {"DMS_FATIGUE", "Slightly sleepy, level 2 (DMS)", "urgent"},
	261: {"DMS_MOUTH_COVER", "Mouth covered (DMS)", "important"},

	// ── HOD — hands on wheel / dashboard ────────────────────────────────────
	262: {"HOD_ONE_HAND", "One hand off the wheel", "important"},
	263: {"HOD_NO_HANDS", "Both hands off the wheel", "urgent"},
	264: {"DMS_PHONE", "Texting on mobile phone", "urgent"},
	265: {"DASHBOARD_OBJECT", "Objects left on dashboard", "important"},

	// ── BSD — blind-spot detection, four sensors × three levels ─────────────
	135: {"ADAS_OBSTACLE", "Blind spot 1, level 1", "important"},
	136: {"ADAS_OBSTACLE", "Blind spot 1, level 2", "important"},
	137: {"ADAS_OBSTACLE", "Blind spot 1, level 3", "urgent"},
	138: {"ADAS_OBSTACLE", "Blind spot 2, level 1", "important"},
	139: {"ADAS_OBSTACLE", "Blind spot 2, level 2", "important"},
	140: {"ADAS_OBSTACLE", "Blind spot 2, level 3", "urgent"},
	141: {"ADAS_OBSTACLE", "Blind spot 3, level 1", "important"},
	142: {"ADAS_OBSTACLE", "Blind spot 3, level 2", "important"},
	143: {"ADAS_OBSTACLE", "Blind spot 3, level 3", "urgent"},
	144: {"ADAS_OBSTACLE", "Blind spot 4, level 1", "important"},
	145: {"ADAS_OBSTACLE", "Blind spot 4, level 2", "important"},
	146: {"ADAS_OBSTACLE", "Blind spot 4, level 3", "urgent"},

	// ── Geofence (200–209, 215–216, 206, 266–267) ───────────────────────────
	200: {"GEOFENCE_ENTER", "Geofence entered", "important"},
	201: {"GEOFENCE_EXIT", "Geofence exited", "important"},
	202: {"OVERSPEED", "Geofence over-speed alarm", "urgent"},
	203: {"OVERSPEED", "Geofence over-speed warning", "important"},
	204: {"LOW_SPEED", "Geofence low-speed alarm", "important"},
	205: {"LOW_SPEED", "Geofence low-speed warning", "important"},
	206: {"OVERSPEED", "Geofence excessive over-speed", "urgent"},
	207: {"GEOFENCE_PARKING", "Forbidden parking — engine started", "important"},
	208: {"GEOFENCE_PARKING", "Forbidden parking — engine off", "important"},
	209: {"EXCEEDED_PARKING", "Overtime stay in geofence", "important"},
	215: {"GEOFENCE_ENTER", "Geofence pre-entry", "important"},
	216: {"GEOFENCE_EXIT", "Geofence pre-exit", "important"},
	266: {"HARSH_ACCEL", "Harsh acceleration in geofence", "important"},
	267: {"HARSH_BRAKING", "Harsh braking in geofence", "important"},

	// ── Fuel ────────────────────────────────────────────────────────────────
	210: {"FUEL_GAIN", "Refuel", "important"},
	211: {"FUEL_THEFT", "Fuel theft", "urgent"},

	// ── G-sensor sub-types (212–218) ────────────────────────────────────────
	212: {"HARSH_ACCEL", "Harsh acceleration (G-sensor)", "important"},
	213: {"HARSH_BRAKING", "Harsh braking (G-sensor)", "important"},
	214: {"SHARP_TURN", "Harsh turn (G-sensor)", "important"},
	217: {"HARSH_BUMP", "Impact (G-sensor)", "urgent"},
	218: {"ROLLOVER", "Rollover (G-sensor)", "urgent"},

	// ── Digital inputs / outputs ────────────────────────────────────────────
	220: {"INPUT_TRIGGER", "Input trigger ch 0/1", "important"},
	221: {"INPUT_TRIGGER", "Input trigger ch 2", "important"},
	222: {"INPUT_TRIGGER", "Input trigger ch 3", "important"},
	223: {"INPUT_TRIGGER", "Input trigger ch 4", "important"},
	224: {"INPUT_TRIGGER", "Input trigger ch 5", "important"},
	225: {"INPUT_TRIGGER", "Input trigger ch 6", "important"},
	226: {"INPUT_TRIGGER", "Input trigger ch 7", "important"},
	227: {"INPUT_TRIGGER", "Input trigger ch 8", "important"},
	228: {"IO_OUTPUT", "Output 1", "important"},
	229: {"IO_OUTPUT", "Output 2", "important"},
	245: {"INPUT_TRIGGER", "Datahub input 1", "important"},
	246: {"INPUT_TRIGGER", "Datahub input 2", "important"},
	247: {"INPUT_TRIGGER", "Datahub input 3", "important"},
	248: {"INPUT_TRIGGER", "Datahub input 4", "important"},
	249: {"INPUT_TRIGGER", "Datahub input 5", "important"},
	250: {"INPUT_TRIGGER", "Datahub input 6", "important"},

	// ── Load ────────────────────────────────────────────────────────────────
	237: {"OVERLOAD", "Overload", "urgent"},
	238: {"UNDERLOAD", "Underload", "important"},
	239: {"WEIGHT_SENSOR", "Weight sensor communication abnormal", "important"},

	// ── Tracker-reported (51–56, 63–71) ─────────────────────────────────────
	51: {"DMS_SEATBELT", "Seatbelt (tracker)", "important"},
	52: {"HARSH_ACCEL", "Harsh acceleration (tracker)", "important"},
	53: {"HARSH_BRAKING", "Harsh braking (tracker)", "important"},
	54: {"OVERSPEED", "Overspeed (tracker)", "important"},
	55: {"OVERSPEED", "Excessive overspeed (tracker)", "urgent"},
	56: {"PANEL_OPEN", "Panel open (tracker)", "important"},
	63: {"ENGINE_OVERREV", "RPM too high (tracker)", "important"},
	64: {"HARSH_BUMP", "Impact (tracker)", "urgent"},
	65: {"TOWING", "Traction (tracker)", "urgent"},
	66: {"POWER_DISCONNECT", "False disconnection (tracker)", "important"},
	67: {"VEHICLE_MOVE", "Displacement (tracker)", "important"},
	68: {"EXCEEDED_IDLE", "Idling (tracker)", "important"},
	69: {"ROLLOVER", "Rollover (tracker)", "urgent"},
	70: {"PANIC", "Emergency warning (tracker)", "urgent"},
	71: {"SHARP_TURN", "Harsh cornering (tracker)", "important"},

	// ── Platform virtual alarms ─────────────────────────────────────────────
	11003: {"DEVICE_OFFLINE", "Vehicle offline for a long time", "important"},
	11004: {"DEVICE_OFFLINE", "Transmission break", "important"},
}
