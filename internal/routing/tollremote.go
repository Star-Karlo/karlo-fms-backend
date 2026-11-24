package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// RemoteToll prices a route by asking the Maps toll service, instead of
// reading toll_gates and toll_tariffs out of this database.
//
// WHY THIS REPLACES InternalToll. Those tables now exist in two places: here,
// from migrations 0058-0065, and in the maps schema of the telemetry database
// that karlo-prod-maps-toll serves. Identical the day they were copied, and
// guaranteed to diverge the moment either is edited — which is the entire
// point of the admin UI being built against the Maps copy.
//
// A divergence here is not a stale cache. TMS prices every order through
// POST /v1/toll/quote and the driver allowance depends on it, so an operator
// correcting a tariff in the admin UI would fix the copy nobody bills from
// while the copy that does bill stayed wrong, with no error logged anywhere.
// One authoritative store is the only version of this that is safe, and the
// Maps service is the one with a way to edit it.
//
// IT FAILS RATHER THAN GUESSES. A network error, a 503 from an unreadable
// tariff table, or a malformed body all return an error — never an empty
// quote. A zero toll from "the service is down" and a zero toll from "this
// road is free" are the same number meaning opposite things, and the caller
// can only tell them apart if one of them is an error. The MAPID fallback
// wired alongside in cmd/api then engages exactly as it did when the tables
// were local and empty.
type RemoteToll struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewRemoteToll builds a client. baseURL is the service root, e.g.
// http://maps-toll.karlo.internal:5009 — Cloud Map, inside the VPC, so no
// public hostname and no TLS is involved.
func NewRemoteToll(baseURL, token string) *RemoteToll {
	return &RemoteToll{
		baseURL: baseURL,
		token:   token,
		// Bounded, because this now sits in the dispatch path. A toll service
		// that accepts a connection and never answers must not hold a
		// workshop dispatch open indefinitely; it must fail so the fallback
		// can run.
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *RemoteToll) Configured() bool { return r != nil && r.baseURL != "" }

func (r *RemoteToll) Name() string { return "maps-toll" }

type remoteTollRequest struct {
	Geometry [][2]float64 `json:"geometry"`
}

// The service's own response shape. Deliberately a separate type rather than
// reusing TollQuote: this is a wire contract with another service, and it
// should break loudly at the boundary if that service changes, not silently
// decode into something that looks right.
type remoteTollResponse struct {
	ByGolongan    map[int]int64 `json:"by_golongan"`
	Segments      []TollSegment `json:"segments"`
	UnpricedGates []string      `json:"unpriced_gates"`
	Complete      bool          `json:"complete"`
	Error         string        `json:"error"`

	// WHO ACTUALLY PRICED IT, added by the Maps gateway.
	//
	// The gateway asks Karlo's own tariff tables first and falls back to
	// MAPID automatically when they cannot price a journey completely. That
	// fallback is the right behaviour — an operator cannot wait for a missing
	// tariff — but it means a figure can come from an aggregator rather than
	// from our own data, and nothing downstream could tell. These two fields
	// are how it says so.
	//
	// Absent when the request went straight to the toll service rather than
	// through the gateway, which is the rollback shape, so neither field can
	// be relied on to be present.
	PricedBy string `json:"priced_by"`
	FellBack bool   `json:"fell_back"`
}

// PriceRoute implements TollQuoter.
func (r *RemoteToll) PriceRoute(ctx context.Context, geometry [][2]float64) (*TollQuote, error) {
	if !r.Configured() {
		return nil, ErrNotConfigured
	}
	// Two points is the minimum the service accepts, and a route with fewer
	// is a caller bug rather than a free journey.
	if len(geometry) < 2 {
		return nil, fmt.Errorf("toll: a route geometry needs at least two points, got %d", len(geometry))
	}

	body, err := json.Marshal(remoteTollRequest{Geometry: geometry})
	if err != nil {
		return nil, fmt.Errorf("toll: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/toll/quote", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("toll: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toll: %s unreachable: %w", r.baseURL, err)
	}
	defer resp.Body.Close()

	var out remoteTollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && resp.StatusCode == http.StatusOK {
		return nil, fmt.Errorf("toll: decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// The service says 503 when its tariff tables are empty or
		// unreadable. Passed up as an error so the fallback engages, with its
		// own sentence kept because it is written to be shown.
		if out.Error != "" {
			return nil, fmt.Errorf("toll: %s: %s", resp.Status, out.Error)
		}
		return nil, fmt.Errorf("toll: %s", resp.Status)
	}

	// An EMPTY by_golongan is not a free journey — it is a route the service
	// could not price for any class. Treated as an error for the same reason
	// a 503 is.
	if len(out.ByGolongan) == 0 && len(out.UnpricedGates) == 0 {
		return nil, fmt.Errorf("toll: %s returned no figures for any class", r.Name())
	}

	return &TollQuote{
		ByGolongan:    out.ByGolongan,
		Segments:      out.Segments,
		UnpricedGates: out.UnpricedGates,
		Provider:      r.Name(),
		PricedBy:      out.PricedBy,
		FellBack:      out.FellBack,
	}, nil
}

var _ TollQuoter = (*RemoteToll)(nil)
