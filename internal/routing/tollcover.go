package routing

import (
	"fmt"
	"math"
	"sort"
)

// Pricing a route from our own tariff table.
//
// MAPID answered "what does this journey cost" in one call. Doing it in-house
// means doing the two steps it hid:
//
//	1. WHICH GATES does this route pass, and in what order.
//	2. WHICH CHARGES do those gates make up.
//
// Step 2 is not a lookup. A journey is several transactions — measured on a
// real route, Cilegon Barat->Serang Timur at 37.500 plus Ciujung->Cikupa at
// 46.000 is exactly the 83.500 total — and the gates in between do not
// announce which pairs are chargeable. The tariff table does: a pair that
// exists in it is a charge, and one that does not is not. So the gate sequence
// is COVERED by table entries, and the cover is the answer.
//
// THE FAILURE MODE THIS IS BUILT AROUND. A cover that explains some gates and
// not others produces a plausible number that is too small, and nothing about
// it looks wrong. So an incomplete cover is not returned as a partial sum: it
// is returned as a refusal naming the gates it could not explain. A missing
// toll figure is recoverable; a quietly low one is billed.

// TollGate is a gate with a position, from the gates table.
type TollGate struct {
	ID       int64
	Name     string
	NameNorm string
	Lat      float64
	Lon      float64
}

// TollCell is one chargeable transaction in the tariff table.
type TollCell struct {
	// EntryNorm and ExitNorm are normalised gate names. ExitNorm is empty for
	// a flat fee, which has no exit by design.
	EntryNorm string
	ExitNorm  string
	Flat      bool
	// AmountRp per golongan. A class absent here is a class this cell cannot
	// price, which the cover reports rather than substituting a neighbour.
	AmountRp map[int]int64
	Verdict  string
}

// TollTable is the tariff data, indexed for covering.
type TollTable struct {
	// closed is keyed entryNorm -> exitNorm -> cell.
	closed map[string]map[string]TollCell
	flat   map[string]TollCell
}

// NewTollTable indexes cells for lookup.
//
// REVIEW CELLS ARE EXCLUDED, not merely flagged. They failed a consistency
// check — a Golongan I above Golongan II, a direction-dependent price in some
// classes only — and including them would let a known-bad figure into a quote
// on the grounds that it was labelled. A route that needs one is a route we
// cannot price yet, which the cover will say.
func NewTollTable(cells []TollCell) *TollTable {
	t := &TollTable{closed: map[string]map[string]TollCell{}, flat: map[string]TollCell{}}
	for _, c := range cells {
		if c.Verdict != "usable" {
			continue
		}
		if c.Flat {
			t.flat[c.EntryNorm] = c
			continue
		}
		if t.closed[c.EntryNorm] == nil {
			t.closed[c.EntryNorm] = map[string]TollCell{}
		}
		t.closed[c.EntryNorm][c.ExitNorm] = c
	}
	return t
}

// Cells counts what was indexed, for a startup log that says how much of the
// table is actually usable.
func (t *TollTable) Cells() (closed, flat int) {
	for _, m := range t.closed {
		closed += len(m)
	}
	return closed, len(t.flat)
}

// tollGateNearM is how close a route must come to a gate to be counted as
// passing it.
//
// Tight on purpose. Toll plazas sit beside the roads they serve, and a
// generous radius picks up gates on a parallel carriageway or an adjacent ruas
// — which would insert a charge the vehicle never incurred. 80 m admits a
// route whose geometry runs down the centreline past a booth cluster and
// excludes the service road alongside.
const tollGateNearM = 80.0

// PassedGate is a gate the route goes past, with how far along it sits.
type PassedGate struct {
	Gate    TollGate
	AlongM  float64
	OffsetM float64
}

// GatesAlong returns the gates the route passes, in travel order.
//
// Ordered by distance ALONG the route rather than by proximity to either end:
// a route that doubles back — around an interchange, or to reach a gate on the
// far carriageway — would otherwise order its gates wrongly, and the cover
// would then look for charges between gates in an order nobody drove.
func GatesAlong(geometry [][2]float64, gates []TollGate) []PassedGate {
	if len(geometry) < 2 {
		return nil
	}

	// Cumulative distance to each vertex, so a gate's position along the route
	// is known without re-walking the line per gate.
	cum := make([]float64, len(geometry))
	for i := 1; i < len(geometry); i++ {
		cum[i] = cum[i-1] + haversineM(
			geometry[i-1][1], geometry[i-1][0],
			geometry[i][1], geometry[i][0])
	}

	var out []PassedGate
	for _, g := range gates {
		best, bestAt := math.Inf(1), 0.0
		for i := 1; i < len(geometry); i++ {
			d, t := pointToSegmentM(g.Lat, g.Lon,
				geometry[i-1][1], geometry[i-1][0],
				geometry[i][1], geometry[i][0])
			if d < best {
				best = d
				bestAt = cum[i-1] + t*(cum[i]-cum[i-1])
			}
		}
		if best <= tollGateNearM {
			out = append(out, PassedGate{Gate: g, AlongM: bestAt, OffsetM: best})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AlongM < out[j].AlongM })
	return out
}

// pointToSegmentM is the distance from a point to a SEGMENT, plus how far
// along that segment the closest approach lies (0..1).
//
// To the segment and not to its nearest vertex: Valhalla's geometry can run
// hundreds of metres between vertices on a motorway, so a gate sitting exactly
// on the road would measure far from either neighbour and be missed.
func pointToSegmentM(plat, plon, alat, alon, blat, blon float64) (float64, float64) {
	mx := 111320.0 * math.Cos(plat*math.Pi/180)
	my := 110540.0
	px, py := plon*mx, plat*my
	ax, ay := alon*mx, alat*my
	bx, by := blon*mx, blat*my
	dx, dy := bx-ax, by-ay
	len2 := dx*dx + dy*dy
	if len2 == 0 {
		return math.Hypot(px-ax, py-ay), 0
	}
	t := ((px-ax)*dx + (py-ay)*dy) / len2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy)), t
}

// TollCharge is one transaction in a priced journey.
type TollCharge struct {
	FromGate string `json:"from_gate"`
	ToGate   string `json:"to_gate,omitempty"`
	Flat     bool   `json:"flat,omitempty"`
	AmountRp int64  `json:"amount_rp"`
}

// CoverResult is what a gate sequence came to, and what of it is missing.
type CoverResult struct {
	Charges []TollCharge
	// Unpriced names the gates no tariff covered. Those contribute ZERO to the
	// total, so a result with any of them is an UNDERSTATEMENT — never a
	// figure to bill on, and the reason Incomplete exists.
	Unpriced []string
}

// Incomplete reports whether some gate went unpriced.
//
// WHY THIS IS A FLAG AND NOT JUST A SHORTER NUMBER. A partial sum is useful —
// an operator choosing between two workshops wants the figures it does have —
// but Rp 0 for an unpriced gate and Rp 0 for a genuinely toll-free road are
// the same number meaning opposite things. Without this, nobody downstream can
// tell a cheap route from an unmeasured one.
func (c CoverResult) Incomplete() bool { return len(c.Unpriced) > 0 }

// Cover works out the charges a gate sequence incurs, for one golongan.
//
// GREEDY LONGEST MATCH, which mirrors how a closed system bills: you take a
// card at an entry and pay at the furthest exit you reach before leaving, so
// the charge spans as many intermediate gates as the table allows. Walking the
// sequence and taking the longest available closed cell from each position
// reproduces that; taking the shortest would split one transaction into
// several and overcharge.
//
// A GATE NO TARIFF COVERS IS SKIPPED AND NAMED, not fatal. An earlier version
// refused the whole quote, which is right for billing and wrong for the screen
// an operator actually uses: a 459 km route to a workshop reported nothing at
// all because one of its nine gates — Cibitung 4 — had no cell leading onward.
// Pricing eight of nine and saying which one is missing is more useful, and
// the missing names are the work list for sourcing them.
func (t *TollTable) Cover(passed []PassedGate, golongan int) (CoverResult, error) {
	var res CoverResult
	if len(passed) == 0 {
		return res, nil // no gates: a toll-free route, which is an answer
	}
	i := 0
	for i < len(passed) {
		from := passed[i].Gate

		// The furthest exit reachable from here that the table prices.
		bestJ, bestCell := -1, TollCell{}
		if exits := t.closed[from.NameNorm]; exits != nil {
			for j := len(passed) - 1; j > i; j-- {
				if cell, ok := exits[passed[j].Gate.NameNorm]; ok {
					bestJ, bestCell = j, cell
					break
				}
			}
		}
		if bestJ > 0 {
			if amount, ok := bestCell.AmountRp[golongan]; ok {
				res.Charges = append(res.Charges, TollCharge{
					FromGate: from.Name,
					ToGate:   passed[bestJ].Gate.Name,
					AmountRp: amount,
				})
				// Resume AFTER the exit: the gates in between were covered by
				// this one charge, which is what a closed transaction means.
				i = bestJ + 1
				continue
			}
			// The pair exists but not for this class. Named with the class, so
			// the gap is specific: II and III share a price on many ruas,
			// which makes substituting one for the other tempting and wrong.
			res.Unpriced = append(res.Unpriced,
				fmt.Sprintf("%s to %s (golongan %d)",
					from.Name, passed[bestJ].Gate.Name, golongan))
			i = bestJ + 1
			continue
		}

		if cell, ok := t.flat[from.NameNorm]; ok {
			if amount, ok := cell.AmountRp[golongan]; ok {
				res.Charges = append(res.Charges, TollCharge{
					FromGate: from.Name,
					Flat:     true,
					AmountRp: amount,
				})
				i++
				continue
			}
			res.Unpriced = append(res.Unpriced,
				fmt.Sprintf("%s flat fee (golongan %d)", from.Name, golongan))
			i++
			continue
		}

		// No cell starts here at all. Named plainly, because this is the form
		// the work list takes: "Cibitung 4" is a gate someone can look up.
		res.Unpriced = append(res.Unpriced, from.Name)
		i++
	}
	return res, nil
}
