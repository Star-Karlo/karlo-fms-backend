package routing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Pricing tolls from our own tables, with no external call.
//
// The two halves live in tollcover.go: GatesAlong finds which gates a route
// passes, and TollTable.Cover works out the charges they make up. This wires
// them to the database and caches the result, because the data is small —
// around 200 cells and 250 gates — national, and changes when a ministerial
// decree does, which is roughly every two years.

// TollStore loads the tariff data. Implemented over Postgres; an interface so
// the pricer can be tested without one.
type TollStore interface {
	LoadTollGates(ctx context.Context) ([]TollGate, error)
	LoadTollCells(ctx context.Context) ([]TollCell, error)
}

// InternalToll prices a route from the tariff tables.
type InternalToll struct {
	store TollStore
	ttl   time.Duration

	mu       sync.RWMutex
	gates    []TollGate
	table    *TollTable
	loadedAt time.Time
}

// NewInternalToll builds the pricer. A zero ttl means the default.
func NewInternalToll(store TollStore, ttl time.Duration) *InternalToll {
	if ttl <= 0 {
		// Long, because tariffs move on a two-year review cycle and a reload
		// costs two small queries. Short enough that correcting a cell in the
		// table reaches production the same day without a deploy.
		ttl = 30 * time.Minute
	}
	return &InternalToll{store: store, ttl: ttl}
}

func (p *InternalToll) Name() string { return "internal" }

// load refreshes the cache if stale.
//
// ON A RELOAD FAILURE THE PREVIOUS DATA IS KEPT. A database blip must not turn
// every quote into "no tariff found", which reads as a toll-free route — the
// exact confusion this whole design avoids elsewhere. Stale tariffs are wrong
// by an inflation adjustment; absent ones are wrong by the entire toll.
func (p *InternalToll) load(ctx context.Context) ([]TollGate, *TollTable, error) {
	p.mu.RLock()
	fresh := p.table != nil && time.Since(p.loadedAt) < p.ttl
	gates, table := p.gates, p.table
	p.mu.RUnlock()
	if fresh {
		return gates, table, nil
	}

	g, err := p.store.LoadTollGates(ctx)
	if err == nil {
		var cells []TollCell
		cells, err = p.store.LoadTollCells(ctx)
		if err == nil {
			t := NewTollTable(cells)
			p.mu.Lock()
			p.gates, p.table, p.loadedAt = g, t, time.Now()
			p.mu.Unlock()
			return g, t, nil
		}
	}
	if table != nil {
		return gates, table, nil // keep serving what we had
	}
	return nil, nil, fmt.Errorf("toll tariffs unavailable: %w", err)
}

// PriceRoute prices the journey a route geometry describes.
//
// EVERY GOLONGAN IS COVERED SEPARATELY, and a class that cannot be covered is
// omitted rather than failing the whole quote. The table is built from
// observations and its coverage is uneven — a cell may price Golongan IV and V
// and not I — so an all-or-nothing answer would discard usable prices for the
// classes that matter most. The caller sees which classes are present.
//
// Geometry is [lon, lat], GeoJSON order, as Route.Geometry gives it.
func (p *InternalToll) PriceRoute(ctx context.Context, geometry [][2]float64) (*TollQuote, error) {
	gates, table, err := p.load(ctx)
	if err != nil {
		return nil, err
	}

	passed := GatesAlong(geometry, gates)
	if len(passed) == 0 {
		// A real answer: the route stays off the network. Distinguished from a
		// failure by carrying no error and no prices.
		return &TollQuote{Provider: p.Name()}, nil
	}

	// ONE COVER, THEN PRICED PER CLASS.
	//
	// Covering separately per class and summing whatever each could price
	// produced an impossible answer in production: a 543 km dispatch showed
	// Golongan I at 365.000 above Golongan IV/V at 364.500, when Golongan I is
	// the cheapest class on every Indonesian tariff. The cells were all fine —
	// Jasa Marga's dump has 2.397 pairs priced for Golongan I alone, so on a
	// long route Golongan I was charged at gates the other classes were not,
	// and the totals were sums of different journeys.
	//
	// Covering once and pricing that same structure per class makes the totals
	// comparable by construction, and a class that cannot price every charge
	// is left out rather than given a partial.
	structure := table.CoverAll(passed)

	quote := &TollQuote{ByGolongan: map[int]int64{}, Provider: p.Name()}
	unpriced := map[string]bool{}
	for _, u := range structure.Unpriced {
		unpriced[u] = true
	}
	for gol := 1; gol <= 5; gol++ {
		total, charges, ok := structure.PriceFor(gol)
		if !ok || total <= 0 {
			continue
		}
		quote.ByGolongan[gol] = total
		// Segments describe the journey's structure, which is now identical
		// for every class by construction, so recording them once is correct
		// rather than merely convenient.
		if len(quote.Segments) == 0 {
			for _, c := range charges {
				quote.Segments = append(quote.Segments, TollSegment{
					FromGate: c.FromGate,
					ToGate:   c.ToGate,
				})
			}
		}
	}

	// A class priced where a cheaper one is not would still look odd, so the
	// ordering is checked rather than assumed. Golongan I is the cheapest
	// class on every published Indonesian tariff; if the totals say otherwise
	// something is wrong with the data and the quote should not assert it.
	for a := 1; a <= 5; a++ {
		for b := a + 1; b <= 5; b++ {
			x, okA := quote.ByGolongan[a]
			y, okB := quote.ByGolongan[b]
			if okA && okB && x > y {
				return nil, fmt.Errorf(
					"toll totals are inconsistent: golongan %d (%d) above golongan %d (%d) "+
						"across %d charge(s) — the tariff data for this route disagrees with itself",
					a, x, b, y, len(structure.Cells))
			}
		}
	}

	// THE GAPS, CARRIED NOT HIDDEN.
	//
	// A total missing some of its gates is an understatement, and every one of
	// those gates is a tariff someone can go and look up. So the names travel
	// with the quote: the caller shows them, and they double as the work list.
	if len(unpriced) > 0 {
		for name := range unpriced {
			quote.UnpricedGates = append(quote.UnpricedGates, name)
		}
		sort.Strings(quote.UnpricedGates)
	}

	if len(quote.ByGolongan) == 0 {
		// Gates were found and NOTHING could be priced. Still not an error:
		// the caller wants the gate names, which are the only actionable part.
		// Reported as a zero-price quote that is explicitly incomplete, so it
		// cannot be mistaken for a toll-free route.
		quote.ByGolongan = nil
	}
	return quote, nil
}

// PriceToll satisfies TollPricer for callers that only have endpoints.
//
// NOT USED BY THE DISPATCH, which has the full route geometry and passes it to
// PriceRoute. Two endpoints cannot say which gates lie between them, so this
// can only be right when the straight line happens to follow the road —
// which on a toll journey it does not. It refuses rather than guessing.
func (p *InternalToll) PriceToll(ctx context.Context, points [][2]float64) (*TollQuote, error) {
	if len(points) < 2 {
		return nil, fmt.Errorf("need at least 2 points to price a journey")
	}
	return nil, fmt.Errorf(
		"internal toll pricing needs the route geometry, not just its endpoints: " +
			"two points cannot say which gates lie between them")
}
