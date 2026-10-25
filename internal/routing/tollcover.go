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

// Cover works out the charges a gate sequence incurs, for one golongan.
//
// GREEDY LONGEST MATCH, which mirrors how a closed system bills: you take a
// card at an entry and pay at the furthest exit you reach before leaving, so
// the charge spans as many intermediate gates as the table allows. Walking the
// sequence and taking the longest available closed cell from each position
// reproduces that; taking the shortest would split one transaction into
// several and overcharge.
//
// Every gate must be consumed. A gate that begins no closed cell and has no
// flat fee is unexplained, and the whole quote is refused rather than returned
// short — see the type comment for why.
func (t *TollTable) Cover(passed []PassedGate, golongan int) ([]TollCharge, error) {
	if len(passed) == 0 {
		return nil, nil // no gates: a toll-free route, which is an answer
	}
	var charges []TollCharge
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
			amount, ok := bestCell.AmountRp[golongan]
			if !ok {
				return nil, fmt.Errorf(
					"no golongan %d tariff for %s to %s",
					golongan, from.Name, passed[bestJ].Gate.Name)
			}
			charges = append(charges, TollCharge{
				FromGate: from.Name,
				ToGate:   passed[bestJ].Gate.Name,
				AmountRp: amount,
			})
			// Resume AFTER the exit: the gates in between were covered by this
			// one charge, which is what a closed transaction means.
			i = bestJ + 1
			continue
		}

		if cell, ok := t.flat[from.NameNorm]; ok {
			amount, ok := cell.AmountRp[golongan]
			if !ok {
				return nil, fmt.Errorf("no golongan %d flat tariff for %s",
					golongan, from.Name)
			}
			charges = append(charges, TollCharge{
				FromGate: from.Name,
				Flat:     true,
				AmountRp: amount,
			})
			i++
			continue
		}

		// Unexplained. Named, because "toll unavailable" is not actionable and
		// "no tariff for X" tells someone which cell to go and source.
		return nil, fmt.Errorf(
			"no tariff covers %s (gate %d of %d on this route)",
			from.Name, i+1, len(passed))
	}
	return charges, nil
}
