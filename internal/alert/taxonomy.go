// Package alert models safety/telematics alerts shared by Control Tower, the
// Notification module, and the Vehicle Alerts History report.
package alert

import "strings"

// DeviceSource classifies where an alert originates.
const (
	SourceGPS    = "gps"
	SourceCamera = "camera"
	SourceSystem = "system" // raised by FMS itself, not by a device
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
//
// Default severity follows BUSINESS IMPACT, not sensor drama (feedback review,
// 2026-09-16):
//
//	urgent    = safety or asset at risk now; someone acts within minutes
//	            (panic, theft, rollover, tamper, collision warnings, driver
//	            asleep or absent, camera covered).
//	important = operational deviation; act today (overspeed, idle/parking over
//	            limit, fatigue signs, phone use, geofence exit).
//	info      = for the record and for scoring; no action expected (single
//	            harsh-driving events, refuels, geofence entry, lane warnings).
//
// A single harsh brake is info; a PATTERN of them is what the frequency rules
// and the driver scorecard surface. Customers can override per rule.
var Taxonomy = []AlertType{
	{"DOCUMENT_EXPIRING", "Dokumen Akan Kedaluwarsa", "Document Expiring", SourceSystem, SevImportant, false},
	// SOP (procedure) checks — thresholds are the company's, see internal/sop.
	{"SOP_OVERSPEED", "Melebihi Batas Kecepatan SOP", "Sustained Overspeed (SOP)", SourceSystem, SevUrgent, true},
	{"SOP_LONG_STOP", "Berhenti Melebihi Batas SOP", "Stopped Too Long (SOP)", SourceSystem, SevImportant, false},
	{"SOP_OFFLINE", "Kendaraan Offline Melebihi Batas SOP", "Offline Too Long (SOP)", SourceSystem, SevImportant, false},
	{"SOP_SENSOR", "Sensor Tidak Terbaca", "Sensor Not Reporting (SOP)", SourceSystem, SevImportant, false},
	{"FUEL_GAIN", "Penambahan Bahan Bakar", "Fuel Gain", SourceGPS, SevInfo, false},
	{"GEOFENCE_ENTER", "Masuk Geolokasi", "Entering Geofence", SourceGPS, SevInfo, false},
	{"GEOFENCE_EXIT", "Keluar Geolokasi", "Exiting Geofence", SourceGPS, SevImportant, false},
	{"EXCEEDED_PARKING", "Melebihi Waktu Parkir", "Exceeded Parking Time", SourceGPS, SevImportant, true},
	{"EXCEEDED_IDLE", "Melebihi Waktu Idle", "Exceeded Idle Time", SourceGPS, SevImportant, true},
	{"HARSH_BRAKING", "Perlambatan Mendadak", "Harsh Braking / Sudden Deceleration", SourceGPS, SevInfo, true},
	{"HARSH_ACCEL", "Akselerasi Mendadak", "Harsh Acceleration / Sudden Acceleration", SourceGPS, SevInfo, true},
	{"SHARP_TURN", "Belokan Tajam", "Sharp Turn", SourceGPS, SevInfo, true},
	{"HARSH_BUMP", "Guncangan Vertikal / Jalan Rusak", "Harsh Bump / Road Shock", SourceGPS, SevInfo, true},
	{"OVERSPEED", "Melebihi Batas Kecepatan", "Speed Over Safety Limit", SourceGPS, SevImportant, true},
	{"ENGINE_OVERREV", "Putaran Mesin Berlebih (RPM)", "Engine Over-Rev (RPM)", SourceGPS, SevImportant, true},
	{"LOW_OIL_PRESSURE", "Tekanan Oli Rendah", "Low Engine Oil Pressure", SourceGPS, SevUrgent, false},
	{"BATTERY_DROP", "Baterai Turun / Terputus", "Battery Drop / Disconnect", SourceGPS, SevUrgent, false},
	{"DMS_SHOCK", "DMS - Guncangan", "Shock", SourceCamera, SevInfo, false},
	// Camera-sourced events that arrived with the corrected Howen VSS mapping.
	// Each is severe enough that a customer will want a rule or a notification
	// on it, which requires a taxonomy entry; the long tail of device
	// housekeeping codes stays out and renders by name only.
	{"PANIC", "Tombol Darurat", "Emergency / Panic Button", SourceCamera, SevUrgent, false},
	{"FUEL_THEFT", "Pencurian Bahan Bakar", "Fuel Theft", SourceCamera, SevUrgent, false},
	{"ROLLOVER", "Kendaraan Terguling", "Rollover", SourceCamera, SevUrgent, false},
	{"TOWING", "Kendaraan Ditarik / Derek", "Towing / Traction", SourceCamera, SevUrgent, false},
	{"POWER_DISCONNECT", "Catu Daya Terputus Mencurigakan", "Suspicious Power Disconnection", SourceCamera, SevUrgent, false},
	{"GPS_JAMMING", "Gangguan Sinyal GPS (Jamming)", "GPS Jamming", SourceCamera, SevUrgent, false},
	{"GSM_JAMMING", "Gangguan Sinyal GSM (Jamming)", "GSM Jamming", SourceCamera, SevUrgent, false},
	{"DMS_AUTH_FAILED", "DMS - Autentikasi Sopir Gagal", "Driver Authentication Failed", SourceCamera, SevImportant, false},
	{"HOD_NO_HANDS", "Kedua Tangan Lepas Setir", "Both Hands Off the Wheel", SourceCamera, SevImportant, true},
	{"LOW_SPEED", "Kecepatan Terlalu Rendah", "Low Speed", SourceCamera, SevInfo, false},
	{"DMS_CAMERA_BLOCKED", "DMS - Kamera Terhalang", "Camera Blocked", SourceCamera, SevUrgent, false},
	{"DMS_SEATBELT", "DMS - Sabuk Pengaman", "Seatbelt", SourceCamera, SevImportant, false},
	{"DMS_OUT_OF_FRAME", "DMS - Wajah Hilang", "Missing Face / Out of Frame", SourceCamera, SevInfo, true},
	{"DMS_DISTRACTION", "DMS - Distraksi", "Distraction", SourceCamera, SevImportant, true},
	{"DMS_YAWN", "DMS - Menguap", "Yawn", SourceCamera, SevInfo, true},
	{"DMS_EYE_CLOSED", "DMS - Mata Tertutup", "Eye Closed", SourceCamera, SevUrgent, true},
	{"DMS_PHONE", "DMS - Menelepon", "Phone Calling", SourceCamera, SevImportant, true},
	{"DMS_SMOKING", "DMS - Merokok", "Smoking", SourceCamera, SevImportant, true},
	{"DMS_FATIGUE", "DMS - Kelelahan", "Driver Fatigue / Drowsiness", SourceCamera, SevImportant, true},
	{"DMS_NO_DRIVER", "DMS - Sopir Tidak Ada", "Driver Absent", SourceCamera, SevUrgent, false},
	{"ADAS_LDW", "ADAS - LDW", "Lane Departure Warning", SourceCamera, SevInfo, false},
	{"ADAS_HMW", "ADAS - HMW", "Headway Monitoring Warning", SourceCamera, SevImportant, false},
	{"ADAS_FCW", "ADAS - FCW", "Forward Collision Warning", SourceCamera, SevUrgent, false},
	{"ADAS_PCW", "ADAS - PCW", "Pedestrian Collision Warning", SourceCamera, SevUrgent, false},
	{"ADAS_LANE_CHANGE", "ADAS - Pindah Lajur", "Frequent Lane Change", SourceCamera, SevInfo, false},
	{"ADAS_ROAD_SIGN", "ADAS - Rambu", "Road-Sign / Speed-Sign Alert", SourceCamera, SevInfo, false},
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
