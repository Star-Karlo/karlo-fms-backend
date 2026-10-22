package liveview

import "testing"

// Plate matching is the whole point of the by-plate endpoints: the caller has
// the number as somebody wrote it down, not as the registry stores it.
func TestNormalisePlate(t *testing.T) {
	same := []string{"B 9012 UBC", "B9012UBC", "b-9012-ubc", " b 9012ubc ", "B.9012.UBC"}
	want := NormalisePlate(same[0])
	if want != "B9012UBC" {
		t.Fatalf("NormalisePlate(%q) = %q, want B9012UBC", same[0], want)
	}
	for _, s := range same[1:] {
		if got := NormalisePlate(s); got != want {
			t.Errorf("NormalisePlate(%q) = %q, want %q — these are the same truck", s, got, want)
		}
	}
	// Different trucks must stay different: stripping separators must not
	// strip digits with them.
	if NormalisePlate("B 9012 UBC") == NormalisePlate("B 9013 UBC") {
		t.Error("two different plates normalised to the same value")
	}
	if NormalisePlate("   ") != "" {
		t.Error("a blank plate should normalise to empty, so the lookup can refuse it")
	}
}

// Every field the summary endpoint advertises must be readable, or a caller
// asking for it gets a zero that looks like a measurement.
func TestSummaryFieldsCoverDefaults(t *testing.T) {
	for _, f := range defaultFields {
		if _, ok := summaryFields[f]; !ok {
			t.Errorf("default field %q has no reader in summaryFields", f)
		}
	}
	b := BehaviorSummary{DistanceKm: 321.3, MaxSpeed: 50, HarshBrake: 2, TotalIdleMin: 465}
	if got := summaryFields["distance_km"](&b); got != 321.3 {
		t.Errorf("distance_km read back %v, want 321.3", got)
	}
	if got := summaryFields["harsh_brake"](&b); got != 2 {
		t.Errorf("harsh_brake read back %v, want 2", got)
	}
}
