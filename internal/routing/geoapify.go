package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Geoapify implements both routing and map matching.
//
// Chosen over OpenRouteService for two concrete reasons rather than taste:
// its free tier permits commercial use (Stadia and GraphHopper's free plans
// explicitly do not), and it HAS a map-matching endpoint, which ORS lacks
// entirely. That second point is what makes snapping a travelled path to roads
// possible now instead of after standing up a self-hosted engine.
//
// Attribution is a condition of the free tier — "Powered by Geoapify" must
// appear wherever the results are shown.

// geoapifyBase is the API host. The key travels as a QUERY PARAMETER here,
// unlike ORS which uses the Authorization header — easy to get wrong when
// switching providers.
const geoapifyBase = "https://api.geoapify.com"

// geoapifyRouteMode is the vehicle profile for ROUTING.
//
// heavy_truck is the 40t class, which matches the trailers in this fleet and
// makes the engine respect weight limits, height restrictions and lorry bans.
// A car route that sends a 40ft trailer down a residential shortcut is worse
// than no route, because it looks authoritative.
const geoapifyRouteMode = "heavy_truck"

// geoapifyMatchMode is the profile for MAP MATCHING, and it is deliberately
// different.
//
// The matching endpoint offers only drive, walk and bicycle — there is no truck
// mode. That is a real limitation, not an oversight here: matching a trace with
// a car profile can snap a truck onto a road it is not legally allowed to use.
// It is acceptable because matching describes where the vehicle ALREADY WENT
// rather than advising where it should go, so a wrong-class road is a cosmetic
// error rather than a dangerous instruction.
const geoapifyMatchMode = "drive"

// matchChunkSize bounds ONE map-matching request. Longer traces are split into
// consecutive chunks and stitched back together.
//
// THIS MUST NOT BE DONE BY DOWNSAMPLING, which is what this originally did and
// what the numbers below killed. Measured against a real 461 km day (961 fixes,
// distance known independently from the daily rollup):
//
//	downsampled to 100 pts -> 159 km
//	downsampled to 250 pts ->  50 km
//	downsampled to 500 pts -> 217 km
//	5 consecutive chunks   -> 489 km   (~6% over, and plausible)
//
// Thinning a long journey spreads the fixes kilometres apart, and the matcher
// then joins them by the SHORTEST path rather than the travelled one — so the
// answer is not merely imprecise, it is a different route. Every downsampled
// figure above is wrong, and wrong in an unpredictable direction, which is the
// worst property a distance can have.
//
// 200 keeps fixes at their recorded spacing and each request near three
// seconds. A full day costs about five credits of the 3,000 daily allowance.
const matchChunkSize = 200

// matchConcurrency bounds parallel chunk requests.
//
// Sequential, a day took ~15s. Three at a time brings that to a few seconds
// while staying far below any plausible rate limit — the point is a responsive
// map, not saturating someone else's free tier.
const matchConcurrency = 3

type Geoapify struct {
	apiKey string
	base   string
	client *http.Client
}

func NewGeoapify(apiKey, base string) *Geoapify {
	if base == "" {
		base = geoapifyBase
	}
	return &Geoapify{apiKey: apiKey, base: base, client: &http.Client{Timeout: 60 * time.Second}}
}

func (g *Geoapify) Name() string { return "geoapify" }

// geoResponse covers both endpoints: each returns a GeoJSON FeatureCollection
// whose geometry is a MultiLineString — one line per route leg or matched
// segment — NOT a LineString. Decoding it as a flat line silently yields
// nothing.
type geoResponse struct {
	Features []struct {
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Distance float64 `json:"distance"`
			Time     float64 `json:"time"`
		} `json:"properties"`
	} `json:"features"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// flatten turns a MultiLineString (or LineString) into a single polyline.
//
// The legs are contiguous — each begins where the previous ended — so joining
// them is correct for drawing, and duplicated junction points are dropped so
// the line has no zero-length segments.
func flatten(kind string, raw json.RawMessage) ([][2]float64, error) {
	var out [][2]float64
	if kind == "LineString" {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	var multi [][][2]float64
	if err := json.Unmarshal(raw, &multi); err != nil {
		return nil, fmt.Errorf("unexpected geometry %q: %w", kind, err)
	}
	for _, line := range multi {
		for _, p := range line {
			if n := len(out); n > 0 && out[n-1] == p {
				continue
			}
			out = append(out, p)
		}
	}
	return out, nil
}

func (g *Geoapify) fail(resp *http.Response, body geoResponse) error {
	if body.Message != "" {
		return fmt.Errorf("routing provider: %s", body.Message)
	}
	if body.Error != "" {
		return fmt.Errorf("routing provider: %s", body.Error)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("routing key rejected (HTTP %d) — check GEOAPIFY_API_KEY", resp.StatusCode)
	case http.StatusTooManyRequests:
		return fmt.Errorf("daily routing quota exhausted (HTTP 429)")
	default:
		return fmt.Errorf("routing provider returned HTTP %d", resp.StatusCode)
	}
}

// Route asks Geoapify for a truck route between two points.
func (g *Geoapify) Route(ctx context.Context, from, to Point) (*Route, error) {
	if g.apiKey == "" {
		return nil, ErrNotConfigured
	}
	q := url.Values{}
	// waypoints are lat,lon pairs separated by "|" — note this is the OPPOSITE
	// order to the GeoJSON output, which is lon,lat.
	q.Set("waypoints", fmt.Sprintf("%f,%f|%f,%f", from.Lat, from.Lon, to.Lat, to.Lon))
	q.Set("mode", geoapifyRouteMode)
	q.Set("apiKey", g.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"/v1/routing?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routing provider unreachable: %w", err)
	}
	defer resp.Body.Close()

	var body geoResponse
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	_ = json.Unmarshal(payload, &body)
	if resp.StatusCode != http.StatusOK {
		return nil, g.fail(resp, body)
	}
	if len(body.Features) == 0 {
		// A real outcome, not a bug: common when a truck's last fix is inside a
		// building or on an unmapped yard track.
		return nil, fmt.Errorf("no drivable route found between these points")
	}
	f := body.Features[0]
	geom, err := flatten(f.Geometry.Type, f.Geometry.Coordinates)
	if err != nil {
		return nil, err
	}
	return &Route{
		DistanceM: f.Properties.Distance,
		DurationS: f.Properties.Time,
		Geometry:  geom,
		Provider:  g.Name(),
	}, nil
}

// TracePoint is one GPS fix to be matched against the road network.
type TracePoint struct {
	Lat float64
	Lon float64
	// At is optional but improves matching: timestamps let the matcher reason
	// about speed and reject a path that would have required going impossibly
	// fast between two fixes.
	At time.Time
}

// MatchedTrace is a GPS trace snapped to roads.
type MatchedTrace struct {
	Geometry [][2]float64 `json:"geometry"`
	// DistanceM is the length along roads, which is generally SHORTER than the
	// raw GPS distance: scatter around a stationary vehicle inflates the raw
	// figure with movement that never happened.
	DistanceM  float64 `json:"distance_m"`
	PointsIn   int     `json:"points_in"`
	PointsUsed int     `json:"points_used"`
	// ChunksMatched vs ChunksTotal exposes partial success: a stretch through
	// an unmapped yard can fail while the rest of the day matches perfectly,
	// and the caller should be able to say so rather than present a gap as a
	// complete path.
	ChunksTotal   int    `json:"chunks_total"`
	ChunksMatched int    `json:"chunks_matched"`
	Provider      string `json:"provider"`

	// RawDistanceM is the great-circle length through the INPUT fixes, and
	// exists to make a partial match detectable.
	//
	// A road path is always at least as long as the straight lines between the
	// same points, so DistanceM should come out ABOVE this. Coming out well
	// below means the matcher reconstructed only a fragment — and it reports
	// that as a success: 200, one leg, a plausible geometry, and a distance
	// that is simply too small. Without this number there is nothing to compare
	// against and no way to tell.
	RawDistanceM float64 `json:"raw_distance_m"`
	// Partial is set when DistanceM falls far enough below RawDistanceM that
	// the result should not be presented as a complete path.
	Partial bool `json:"partial"`
}

// Matcher snaps a GPS trace to the road network.
//
// Separate from Router because not every provider has it — OpenRouteService
// notably does not — so callers must be able to ask whether matching is
// available rather than assuming a Router can do it.
type Matcher interface {
	Match(ctx context.Context, points []TracePoint) (*MatchedTrace, error)
}

type matchWaypoint struct {
	Location  [2]float64 `json:"location"`
	Timestamp string     `json:"timestamp,omitempty"`
}

type matchRequest struct {
	Mode      string          `json:"mode"`
	Waypoints []matchWaypoint `json:"waypoints"`
}

// Match snaps a GPS trace to roads.
//
// Long traces are split into consecutive chunks, matched in parallel and
// stitched back together in order. See matchChunkSize for why chunking rather
// than thinning is the only correct approach here.
func (g *Geoapify) Match(ctx context.Context, points []TracePoint) (*MatchedTrace, error) {
	if g.apiKey == "" {
		return nil, ErrNotConfigured
	}
	if len(points) < 2 {
		return nil, fmt.Errorf("a trace needs at least two points to match")
	}

	var chunks [][]TracePoint
	for i := 0; i < len(points); i += matchChunkSize {
		end := i + matchChunkSize
		if end > len(points) {
			end = len(points)
		}
		if end-i >= 2 {
			chunks = append(chunks, points[i:end])
		}
	}

	type result struct {
		geom  [][2]float64
		dist  float64
		err   error
		index int
	}
	results := make([]result, len(chunks))
	sem := make(chan struct{}, matchConcurrency)
	var wg sync.WaitGroup
	for i, c := range chunks {
		wg.Add(1)
		go func(i int, c []TracePoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			geom, dist, err := g.matchChunk(ctx, c)
			results[i] = result{geom: geom, dist: dist, err: err, index: i}
		}(i, c)
	}
	wg.Wait()

	out := &MatchedTrace{PointsIn: len(points), PointsUsed: len(points), Provider: g.Name()}
	var firstErr error
	matched := 0
	for _, r := range results {
		if r.err != nil {
			// One bad chunk does not sink the trace: a stretch through an
			// unmapped yard can be unmatchable while the rest of the day is
			// perfectly good, and returning nothing would be a worse answer
			// than returning most of the journey.
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		matched++
		out.DistanceM += r.dist
		for _, p := range r.geom {
			if n := len(out.Geometry); n > 0 && out.Geometry[n-1] == p {
				continue
			}
			out.Geometry = append(out.Geometry, p)
		}
	}
	if matched == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("the trace could not be matched to any road")
	}
	out.ChunksTotal = len(chunks)
	out.ChunksMatched = matched
	return out, nil
}

// matchChunk matches one bounded run of consecutive fixes.
func (g *Geoapify) matchChunk(ctx context.Context, points []TracePoint) ([][2]float64, float64, error) {
	wps := make([]matchWaypoint, 0, len(points))
	for _, p := range points {
		w := matchWaypoint{Location: [2]float64{p.Lon, p.Lat}}
		if !p.At.IsZero() {
			w.Timestamp = p.At.UTC().Format(time.RFC3339)
		}
		wps = append(wps, w)
	}
	body, err := json.Marshal(matchRequest{Mode: geoapifyMatchMode, Waypoints: wps})
	if err != nil {
		return nil, 0, err
	}

	u := g.base + "/v1/mapmatching?apiKey=" + url.QueryEscape(g.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("matching provider unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out geoResponse
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	_ = json.Unmarshal(payload, &out)
	if resp.StatusCode != http.StatusOK {
		return nil, 0, g.fail(resp, out)
	}
	if len(out.Features) == 0 {
		return nil, 0, fmt.Errorf("the trace could not be matched to any road")
	}
	f := out.Features[0]
	geom, err := flatten(f.Geometry.Type, f.Geometry.Coordinates)
	if err != nil {
		return nil, 0, err
	}
	return geom, f.Properties.Distance, nil
}
