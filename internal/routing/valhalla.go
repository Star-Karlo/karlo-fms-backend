package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"
)

// Valhalla is a self-hosted routing engine on an Indonesia OSM extract.
//
// WHY THIS EXISTS. The hosted providers meter every call, which is what makes
// routing something to ration rather than something to use. Self-hosting
// removes the meter, and at Karlo's volume the saving is the point: see
// docs/eta-analysis-task.md for the numbers.
//
// WHAT IT IS NOT FOR. Nobody is being told which exit to take. This computes
// lane kilometres for pricing, identifies which toll roads a lane uses, and
// gives a reference path to compare a real GPS trace against. A few percent out
// is acceptable, and that tolerance is what makes self-hosting realistic in the
// first place.
//
// It is the only provider here that does BOTH routing and map matching, which
// is why it supersedes the arrangement in cmd/api/main.go where matching had to
// be wired from a Geoapify key independently of the routing choice.
//
// Valhalla knows which roads are tolled (OSM `toll=yes`) but holds no tariffs.
// Toll PRICES come from BPJT and are a separate piece of work; nothing here
// should be read as pricing a gate.
type Valhalla struct {
	baseURL string
	token   string
	client  *http.Client
	truck   *TruckOptions
}

// NewValhalla returns a router against a Valhalla HTTP service, e.g.
// http://fms-routing.karlo.internal:5008.
func NewValhalla(baseURL string) *Valhalla {
	return &Valhalla{
		baseURL: trimSlash(baseURL),
		// Generous: a cold Valhalla answers a long cross-Java route in a second
		// or two, but the first request after a restart pages tiles in from
		// disk and is markedly slower.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (v *Valhalla) Name() string { return "valhalla" }

// TruckOptions are Valhalla's heavy-vehicle costing parameters.
//
// THE SEAM FOR TRUCK-AWARE ROUTING, left deliberately. Valhalla already routes
// around height, weight and axle limits when told the vehicle's dimensions; the
// blocker is that FMS does not record them — `vehicles` carries `vehicle_type`
// and `box_type` as free text and no dimensions at all. So this starts as a
// fleet-wide default and becomes per-vehicle once that master data exists,
// which is a data change rather than a rewrite.
//
// Zero fields are omitted, and Valhalla then applies its own truck defaults.
// Units are metres and metric tonnes.
type TruckOptions struct {
	Height    float64 `json:"height,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Length    float64 `json:"length,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
	AxleLoad  float64 `json:"axle_load,omitempty"`
	AxleCount int     `json:"axle_count,omitempty"`
	Hazmat    bool    `json:"hazmat,omitempty"`
	// UseTolls: 0 avoids toll roads, 1 is indifferent. Left nil-by-default
	// rather than set to 0, because a truck that avoids every toll road is NOT
	// the fleet's behaviour and biasing the reference path would corrupt the
	// very comparison this engine is being stood up to make.
	UseTolls *float64 `json:"use_tolls,omitempty"`
}

// WithToken sets the bearer token the service expects.
//
// REQUIRED WHENEVER THE SERVICE HAS A PUBLIC HOSTNAME. It gates every routing
// call there, because a routing engine reachable by name is free compute for
// anyone who finds it. Without a token the service answers 401 — instantly,
// and with no network symptom to suggest connectivity — which surfaced here as
// "no part of the trace could be matched to roads" on an endpoint that had
// never been reached at all.
//
// Empty when the service runs unauthenticated, which is correct for a
// VPC-internal deployment with no hostname.
func (v *Valhalla) WithToken(t string) *Valhalla {
	v.token = t
	return v
}

// WithTruck sets fleet-wide heavy-vehicle costing. Returns the receiver so it
// composes at construction.
func (v *Valhalla) WithTruck(o TruckOptions) *Valhalla {
	v.truck = &o
	return v
}

// valhallaCosting is "truck", not "auto". A car route happily ignores the
// weight limits, height restrictions and lorry bans that decide where a 40ft
// trailer can actually go, and a route that sends one down a residential
// shortcut is worse than no route because it looks authoritative.
const valhallaCosting = "truck"

type valhallaLocation struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// Type "break" permits a stop (and a U-turn) at this location; the default
	// "via" forbids it and can force a long detour at an endpoint sitting on
	// the wrong side of a divided road.
	Type string `json:"type,omitempty"`
}

type valhallaCostingOptions struct {
	Truck *TruckOptions `json:"truck,omitempty"`
}

type valhallaRouteRequest struct {
	Locations      []valhallaLocation      `json:"locations"`
	Costing        string                  `json:"costing"`
	CostingOptions *valhallaCostingOptions `json:"costing_options,omitempty"`
	Units          string                  `json:"units"`
}

type valhallaTraceRequest struct {
	Shape          []valhallaShapePoint    `json:"shape"`
	Costing        string                  `json:"costing"`
	CostingOptions *valhallaCostingOptions `json:"costing_options,omitempty"`
	Units          string                  `json:"units"`
	// ShapeMatch "map_snap" lets Valhalla find the roads itself. The
	// alternative, "edge_walk", assumes the input already follows the network
	// and fails outright on ordinary GPS scatter.
	ShapeMatch   string                `json:"shape_match"`
	TraceOptions *valhallaTraceOptions `json:"trace_options,omitempty"`
}

// valhallaTraceOptions tune map matching for OUR sampling rate, which is far
// sparser than the second-by-second input Valhalla's defaults assume.
type valhallaTraceOptions struct {
	BreakageDistance float64 `json:"breakage_distance,omitempty"`
	SearchRadius     float64 `json:"search_radius,omitempty"`
	GPSAccuracy      float64 `json:"gps_accuracy,omitempty"`
}

// valhallaBreakageDistanceM is the furthest apart two consecutive fixes may be
// before Valhalla refuses to join them.
//
// ITS DEFAULT IS 2.000 m, AND THAT IS UNUSABLE HERE. Our trackers report around
// every 50 seconds, which at 60 km/h is already ~830 m, and any ordinary gap —
// a tunnel, a few minutes of lost signal — exceeds two kilometres immediately.
// With the default, matching fails outright:
//
//	{"error_code":172,"error":"Exceeded breakage distance for all pairs: 2000 meters"}
//
// 20 km tolerates roughly a quarter-hour gap at motorway speed. Beyond that a
// "gap" is not a gap but a genuine break in the record, and joining across it
// would invent a path the truck never drove — which is why the chunker below
// splits on time instead of letting this number grow.
const valhallaBreakageDistanceM = 20000

// valhallaSearchRadiusM is how far from a fix Valhalla may look for a road.
// Generous enough for consumer-grade GPS sitting beside a carriageway, without
// reaching across to a parallel service road.
const valhallaSearchRadiusM = 100

// valhallaGPSAccuracyM describes how much the fixes themselves can be trusted,
// which is what lets the matcher prefer a plausible road over the nearest one.
const valhallaGPSAccuracyM = 25

type valhallaShapePoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// Time is epoch seconds. Optional, and worth sending: timestamps let the
	// matcher reject a path that would have required going impossibly fast
	// between two fixes.
	Time int64 `json:"time,omitempty"`
}

// valhallaResponse covers /route and /trace_route, which share a trip shape.
type valhallaResponse struct {
	Trip struct {
		Legs []struct {
			// Shape is an encoded polyline at precision 6.
			Shape   string `json:"shape"`
			Summary struct {
				Length float64 `json:"length"` // kilometres, per Units
				Time   float64 `json:"time"`   // seconds
			} `json:"summary"`
		} `json:"legs"`
		Summary struct {
			Length float64 `json:"length"`
			Time   float64 `json:"time"`
		} `json:"summary"`
		StatusMessage string `json:"status_message"`
	} `json:"trip"`
	// Valhalla reports failures as a body, not only a status code.
	Error     string `json:"error"`
	ErrorCode int    `json:"error_code"`
}

func (v *Valhalla) costingOptions() *valhallaCostingOptions {
	if v.truck == nil {
		return nil
	}
	return &valhallaCostingOptions{Truck: v.truck}
}

// post sends a JSON request to a Valhalla endpoint and decodes the trip.
func (v *Valhalla) post(ctx context.Context, path string, body any) (*valhallaResponse, error) {
	if v.baseURL == "" {
		return nil, ErrNotConfigured
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.token != "" {
		req.Header.Set("Authorization", "Bearer "+v.token)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routing provider unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out valhallaResponse
	// A tile-rich response for a long trace can be large; 32 MB is well clear
	// of a cross-Java route and still bounded.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		// A non-JSON body here is usually the reverse proxy, not Valhalla.
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("routing provider returned %s", resp.Status)
		}
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("routing service rejected our credentials (401): " +
			"set VALHALLA_TOKEN to the value the service expects")
	}
	if out.Error != "" {
		// Valhalla's own wording is more useful than anything invented here:
		// "No suitable edges near location" says which end was the problem.
		return nil, fmt.Errorf("no drivable route found (%s)", out.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("routing provider returned %s", resp.Status)
	}
	if len(out.Trip.Legs) == 0 {
		return nil, fmt.Errorf("no drivable route found")
	}
	return &out, nil
}

// legsToRoute flattens a trip's legs into one line and one total.
//
// Totals come from trip.summary rather than from summing the legs, because
// Valhalla computes it and a hand-rolled sum would drift from the engine's own
// answer for no reason.
func legsToRoute(out *valhallaResponse) ([][2]float64, float64, float64) {
	geometry := make([][2]float64, 0, 256)
	for i, leg := range out.Trip.Legs {
		pts := decodePolyline6(leg.Shape)
		// Consecutive legs share their junction point; dropping the duplicate
		// keeps the line clean for anything measuring along it.
		if i > 0 && len(pts) > 0 {
			pts = pts[1:]
		}
		geometry = append(geometry, pts...)
	}
	return geometry, out.Trip.Summary.Length * 1000, out.Trip.Summary.Time
}

func (v *Valhalla) Route(ctx context.Context, from, to Point) (*Route, error) {
	out, err := v.post(ctx, "/route", valhallaRouteRequest{
		Locations: []valhallaLocation{
			{Lat: from.Lat, Lon: from.Lon, Type: "break"},
			{Lat: to.Lat, Lon: to.Lon, Type: "break"},
		},
		Costing:        valhallaCosting,
		CostingOptions: v.costingOptions(),
		Units:          "kilometers",
	})
	if err != nil {
		return nil, err
	}
	geometry, distanceM, durationS := legsToRoute(out)
	return &Route{
		DistanceM: distanceM,
		DurationS: durationS,
		Geometry:  geometry,
		Provider:  v.Name(),
	}, nil
}

// valhallaTraceChunk bounds ONE trace_route request.
//
// Larger than the Geoapify equivalent because this is our own server with no
// per-call charge, so the only limits are Valhalla's `max_trace_shape` and
// request latency. Chunking rather than thinning, for the same reason as in
// geoapify.go: dropping fixes puts holes in the trace and corrupts the matched
// distance, whereas splitting it loses only the join between chunks.
const valhallaTraceChunk = 1000

// valhallaTraceConcurrency bounds parallel chunk requests. Modest, because the
// chunks are competing for one server's CPU rather than for a remote quota.
const valhallaTraceConcurrency = 4

// valhallaTraceGapBreak is the silence after which a trace is treated as two
// separate journeys rather than one.
//
// A truck parked overnight, or a device offline for an hour, leaves two fixes
// that are adjacent in the data and unrelated on the road. Matching across that
// would draw a confident line down a route nobody took and add its length to
// the total. Ten minutes is comfortably longer than any ordinary reporting gap
// and far shorter than a stop worth separating.
const valhallaTraceGapBreak = 10 * time.Minute

// chunkTrace splits a trace into segments Valhalla can match in one request:
// bounded in size, and broken wherever the record itself is broken.
//
// Splitting rather than thinning, for the same reason as in geoapify.go:
// dropping fixes puts holes in the trace and corrupts the matched distance,
// whereas splitting loses only the join between segments.
func chunkTrace(points []TracePoint) [][]TracePoint {
	var chunks [][]TracePoint
	start := 0
	for i := 1; i <= len(points); i++ {
		split := i == len(points) || i-start >= valhallaTraceChunk
		if !split && !points[i].At.IsZero() && !points[i-1].At.IsZero() {
			split = points[i].At.Sub(points[i-1].At) > valhallaTraceGapBreak
		}
		if !split {
			continue
		}
		// A lone fix spans nothing and cannot be matched; drop it rather than
		// sending a request that is certain to fail.
		if i-start >= 2 {
			chunks = append(chunks, points[start:i])
		}
		start = i
	}
	return chunks
}

// Match snaps a GPS trace to roads via Valhalla's trace_route.
//
// Partial success is reported rather than hidden: a stretch through an unmapped
// yard can fail while the rest of the day matches perfectly, and a caller
// should be able to say so instead of presenting a gap as a complete path.
func (v *Valhalla) Match(ctx context.Context, points []TracePoint) (*MatchedTrace, error) {
	if v.baseURL == "" {
		return nil, ErrNotConfigured
	}
	if len(points) < 2 {
		return nil, fmt.Errorf("need at least 2 points to match a trace")
	}

	chunks := chunkTrace(points)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("no usable segment in a %d-point trace", len(points))
	}

	type result struct {
		geometry [][2]float64
		distance float64
		err      error
	}
	results := make([]result, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, valhallaTraceConcurrency)
	for i, c := range chunks {
		wg.Add(1)
		go func(i int, c []TracePoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			g, d, err := v.matchChunk(ctx, c)
			results[i] = result{geometry: g, distance: d, err: err}
		}(i, c)
	}
	wg.Wait()

	out := &MatchedTrace{
		PointsIn:    len(points),
		ChunksTotal: len(chunks),
		Provider:    v.Name(),
	}
	// Stitched back in order, so a failed chunk leaves a gap in the line rather
	// than reordering what did match.
	for _, r := range results {
		if r.err != nil || len(r.geometry) == 0 {
			continue
		}
		out.ChunksMatched++
		out.DistanceM += r.distance
		out.Geometry = append(out.Geometry, r.geometry...)
	}
	out.PointsUsed = len(out.Geometry)
	if out.ChunksMatched == 0 {
		// CARRY THE ENGINE'S OWN REASON. An earlier version returned only
		// "no part of the trace could be matched to roads", which says that
		// something failed and nothing about what — and the plausible causes
		// are very different: fixes too far apart, points out over water on a
		// ferry crossing, or a costing that rejects the roads involved.
		// Valhalla names which; throwing that away made every failure look the
		// same.
		why := "no reason reported"
		for _, r := range results {
			if r.err != nil {
				why = r.err.Error()
				break
			}
		}
		return nil, fmt.Errorf("no part of the trace could be matched to roads "+
			"(%d segment(s) tried, first failure: %s)", len(chunks), why)
	}

	// Measured on an Indonesia graph, Salatiga->Semarang, by
	// routing-service/scripts/probe-matching.py: matching reconstructs 100% of
	// a known route while fixes are within ~640 m, 83% at ~1.0 km, 28% at
	// ~1.5 km and 3% at ~2.4 km. FMS trackers report about every 50 s, which is
	// ~420 m in town but ~1.1 km at Trans-Java speeds — so degradation is
	// expected on exactly the fast roads that matter most, and it has to be
	// visible rather than inferred from a number looking small.
	out.RawDistanceM = rawTraceDistanceM(points)
	out.Partial = out.RawDistanceM > 0 && out.DistanceM < out.RawDistanceM*valhallaPartialMatchRatio
	return out, nil
}

// valhallaPartialMatchRatio is how far below the straight-line length a matched
// path may fall before it is called partial.
//
// Roads are longer than the straight lines between their own points, so a
// complete match lands above 1,0. 0,8 leaves room for sparse sampling cutting
// corners while still catching the 83%-and-below cases the probe found.
const valhallaPartialMatchRatio = 0.8

// rawTraceDistanceM sums the great-circle hops between consecutive fixes: the
// shortest distance the vehicle could possibly have travelled through them.
func rawTraceDistanceM(points []TracePoint) float64 {
	const earthM = 6371008.8
	total := 0.0
	for i := 1; i < len(points); i++ {
		lat1 := points[i-1].Lat * math.Pi / 180
		lat2 := points[i].Lat * math.Pi / 180
		dLat := lat2 - lat1
		dLon := (points[i].Lon - points[i-1].Lon) * math.Pi / 180
		h := math.Pow(math.Sin(dLat/2), 2) +
			math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin(dLon/2), 2)
		total += earthM * 2 * math.Asin(math.Sqrt(h))
	}
	return total
}

func (v *Valhalla) matchChunk(ctx context.Context, points []TracePoint) ([][2]float64, float64, error) {
	shape := make([]valhallaShapePoint, 0, len(points))
	for _, p := range points {
		sp := valhallaShapePoint{Lat: p.Lat, Lon: p.Lon}
		if !p.At.IsZero() {
			sp.Time = p.At.Unix()
		}
		shape = append(shape, sp)
	}
	out, err := v.post(ctx, "/trace_route", valhallaTraceRequest{
		Shape:          shape,
		Costing:        valhallaCosting,
		CostingOptions: v.costingOptions(),
		Units:          "kilometers",
		ShapeMatch:     "map_snap",
		TraceOptions: &valhallaTraceOptions{
			BreakageDistance: valhallaBreakageDistanceM,
			SearchRadius:     valhallaSearchRadiusM,
			GPSAccuracy:      valhallaGPSAccuracyM,
		},
	})
	if err != nil {
		return nil, 0, err
	}
	geometry, distanceM, _ := legsToRoute(out)
	return geometry, distanceM, nil
}

// decodePolyline6 decodes Valhalla's encoded polyline into [lon, lat] pairs.
//
// PRECISION 6, not 5. Valhalla encodes at 1e6 while Google's original format
// and most decoders found in the wild use 1e5. A precision-5 decoder returns
// coordinates a factor of ten out — which lands in the ocean rather than
// failing, so it would be noticed as "routing is broken" long after the cause.
//
// Output is [lon, lat] to match Route.Geometry, which is GeoJSON order.
func decodePolyline6(encoded string) [][2]float64 {
	const factor = 1e6
	out := make([][2]float64, 0, len(encoded)/4)
	var lat, lon int
	i := 0

	// next reads one zigzag-encoded varint, or reports exhaustion.
	next := func() (int, bool) {
		var shift uint
		var result int
		for {
			if i >= len(encoded) {
				return 0, false
			}
			b := int(encoded[i]) - 63
			i++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}
		if result&1 != 0 {
			return ^(result >> 1), true
		}
		return result >> 1, true
	}

	for i < len(encoded) {
		dlat, ok := next()
		if !ok {
			break
		}
		dlon, ok := next()
		if !ok {
			break
		}
		lat += dlat
		lon += dlon
		out = append(out, [2]float64{float64(lon) / factor, float64(lat) / factor})
	}
	return out
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
