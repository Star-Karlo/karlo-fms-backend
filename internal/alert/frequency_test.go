package alert

import "testing"

func TestFrequencyCode(t *testing.T) {
	got := FrequencyCode("DMS_SMOKING")
	if got != "DMS_SMOKING_FREQ" {
		t.Fatalf("FrequencyCode = %q", got)
	}
	// A rollup code must never equal its source, or a frequency rule would count
	// its own escalations and re-fire forever.
	if got == "DMS_SMOKING" {
		t.Fatal("rollup code collides with source code")
	}
}

func TestLabelFor_Frequency(t *testing.T) {
	if got := LabelFor("DMS_SMOKING_FREQ"); got != "Repeated Smoking" {
		t.Fatalf("LabelFor rollup = %q, want %q", got, "Repeated Smoking")
	}
	// A plain code is unchanged.
	if got := LabelFor("DMS_SMOKING"); got != "Smoking" {
		t.Fatalf("LabelFor source = %q", got)
	}
	// An unknown code falls back to itself.
	if got := LabelFor("MYSTERY"); got != "MYSTERY" {
		t.Fatalf("LabelFor unknown = %q", got)
	}
}
