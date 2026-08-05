// Package alert models safety/telematics alerts shared by Control Tower, the
// Notification module, and the Vehicle Alerts History report.
package alert

import "strings"

// DeviceSource classifies where an alert originates.
const (
	SourceGPS    = "gps"
	SourceCamera = "camera"
)

// Severity buckets (drive Control Tower colors + Notification importance).
const (
	SevUrgent    = "urgent"
	SevImportant = "important"
	SevInfo      = "info"
)

// AlertType is one entry in the master alert taxonomy.
type AlertType struct {
	Code     string `json:"code"`
	Name     string `json:"name"`     // Indonesian display name
	NameEN   string `json:"name_en"`  // English subtitle
	Source   string `json:"source"`   // gps | camera
	Severity string `json:"severity"` // default severity
	// Scorecard indicates this alert type also feeds Driver Scorecard scoring.
	Scorecard bool `json:"scorecard"`
}

// freqSuffix marks a frequency (rollup) alert code — a rule that fired because
// a source event repeated within a window. Its code is the source code plus this
// suffix, so it groups distinctly and never re-triggers its own rule.
const freqSuffix = "_FREQ"

// FrequencyCode is the rollup alert code a frequency rule writes for a source
// event code (DMS_SMOKING -> DMS_SMOKING_FREQ).
func FrequencyCode(sourceCode string) string { return sourceCode + freqSuffix }

// LabelFor returns the canonical English display name for an alert code
// (analytics buckets group by code; names carry per-event values).
func LabelFor(code string) string {
	if src, ok := strings.CutSuffix(code, freqSuffix); ok {
		return "Repeated " + LabelFor(src)
	}
	for _, t := range Taxonomy {
		if t.Code == code {
			return t.NameEN
		}
	}
	return code
}

// Taxonomy is the master catalog of alert types (the 18+ rules to replicate).
var Taxonomy = []AlertType{
	{"FUEL_GAIN", "Penambahan Bahan Bakar", "Fuel Gain", SourceGPS, SevImportant, false},
	{"GEOFENCE_ENTER", "Masuk Geolokasi", "Entering Geofence", SourceGPS, SevInfo, false},
	{"GEOFENCE_EXIT", "Keluar Geolokasi", "Exiting Geofence", SourceGPS, SevInfo, false},
	{"EXCEEDED_PARKING", "Melebihi Waktu Parkir", "Exceeded Parking Time", SourceGPS, SevImportant, true},
	{"EXCEEDED_IDLE", "Melebihi Waktu Idle", "Exceeded Idle Time", SourceGPS, SevImportant, true},
	{"HARSH_BRAKING", "Perlambatan Mendadak", "Harsh Braking / Sudden Deceleration", SourceGPS, SevImportant, true},
	{"HARSH_ACCEL", "Akselerasi Mendadak", "Harsh Acceleration / Sudden Acceleration", SourceGPS, SevImportant, true},
	{"SHARP_TURN", "Belokan Tajam", "Sharp Turn", SourceGPS, SevImportant, true},
	{"HARSH_BUMP", "Guncangan Vertikal / Jalan Rusak", "Harsh Bump / Road Shock", SourceGPS, SevImportant, true},
	{"OVERSPEED", "Melebihi Batas Kecepatan", "Speed Over Safety Limit", SourceGPS, SevUrgent, true},
	{"BATTERY_DROP", "Baterai Turun / Terputus", "Battery Drop / Disconnect", SourceGPS, SevUrgent, false},
	{"DMS_SHOCK", "DMS - Guncangan", "Shock", SourceCamera, SevImportant, false},
	{"DMS_CAMERA_BLOCKED", "DMS - Kamera Terhalang", "Camera Blocked", SourceCamera, SevImportant, false},
	{"DMS_SEATBELT", "DMS - Sabuk Pengaman", "Seatbelt", SourceCamera, SevImportant, false},
	{"DMS_OUT_OF_FRAME", "DMS - Wajah Hilang", "Missing Face / Out of Frame", SourceCamera, SevImportant, true},
	{"DMS_DISTRACTION", "DMS - Distraksi", "Distraction", SourceCamera, SevImportant, true},
	{"DMS_YAWN", "DMS - Menguap", "Yawn", SourceCamera, SevImportant, true},
	{"DMS_EYE_CLOSED", "DMS - Mata Tertutup", "Eye Closed", SourceCamera, SevUrgent, true},
	{"DMS_PHONE", "DMS - Menelepon", "Phone Calling", SourceCamera, SevUrgent, true},
	{"DMS_SMOKING", "DMS - Merokok", "Smoking", SourceCamera, SevImportant, true},
	{"DMS_FATIGUE", "DMS - Kelelahan", "Driver Fatigue / Drowsiness", SourceCamera, SevUrgent, true},
	{"DMS_NO_DRIVER", "DMS - Sopir Tidak Ada", "Driver Absent", SourceCamera, SevUrgent, false},
	{"ADAS_LDW", "ADAS - LDW", "Lane Departure Warning", SourceCamera, SevImportant, false},
	{"ADAS_HMW", "ADAS - HMW", "Headway Monitoring Warning", SourceCamera, SevUrgent, false},
	{"ADAS_FCW", "ADAS - FCW", "Forward Collision Warning", SourceCamera, SevUrgent, false},
	{"ADAS_PCW", "ADAS - PCW", "Pedestrian Collision Warning", SourceCamera, SevUrgent, false},
	{"ADAS_LANE_CHANGE", "ADAS - Pindah Lajur", "Frequent Lane Change", SourceCamera, SevImportant, false},
	{"ADAS_ROAD_SIGN", "ADAS - Rambu", "Road-Sign / Speed-Sign Alert", SourceCamera, SevImportant, false},
	{"ADAS_OBSTACLE", "ADAS - Halangan", "Obstacle / Blind Spot", SourceCamera, SevImportant, false},
}

// byCode indexes the taxonomy for quick lookup.
var byCode = func() map[string]AlertType {
	m := make(map[string]AlertType, len(Taxonomy))
	for _, t := range Taxonomy {
		m[t.Code] = t
	}
	return m
}()

// Lookup returns the AlertType for a code (ok=false if unknown).
func Lookup(code string) (AlertType, bool) {
	t, ok := byCode[code]
	return t, ok
}
