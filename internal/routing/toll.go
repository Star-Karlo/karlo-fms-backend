package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Toll pricing for a journey.
//
// WHY THIS CALLS MAPID RATHER THAN A TABLE WE OWN.
//
// A tariff is keyed on (entry gate, exit gate, golongan), and we have harvested
// 206 such cells — 179 closed pairs and 27 flat-fee gates, 192 of them
// internally consistent. But pricing a NEW journey from that table needs a step
// the table does not provide: working out which gates a route passes, in order,
// and whether each is an entry or an exit. That is the error-prone half, and
// getting it wrong produces a confident wrong number rather than a failure.
//
// MAPID answers the whole question in one call, stays current when tariffs rise
// (they are reviewed every two years under UU 2/2022), and is a sister company,
// so there is no cost pressure to avoid it. The harvested table's job is to
// CHECK this, not to replace it — and it has already caught real upstream
// errors: Kramasan->Lambu Kibang prices Golongan I above Golongan II, and
// Kanci<->Cikampek differs ~10% by direction in classes II-V while I matches.
//
// So: price live, and keep the table as the second opinion.

// TollQuoter prices the journey a ROUTE describes, rather than the one between
// two endpoints.
//
// The distinction matters and is not pedantic: a toll is incurred at the gates
// a route passes, and two endpoints do not say which those are. Any pricer
// working from our own tables needs this one; a provider that does its own
// routing can take the endpoints instead.
type TollQuoter interface {
	// PriceRoute prices a route geometry, [lon, lat] in travel order.
	PriceRoute(ctx context.Context, geometry [][2]float64) (*TollQuote, error)
}

// TollPricer answers what a journey costs in tolls.
type TollPricer interface {
	// PriceToll returns the toll for travelling through points, in travel
	// order. Points are [lon, lat] — GeoJSON order.
	PriceToll(ctx context.Context, points [][2]float64) (*TollQuote, error)
}

// TollQuote is a journey's toll, per vehicle class.
type TollQuote struct {
	// ByGolongan maps the Indonesian vehicle class (1..5) to Rupiah.
	//
	// ALL FIVE ARE RETURNED, not just the one the vehicle belongs to. A truck
	// whose axle count is unrecorded — 47% of the fleet in the legacy master
	// data — cannot be assigned a class, and answering with a single figure
	// would mean picking one silently. A caller with an axle count reads its
	// own; one without can show the range and say which it is.
	ByGolongan map[int]int64 `json:"by_golongan"`

	// Segments are the individual charges making up the total, in travel
	// order. A journey is NOT one transaction: measured on a real route,
	// Cilegon Barat->Serang Timur at 37.500 plus Ciujung->Cikupa at 46.000 is
	// exactly the 83.500 total. Surfaced because a disputed figure is argued
	// about per gate, not in aggregate.
	Segments []TollSegment `json:"segments,omitempty"`

	Provider string `json:"provider"`
}

// TollSegment is one toll charge within a journey.
type TollSegment struct {
	// FromGate is empty on a flat-fee charge. An open or integrated system —
	// JORR, for instance — takes one tap and a fixed amount, so there is no
	// exit to name. Those are real charges, not truncated records: one
	// measured journey carried five of them in a row.
	FromGate   string        `json:"from_gate,omitempty"`
	ToGate     string        `json:"to_gate,omitempty"`
	ByGolongan map[int]int64 `json:"by_golongan,omitempty"`
}

// Golongan is the toll class for a goods vehicle, from its axle count.
//
// Per BPJT: II is two axles, III three, IV four, V five or more. Golongan I
// covers cars, buses and small trucks ("pick up / truk kecil"), which a
// two-axle count CANNOT distinguish from a two-axle lorry — that needs the
// vehicle's size or type. So two axles returns II here and a caller that knows
// better may override it.
//
// Returns 0 for an unusable axle count, which callers must treat as unknown
// rather than defaulting: a defaulted class misprices every truck that has one.
func Golongan(axles int) int {
	switch {
	case axles == 2:
		return 2
	case axles == 3:
		return 3
	case axles == 4:
		return 4
	case axles >= 5:
		return 5
	default:
		return 0
	}
}

// --- MAPID ------------------------------------------------------------------

// MAPIDToll prices tolls through MAPID's routing service.
type MAPIDToll struct {
	baseURL string
	key     string
	client  *http.Client
}

// NewMAPIDToll builds the pricer. An empty base URL or key disables it, and
// PriceToll then answers ErrNotConfigured rather than failing obscurely.
//
// THE PATH HAS A CAPITAL V. The live endpoint is /V2/route_n_toll, taking
// {profile, points}. TMS still calls /route_n_toll with {points, profile,
// tollType}, which now 404s — discovered by a harvest run that failed every
// call. Kept as a constant here so the next caller cannot repeat it.
func NewMAPIDToll(baseURL, key string) *MAPIDToll {
	if baseURL == "" {
		baseURL = "https://routing.mapid.io"
	}
	return &MAPIDToll{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		// Generous: this sits behind an operator sending a truck for repair,
		// and a toll figure is worth a second's wait. Not unbounded, because
		// the dispatch must still record without it.
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

const mapidTollPath = "/V2/route_n_toll"

func (m *MAPIDToll) configured() bool { return m != nil && m.key != "" }

type mapidTollRequest struct {
	Profile string       `json:"profile"`
	Points  [][2]float64 `json:"points"`
}

// mapidTollPrices is MAPID's per-class price object.
type mapidTollPrices struct {
	G1 *int64 `json:"golongan_1"`
	G2 *int64 `json:"golongan_2"`
	G3 *int64 `json:"golongan_3"`
	G4 *int64 `json:"golongan_4"`
	G5 *int64 `json:"golongan_5"`
}

// toMap drops absent classes AND an all-zero answer.
//
// ZERO IS NOT A TARIFF. MAPID answers a route it found no toll on with zeros,
// and a caller that stored those would be asserting the road is free. Two
// Trans-Sumatra cells came back that way during the harvest, each with a gate
// sequence holding a single exit and no entry — a route that never joined the
// network, which is unknown rather than free.
func (p *mapidTollPrices) toMap() map[int]int64 {
	if p == nil {
		return nil
	}
	out := map[int]int64{}
	for g, v := range map[int]*int64{1: p.G1, 2: p.G2, 3: p.G3, 4: p.G4, 5: p.G5} {
		if v != nil {
			out[g] = *v
		}
	}
	if len(out) == 0 {
		return nil
	}
	any := false
	for _, v := range out {
		if v > 0 {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	return out
}

type mapidTollGate struct {
	Name string `json:"name"`
	Gate string `json:"gate"` // "in" or "out"
}

type mapidTollResponse struct {
	Prices  *mapidTollPrices `json:"prices"`
	Details []struct {
		From  *mapidTollGate   `json:"from"`
		To    *mapidTollGate   `json:"to"`
		Price *mapidTollPrices `json:"price"`
	} `json:"details"`
	Distance float64 `json:"distance"`
	// MAPID reports a refusal as a body, not only a status.
	Message string `json:"message"`
}

func (m *MAPIDToll) Name() string { return "mapid" }

// PriceToll prices a journey through the given points.
func (m *MAPIDToll) PriceToll(ctx context.Context, points [][2]float64) (*TollQuote, error) {
	if !m.configured() {
		return nil, ErrNotConfigured
	}
	if len(points) < 2 {
		return nil, fmt.Errorf("need at least 2 points to price a journey")
	}

	body, err := json.Marshal(mapidTollRequest{Profile: "truck", Points: points})
	if err != nil {
		return nil, err
	}
	// The key goes in the query string because that is what the service
	// accepts; it is never logged, and the URL is not returned to callers.
	url := m.baseURL + mapidTollPath + "?key=" + m.key
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toll pricing unavailable: %w", err)
	}
	defer res.Body.Close()

	var out mapidTollResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("toll pricing returned an unreadable answer (HTTP %d)", res.StatusCode)
	}
	if out.Message != "" {
		return nil, fmt.Errorf("toll pricing: %s", out.Message)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toll pricing: HTTP %d", res.StatusCode)
	}

	total := out.Prices.toMap()
	if total == nil {
		// No toll on this route. A legitimate answer for a journey that stays
		// off the network, and the caller must be able to tell it apart from a
		// failure — hence a quote with no prices rather than an error.
		return &TollQuote{Provider: m.Name()}, nil
	}

	q := &TollQuote{ByGolongan: total, Provider: m.Name()}
	for _, d := range out.Details {
		seg := d.Price.toMap()
		if seg == nil {
			// A detail without a price is a route-start or route-end gate
			// marker rather than a charge. Skipped, not recorded as free.
			continue
		}
		s := TollSegment{ByGolongan: seg}
		if d.From != nil {
			s.FromGate = d.From.Name
		}
		if d.To != nil {
			s.ToGate = d.To.Name
		}
		q.Segments = append(q.Segments, s)
	}
	return q, nil
}
