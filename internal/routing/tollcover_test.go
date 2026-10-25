package routing

import "testing"

// A straight run east along the equator-ish latitude of Java, with gates
// placed on it. Simple geometry on purpose: the cover logic is what is under
// test, not the distance maths.
func route() [][2]float64 {
	var g [][2]float64
	for i := 0; i <= 100; i++ {
		g = append(g, [2]float64{106.0 + float64(i)*0.01, -6.2})
	}
	return g
}

func gate(id int64, name string, lon float64) TollGate {
	return TollGate{ID: id, Name: name, NameNorm: name, Lat: -6.2, Lon: lon}
}

func TestGatesAlongOrdersByTravelNotProximity(t *testing.T) {
	gates := []TollGate{
		gate(3, "c", 106.8),
		gate(1, "a", 106.1),
		gate(2, "b", 106.4),
		// Far from the route: must not be picked up. A generous radius would
		// insert a charge for a gate on a parallel road.
		{ID: 9, Name: "far", NameNorm: "far", Lat: -6.5, Lon: 106.4},
	}
	passed := GatesAlong(route(), gates)
	if len(passed) != 3 {
		t.Fatalf("want 3 gates on the route, got %d", len(passed))
	}
	for i, want := range []string{"a", "b", "c"} {
		if passed[i].Gate.Name != want {
			t.Errorf("position %d: want %s, got %s", i, want, passed[i].Gate.Name)
		}
	}
	if passed[0].AlongM >= passed[1].AlongM || passed[1].AlongM >= passed[2].AlongM {
		t.Error("gates must be ordered by distance along the route")
	}
}

// THE BILLING MODEL. A closed system charges from an entry to the furthest
// exit reached, covering the gates between. Taking the shortest match instead
// would split one transaction into several and overcharge.
func TestCoverTakesTheLongestMatch(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", AmountRp: map[int]int64{5: 20000}, Verdict: "usable"},
		{EntryNorm: "a", ExitNorm: "c", AmountRp: map[int]int64{5: 50000}, Verdict: "usable"},
		{EntryNorm: "b", ExitNorm: "c", AmountRp: map[int]int64{5: 35000}, Verdict: "usable"},
	})
	passed := GatesAlong(route(), []TollGate{
		gate(1, "a", 106.1), gate(2, "b", 106.4), gate(3, "c", 106.8),
	})
	charges, err := table.Cover(passed, 5)
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("a->c covers all three gates in one charge, got %d charges", len(charges))
	}
	if charges[0].AmountRp != 50000 {
		t.Errorf("want the a->c tariff 50000, got %d", charges[0].AmountRp)
	}
}

// Consecutive flat fees are real: one measured journey carried five in a row
// on JORR, each its own charge.
func TestCoverSumsConsecutiveFlatFees(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", Flat: true, AmountRp: map[int]int64{5: 10000}, Verdict: "usable"},
		{EntryNorm: "b", Flat: true, AmountRp: map[int]int64{5: 15000}, Verdict: "usable"},
	})
	passed := GatesAlong(route(), []TollGate{gate(1, "a", 106.1), gate(2, "b", 106.4)})
	charges, err := table.Cover(passed, 5)
	if err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
	var total int64
	for _, c := range charges {
		if !c.Flat {
			t.Error("both charges should be flat")
		}
		total += c.AmountRp
	}
	if total != 25000 {
		t.Errorf("want 25000 across two flat fees, got %d", total)
	}
}

// THE POINT OF THE WHOLE DESIGN. A gate no tariff covers must refuse the
// quote, not return the part it could price. A partial sum is a plausible
// number that is too small, and nothing about it looks wrong.
func TestCoverRefusesRatherThanUndercharging(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", AmountRp: map[int]int64{5: 20000}, Verdict: "usable"},
	})
	passed := GatesAlong(route(), []TollGate{
		gate(1, "a", 106.1), gate(2, "b", 106.4), gate(3, "c", 106.8),
	})
	_, err := table.Cover(passed, 5)
	if err == nil {
		t.Fatal("a gate with no tariff must refuse the quote, not drop the gate")
	}
	// The message must name the gate, so someone knows which cell to source.
	if !contains(err.Error(), "c") {
		t.Errorf("the refusal must name the uncovered gate, got %q", err)
	}
}

// A cell that failed its consistency checks must not reach a quote at all.
// The harvest found one pricing Golongan I above Golongan II; labelling it is
// not enough if it still gets used.
func TestReviewCellsAreExcluded(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", AmountRp: map[int]int64{5: 20000}, Verdict: "review"},
	})
	if closed, flat := table.Cells(); closed != 0 || flat != 0 {
		t.Fatalf("a review cell must not be indexed, got closed=%d flat=%d", closed, flat)
	}
	passed := GatesAlong(route(), []TollGate{gate(1, "a", 106.1), gate(2, "b", 106.4)})
	if _, err := table.Cover(passed, 5); err == nil {
		t.Error("with only a review cell there is no tariff, so the quote must refuse")
	}
}

// A class the cell does not price is reported, never substituted from a
// neighbouring class — II and III share a price on many ruas, which makes
// substitution tempting and wrong.
func TestCoverReportsAMissingGolongan(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", AmountRp: map[int]int64{5: 20000}, Verdict: "usable"},
	})
	passed := GatesAlong(route(), []TollGate{gate(1, "a", 106.1), gate(2, "b", 106.4)})
	if _, err := table.Cover(passed, 3); err == nil {
		t.Error("golongan 3 is unpriced here; that must be reported, not filled in from 5")
	}
}

func TestCoverOfNoGatesIsFree(t *testing.T) {
	table := NewTollTable(nil)
	charges, err := table.Cover(nil, 5)
	if err != nil {
		t.Fatalf("a route passing no gates is toll-free, not an error: %v", err)
	}
	if len(charges) != 0 {
		t.Errorf("want no charges, got %d", len(charges))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
