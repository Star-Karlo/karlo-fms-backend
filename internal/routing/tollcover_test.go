package routing

import (
	"testing"
	"time"
)

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
	res, err := table.Cover(passed, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Charges) != 1 {
		t.Fatalf("a->c covers all three gates in one charge, got %d", len(res.Charges))
	}
	if res.Charges[0].AmountRp != 50000 {
		t.Errorf("want the a->c tariff 50000, got %d", res.Charges[0].AmountRp)
	}
	if res.Incomplete() {
		t.Errorf("every gate was covered, so this must not read as incomplete: %v", res.Unpriced)
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
	res, err := table.Cover(passed, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var total int64
	for _, c := range res.Charges {
		if !c.Flat {
			t.Error("both charges should be flat")
		}
		total += c.AmountRp
	}
	if total != 25000 {
		t.Errorf("want 25000 across two flat fees, got %d", total)
	}
}

// A gate no tariff covers is PRICED AT ZERO AND NAMED, so the total is useful
// but cannot be mistaken for complete.
//
// This used to refuse outright, which is right for billing and wrong for the
// screen an operator uses: a 459 km route reported nothing at all because one
// of its nine gates had no cell leading onward. Zero plus a name is more
// useful — and the name is what someone goes and looks up.
func TestCoverPricesWhatItCanAndNamesWhatItCannot(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", AmountRp: map[int]int64{5: 20000}, Verdict: "usable"},
	})
	passed := GatesAlong(route(), []TollGate{
		gate(1, "a", 106.1), gate(2, "b", 106.4), gate(3, "c", 106.8),
	})
	res, err := table.Cover(passed, 5)
	if err != nil {
		t.Fatalf("a missing tariff is a gap, not an error: %v", err)
	}
	if len(res.Charges) != 1 || res.Charges[0].AmountRp != 20000 {
		t.Fatalf("the a->b charge must still be priced, got %+v", res.Charges)
	}
	if !res.Incomplete() {
		t.Fatal("a skipped gate MUST make the result incomplete, or a short total reads as a real one")
	}
	// The name must survive, because it is the work list.
	if len(res.Unpriced) != 1 || res.Unpriced[0] != "c" {
		t.Errorf("want the uncovered gate named as \"c\", got %v", res.Unpriced)
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
	res, _ := table.Cover(passed, 5)
	if len(res.Charges) != 0 {
		t.Error("a review cell must not price anything")
	}
	if !res.Incomplete() {
		t.Error("with only a review cell both gates are unpriced, so this is incomplete")
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
	res, _ := table.Cover(passed, 3)
	if len(res.Charges) != 0 {
		t.Error("golongan 3 must not be filled in from golongan 5")
	}
	if !res.Incomplete() {
		t.Error("an unpriced class must be reported as a gap")
	}
	// The class must appear in the gap, since the pair itself exists.
	if len(res.Unpriced) != 1 || !contains(res.Unpriced[0], "golongan 3") {
		t.Errorf("the gap must name the class, got %v", res.Unpriced)
	}
}

func TestCoverOfNoGatesIsFree(t *testing.T) {
	table := NewTollTable(nil)
	res, err := table.Cover(nil, 5)
	if err != nil {
		t.Fatalf("a route passing no gates is toll-free, not an error: %v", err)
	}
	if len(res.Charges) != 0 {
		t.Errorf("want no charges, got %d", len(res.Charges))
	}
	// AND NOT INCOMPLETE. A toll-free route and an unpriced one must be
	// distinguishable, which is the whole reason the flag exists.
	if res.Incomplete() {
		t.Error("no gates means genuinely toll-free, which is complete")
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

// THE SOURCES DISAGREE 88% OF THE TIME where they overlap, so which one wins
// cannot be left to query order. Jasa Marga publishes its own ruas; MAPID
// derives figures from a route and so depends on which gate the geometry
// snapped to — asked for Cikupa -> Merak it entered at a ramp gate and
// answered 124.500 against a published 104.500.
func TestBetterSourceWins(t *testing.T) {
	cells := []TollCell{
		{EntryNorm: "cikupa", ExitNorm: "merak", Source: "mapid",
			AmountRp: map[int]int64{5: 124500}, Verdict: "usable"},
		{EntryNorm: "cikupa", ExitNorm: "merak", Source: "jasamarga",
			AmountRp: map[int]int64{5: 104500}, Verdict: "usable"},
	}
	// Both orderings, because the bug this guards against is order dependence.
	for _, order := range [][]TollCell{cells, {cells[1], cells[0]}} {
		table := NewTollTable(order)
		passed := GatesAlong(route(), []TollGate{
			gate(1, "cikupa", 106.1), gate(2, "merak", 106.4),
		})
		res, err := table.Cover(passed, 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Charges) != 1 || res.Charges[0].AmountRp != 104500 {
			t.Fatalf("the published tariff must win regardless of row order, got %+v", res.Charges)
		}
	}
}

// A decree outranks both, since it is the legal figure the others approximate.
func TestDecreeOutranksEverything(t *testing.T) {
	if SourcePriority["decree"] <= SourcePriority["jasamarga"] ||
		SourcePriority["jasamarga"] <= SourcePriority["mapid"] {
		t.Fatal("ranking must be decree > jasamarga > mapid")
	}
	// An unlisted source must rank BELOW every known one: something added
	// later without thought should not silently outrank a published tariff.
	if SourcePriority["something-new"] >= SourcePriority["mapid"] {
		t.Error("an unknown source must rank below the known ones")
	}
}

// THE PRODUCTION BUG. A 543 km dispatch reported Golongan I at 365.000 above
// Golongan IV/V at 364.500 — impossible, since Golongan I is the cheapest class
// on every Indonesian tariff.
//
// The cells were all individually fine. Jasa Marga's published dump carries
// 2.397 pairs priced for Golongan I ALONE, so on a long route Golongan I was
// charged at gates the other classes could not be, and the two totals were
// sums of different journeys. Covering once and pricing that structure per
// class is what makes them comparable.
func TestClassesArePricedOverTheSameCharges(t *testing.T) {
	table := NewTollTable([]TollCell{
		// Priced for every class.
		{EntryNorm: "a", ExitNorm: "b", Verdict: "usable", Source: "jasamarga",
			AmountRp: map[int]int64{1: 10000, 2: 15000, 3: 15000, 4: 20000, 5: 20000}},
		// GOLONGAN I ONLY — the shape that caused the inversion.
		{EntryNorm: "b", ExitNorm: "c", Verdict: "usable", Source: "jasamarga",
			AmountRp: map[int]int64{1: 500000}},
	})
	passed := GatesAlong(route(), []TollGate{
		gate(1, "a", 106.1), gate(2, "b", 106.4), gate(3, "c", 106.8),
	})

	st := table.CoverAll(passed)
	if len(st.Cells) != 2 {
		t.Fatalf("both charges must be in the structure, got %d", len(st.Cells))
	}

	// Golongan I can price both, so it totals both.
	total1, _, ok := st.PriceFor(1)
	if !ok || total1 != 510000 {
		t.Errorf("golongan 1 prices both charges: want 510000 ok, got %d %v", total1, ok)
	}
	// Golongan 5 cannot price the second, so it is OMITTED — not given 20000,
	// which would be a total for a shorter journey and would sit below
	// golongan 1's, reading as a sane ordering while describing two different
	// trips.
	if _, _, ok := st.PriceFor(5); ok {
		t.Error("golongan 5 cannot price every charge, so it must be omitted entirely")
	}
}

// And the ordering is asserted rather than assumed, so data that disagrees
// with itself refuses instead of publishing an impossible quote.
func TestCoverAllKeepsStructureIdenticalAcrossClasses(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "b", Verdict: "usable",
			AmountRp: map[int]int64{1: 10000, 5: 20000}},
		{EntryNorm: "b", ExitNorm: "c", Verdict: "usable",
			AmountRp: map[int]int64{1: 5000, 5: 9000}},
	})
	passed := GatesAlong(route(), []TollGate{
		gate(1, "a", 106.1), gate(2, "b", 106.4), gate(3, "c", 106.8),
	})
	st := table.CoverAll(passed)
	_, c1, ok1 := st.PriceFor(1)
	_, c5, ok5 := st.PriceFor(5)
	if !ok1 || !ok5 {
		t.Fatal("both classes price every charge here")
	}
	if len(c1) != len(c5) {
		t.Fatalf("the charge structure must be identical: %d vs %d", len(c1), len(c5))
	}
	for i := range c1 {
		if c1[i].FromGate != c5[i].FromGate || c1[i].ToGate != c5[i].ToGate {
			t.Errorf("charge %d differs between classes: %v vs %v", i, c1[i], c5[i])
		}
	}
}

// RESUMING AT THE EXIT MUST STILL TERMINATE. A cell whose entry and exit
// normalise to the same gate — which the data does contain, since a plaza is
// recorded as both an exit and a re-entry — would otherwise charge itself
// forever.
func TestCoverAllTerminatesOnASelfPair(t *testing.T) {
	table := NewTollTable([]TollCell{
		{EntryNorm: "a", ExitNorm: "a", Verdict: "usable",
			AmountRp: map[int]int64{5: 1000}},
	})
	// Two nodes that normalise to the same gate, as a plaza's out-then-in pair
	// does.
	passed := GatesAlong(route(), []TollGate{gate(1, "a", 106.1), gate(2, "a", 106.4)})
	done := make(chan CoverStructure, 1)
	go func() { done <- table.CoverAll(passed) }()
	select {
	case st := <-done:
		if len(st.Cells) > len(passed) {
			t.Errorf("cannot charge more times than there are gates: %d charges, %d gates",
				len(st.Cells), len(passed))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CoverAll did not terminate — resuming at the exit must still advance")
	}
}
