package routing

import (
	"math"
	"testing"
	"time"
)

// Valhalla refuses a trace_route longer than 200 km of path. chunkTrace must
// therefore bound a chunk by DISTANCE and not only by point count — the bug
// probe-sparse-follow.py caught, where 1.000 sparse fixes made one ~1.200 km
// request that was rejected outright with error 154.
func TestChunkTraceBoundsSpanNotJustCount(t *testing.T) {
	// 300 fixes marching north, ~1,11 km apart: 333 km in well under the
	// 1.000-point count limit.
	var pts []TracePoint
	base := time.Now()
	for i := 0; i < 300; i++ {
		pts = append(pts, TracePoint{
			Lat: -6.0 - float64(i)*0.01,
			Lon: 106.8,
			At:  base.Add(time.Duration(i) * 50 * time.Second),
		})
	}

	chunks := chunkTrace(pts)
	if len(chunks) < 2 {
		t.Fatalf("a 333 km trace must split; got %d chunk(s)", len(chunks))
	}
	for i, c := range chunks {
		if span := rawTraceDistanceM(c); span > valhallaTraceMaxSpanM {
			t.Errorf("chunk %d spans %.0f m, over the %d m bound", i, span, valhallaTraceMaxSpanM)
		}
	}
	// Splitting must not lose fixes: every point belongs to exactly one chunk,
	// in order, or the matched distance is quietly short.
	var seen int
	for _, c := range chunks {
		seen += len(c)
	}
	if seen != len(pts) {
		t.Errorf("want all %d fixes chunked, got %d", len(pts), seen)
	}
}

// THE BUG THIS PINS. trace_route reports a discontinuous match as several
// trips: the first contiguous segment in `trip` and the rest in `alternates`,
// in travel order. This client read `trip` alone, so a 99 km stretch that
// matched as 37,7 + 10,3 + 46,5 km was reported as 37,7 km — and then blamed
// on the fixes being too far apart, which no parameter could have fixed.
func TestTraceSegmentsReadsAlternatesAsContinuations(t *testing.T) {
	const shape = "_p~iF~ps|U_ulLnnqC_mqNvxq`@"

	out := &valhallaResponse{}
	out.Trip.Legs = append(out.Trip.Legs, valhallaLeg{Shape: shape, Summary: valhallaSummary{Length: 37.7}})
	out.Trip.Summary = valhallaSummary{Length: 37.7}
	for _, km := range []float64{10.3, 46.5} {
		var alt struct {
			Trip valhallaTrip `json:"trip"`
		}
		alt.Trip.Legs = append(alt.Trip.Legs, valhallaLeg{Shape: shape, Summary: valhallaSummary{Length: km}})
		alt.Trip.Summary = valhallaSummary{Length: km}
		out.Alternates = append(out.Alternates, alt)
	}

	segs := traceSegments(out)
	if len(segs) != 3 {
		t.Fatalf("want 3 contiguous segments (trip + 2 alternates), got %d", len(segs))
	}
	var total float64
	for _, s := range segs {
		total += s.distanceM
	}
	if want := 94500.0; math.Abs(total-want) > 1 {
		t.Errorf("want %.0f m across all segments, got %.0f m", want, total)
	}
	// Order matters: these are consecutive stretches of one journey, and
	// stitching them out of order would draw the line doubling back.
	if segs[0].distanceM != 37700 || segs[2].distanceM != 46500 {
		t.Errorf("segments must stay in travel order, got %.0f then %.0f",
			segs[0].distanceM, segs[2].distanceM)
	}
}

// /route uses `alternates` for COMPETING routes between the same endpoints.
// Summing those would add several versions of one journey together, so the
// /route path must never read the field.
func TestLegsToRouteIgnoresAlternates(t *testing.T) {
	const shape = "_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	out := &valhallaResponse{}
	out.Trip.Legs = append(out.Trip.Legs, valhallaLeg{Shape: shape, Summary: valhallaSummary{Length: 12.0}})
	out.Trip.Summary = valhallaSummary{Length: 12.0}

	var alt struct {
		Trip valhallaTrip `json:"trip"`
	}
	alt.Trip.Summary = valhallaSummary{Length: 15.0}
	alt.Trip.Legs = append(alt.Trip.Legs, valhallaLeg{Shape: shape, Summary: valhallaSummary{Length: 15.0}})
	out.Alternates = append(out.Alternates, alt)

	_, distanceM, _ := legsToRoute(out)
	if distanceM != 12000 {
		t.Errorf("a /route reply must report only its own trip: want 12000 m, got %.0f", distanceM)
	}
}
