package routing

import "testing"

// Golongan must never invent a class. 47% of the legacy fleet has no axle
// count, and a defaulted class misprices every truck that has one — invisibly,
// because the number looks precise.
func TestGolonganRefusesToGuess(t *testing.T) {
	for _, axles := range []int{-1, 0, 1} {
		if got := Golongan(axles); got != 0 {
			t.Errorf("axles=%d: want 0 (unknown), got golongan %d", axles, got)
		}
	}
	for axles, want := range map[int]int{2: 2, 3: 3, 4: 4, 5: 5, 6: 5, 8: 5, 12: 5} {
		if got := Golongan(axles); got != want {
			t.Errorf("axles=%d: want golongan %d, got %d", axles, want, got)
		}
	}
}

// An all-zero price object is a route that never joined the toll network, not
// a free road. Two Trans-Sumatra cells came back that way during the harvest,
// each with a single exit gate and no entry; storing those as a tariff would
// assert the road costs nothing.
func TestAllZeroPricesReadAsUnknown(t *testing.T) {
	zero := int64(0)
	p := &mapidTollPrices{G1: &zero, G2: &zero, G3: &zero, G4: &zero, G5: &zero}
	if got := p.toMap(); got != nil {
		t.Errorf("all zeros must read as unknown, got %v", got)
	}

	// A genuine zero alongside real prices is kept: some classes are exempt on
	// some roads, and dropping the whole answer would lose the rest.
	real1, real5 := int64(49000), int64(83500)
	mixed := &mapidTollPrices{G1: &real1, G2: &zero, G5: &real5}
	got := mixed.toMap()
	if got == nil {
		t.Fatal("a mix of zero and non-zero must be kept")
	}
	if got[1] != 49000 || got[5] != 83500 {
		t.Errorf("real prices must survive, got %v", got)
	}
	if _, ok := got[3]; ok {
		t.Errorf("an absent class must stay absent, got %v", got)
	}
}

// Nil, and an object with no classes at all, are both unknown.
func TestEmptyPricesReadAsUnknown(t *testing.T) {
	var nilp *mapidTollPrices
	if got := nilp.toMap(); got != nil {
		t.Errorf("nil prices must read as unknown, got %v", got)
	}
	if got := (&mapidTollPrices{}).toMap(); got != nil {
		t.Errorf("an empty price object must read as unknown, got %v", got)
	}
}
