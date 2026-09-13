package alert

import "testing"

func sp(s string) *string { return &s }
func ip(i int64) *int64   { return &i }

func TestMatchNoText(t *testing.T) {
	base := func() *coldAlert {
		return &coldAlert{
			CheckStatus: "unchecked", DeviceSource: sp("camera"),
			Severity: sp("urgent"), AlertCode: sp("DMS_FATIGUE"), VehicleID: ip(42),
		}
	}
	tests := []struct {
		name string
		f    alertFilter
		want bool
	}{
		{"empty filter matches", alertFilter{}, true},
		{"check_status match", alertFilter{checkStatus: "unchecked"}, true},
		{"check_status miss", alertFilter{checkStatus: "checked"}, false},
		{"device_source match", alertFilter{deviceSource: "camera"}, true},
		{"device_source miss", alertFilter{deviceSource: "gps"}, false},
		{"severity miss", alertFilter{severity: "info"}, false},
		{"alert_code match", alertFilter{alertCode: "DMS_FATIGUE"}, true},
		{"vehicle match", alertFilter{vehicleID: ip(42)}, true},
		{"vehicle miss", alertFilter{vehicleID: ip(7)}, false},
		{"combined match", alertFilter{checkStatus: "unchecked", severity: "urgent", vehicleID: ip(42)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.matchNoText(base()); got != tt.want {
				t.Errorf("matchNoText = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchNoTextNilFields(t *testing.T) {
	// A row missing severity/device must not match a filter that requires one.
	c := &coldAlert{CheckStatus: "checked"}
	if (alertFilter{severity: "urgent"}).matchNoText(c) {
		t.Error("nil severity should not match a severity filter")
	}
	if (alertFilter{deviceSource: "gps"}).matchNoText(c) {
		t.Error("nil device_source should not match a device filter")
	}
}

func TestMatchQ(t *testing.T) {
	d := DTO{AlertName: sp("Overspeed 92 km/h"), LicensePlate: sp("B-2679-TIZ")}
	cases := []struct {
		q    string
		want bool
	}{
		{"overspeed", true},  // matches alert_name, case-insensitive
		{"2679", true},       // matches plate
		{"b-2679-tiz", true}, // plate, case-insensitive
		{"fatigue", false},   // neither
	}
	for _, c := range cases {
		if got := matchQ(c.q, d); got != c.want {
			t.Errorf("matchQ(%q) = %v, want %v", c.q, got, c.want)
		}
	}
	// A row with no plate must not panic and must not match a plate query.
	if matchQ("2679", DTO{AlertName: sp("Idle")}) {
		t.Error("expected no match when plate is nil")
	}
}
