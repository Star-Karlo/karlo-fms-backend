package dashcam

import "testing"

// The mapping that produced the complaint that started this: a covered lens
// reported as smoking. Pinned to Howen's VSS API sheet so a future "standard"
// table cannot reintroduce it.
func TestAlarmMetaMatchesHowenSheet(t *testing.T) {
	cases := map[int]string{
		123: "DMS_CAMERA_BLOCKED", // was DMS_SMOKING
		117: "DMS_SMOKING",        // was ADAS_EVENT
		121: "DMS_EYE_CLOSED",     // was DMS_FATIGUE
		122: "DMS_YAWN",           // was DMS_PHONE
		125: "DMS_SEATBELT",       // was DMS_CAMERA_BLOCKED
		116: "DMS_PHONE",
		115: "DMS_FATIGUE",
		110: "ADAS_FCW",
		102: "ADAS_LDW",
		104: "ADAS_PCW",
		200: "GEOFENCE_ENTER", // was ADAS_FCW
		201: "GEOFENCE_EXIT",  // was ADAS_LDW
		5:   "PANIC",          // was code 1
		214: "SHARP_TURN",
		217: "HARSH_BUMP",
	}
	for code, want := range cases {
		if got, _, _ := alarmMeta(code); got != want {
			t.Errorf("code %d: got %s want %s", code, got, want)
		}
	}
}

func TestStatusEventsAreNotAlerts(t *testing.T) {
	for _, code := range []int{41, 42, 43, 31, 19, 44, 768} {
		if !isStatusEvent(code) {
			t.Errorf("code %d should be housekeeping, not an alert", code)
		}
	}
	for _, code := range []int{123, 117, 5, 110} {
		if isStatusEvent(code) {
			t.Errorf("code %d is a real alarm and must not be suppressed", code)
		}
	}
}
