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

	quote := &TollQuote{ByGolongan: map[int]int64{}, Provider: p.Name()}
	unpriced := map[string]bool{}
	for gol := 1; gol <= 5; gol++ {
		res, err := table.Cover(passed, gol)
		if err != nil {
			continue
		}
		for _, u := range res.Unpriced {
			unpriced[u] = true
		}
		var total int64
		for _, c := range res.Charges {
			total += c.AmountRp
		}
		if total <= 0 {
			continue
		}
		quote.ByGolongan[gol] = total
		// Segments are recorded once, from whichever class covered first. They
		// describe the journey's STRUCTURE, which is the same for every class;
		// only the amounts differ, and those are in ByGolongan.
		if len(quote.Segments) == 0 {
			for _, c := range res.Charges {
				quote.Segments = append(quote.Segments, TollSegment{
					FromGate: c.FromGate,
					ToGate:   c.ToGate,
				})
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
