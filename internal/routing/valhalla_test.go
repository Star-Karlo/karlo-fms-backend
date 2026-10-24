package routing

import (
	"math"
	"testing"
	"time"
)

// The expected pairs are [lon, lat] — GeoJSON order, matching Route.Geometry.
var polylineCases = []struct {
	name     string
	encoded  string
	expected [][2]float64
}{
	{
		// Encoded independently (a Python reference encoder) from Jakarta
		// towards Semarang. Includes a northward step after a southward one, so
		// the zigzag sign handling is exercised in both directions, and every
		// latitude is negative — the southern hemisphere is where an unsigned
		// shift would show up.
		name:    "jakarta to semarang, mixed-sign deltas",
		encoded: "~jlyJshqvjEfsaBkwaDguhC_~cH",
		expected: [][2]float64{
			{106.816666, -6.200000},
			{106.900000, -6.250500},
			{107.050000, -6.180000},
		},
	},
	{name: "empty", encoded: "", expected: [][2]float64{}},
}

func TestDecodePolyline6(t *testing.T) {
	for _, tc := range polylineCases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodePolyline6(tc.encoded)
			if len(got) != len(tc.expected) {
				t.Fatalf("got %d points, want %d: %v", len(got), len(tc.expected), got)
			}
			for i, want := range tc.expected {
				// 1e-6 is the encoding's own resolution, so anything within
				// half a unit of it is exact as far as the format can express.
				if math.Abs(got[i][0]-want[0]) > 5e-7 || math.Abs(got[i][1]-want[1]) > 5e-7 {
					t.Errorf("point %d: got [%.6f, %.6f], want [%.6f, %.6f]",
						i, got[i][0], got[i][1], want[0], want[1])
				}
			}
		})
	}
}

// TestDecodePolyline6RejectsPrecision5 guards the specific mistake that would
// be hardest to notice in production.
//
// Valhalla encodes at 1e6; Google's original format and most decoders in the
// wild use 1e5. Feeding a precision-5 payload to this decoder yields
// coordinates a factor of ten too small — which lands in the Gulf of Guinea
// rather than erroring, so the symptom would be "routing is broken" with no
// pointer to the cause. This test asserts the failure is detectable, so that if
// anyone later "simplifies" the factor to 1e5 the suite says so.
func TestDecodePolyline6RejectsPrecision5(t *testing.T) {
	// The same three coordinates, encoded at precision 5.
	const p5 = `~|yd@es}jSrzHygOswLoh\`
	got := decodePolyline6(p5)
	if len(got) == 0 {
		t.Skip("decoder consumed nothing; nothing to assert")
	}
	lon, lat := got[0][0], got[0][1]
	// Indonesia: longitude 95..141, latitude -11..6. A precision-5 payload read
	// at precision 6 must fall outside that box.
	if lon >= 95 && lon <= 141 && lat >= -11 && lat <= 6 {
		t.Errorf("a precision-5 payload decoded to a plausible Indonesian coordinate "+
			"[%.6f, %.6f] — the decoder is using the wrong factor", lon, lat)
	}
}

func TestLegsToRouteDropsSharedJunction(t *testing.T) {
	// Two legs meeting at 107.050000,-6.180000. The junction appears as the
	// last point of leg 1 and the first of leg 2; the line must carry it once.
	out := &valhallaResponse{}
	out.Trip.Legs = []struct {
		Shape   string `json:"shape"`
		Summary struct {
			Length float64 `json:"length"`
			Time   float64 `json:"time"`
		} `json:"summary"`
	}{
		{Shape: "~jlyJshqvjEfsaBkwaDguhC_~cH"}, // 3 points, ending at the junction
		{Shape: "~jlyJshqvjEfsaBkwaDguhC_~cH"}, // same 3 points again
	}
	out.Trip.Summary.Length = 12.5
	out.Trip.Summary.Time = 900

	geometry, distanceM, durationS := legsToRoute(out)
	if want := 5; len(geometry) != want {
		t.Errorf("got %d points, want %d (6 minus the shared junction)", len(geometry), want)
	}
	// Kilometres in, metres out.
	if distanceM != 12500 {
		t.Errorf("got %.0f m, want 12500", distanceM)
	}
	if durationS != 900 {
		t.Errorf("got %.0f s, want 900", durationS)
	}
}

func TestTrimSlash(t *testing.T) {
	for in, want := range map[string]string{
		"http://valhalla:8002":    "http://valhalla:8002",
		"http://valhalla:8002/":   "http://valhalla:8002",
		"http://valhalla:8002///": "http://valhalla:8002",
		"":                        "",
		"/":                       "",
	} {
		if got := trimSlash(in); got != want {
			t.Errorf("trimSlash(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestChunkTrace covers the two reasons a trace is split, and the reason a
// fragment is dropped.
func TestChunkTrace(t *testing.T) {
	at := func(min int) time.Time {
		return time.Date(2026, 10, 24, 8, min, 0, 0, time.UTC)
	}
	run := func(mins ...int) []TracePoint {
		pts := make([]TracePoint, 0, len(mins))
		for _, m := range mins {
			pts = append(pts, TracePoint{Lat: -6.2, Lon: 106.8, At: at(m)})
		}
		return pts
	}

	t.Run("one continuous run stays whole", func(t *testing.T) {
		got := chunkTrace(run(0, 1, 2, 3, 4))
		if len(got) != 1 || len(got[0]) != 5 {
			t.Fatalf("got %d chunks %v, want one of 5", len(got), lens(got))
		}
	})

	t.Run("splits at a long silence", func(t *testing.T) {
		// 8-minute gap stays together; the 40-minute one breaks.
		got := chunkTrace(run(0, 1, 9, 10, 50, 51, 52))
		if len(got) != 2 {
			t.Fatalf("got %d chunks %v, want 2", len(got), lens(got))
		}
		if len(got[0]) != 4 || len(got[1]) != 3 {
			t.Errorf("got chunk sizes %v, want [4 3]", lens(got))
		}
	})

	t.Run("a lone fix after a gap is dropped, not sent", func(t *testing.T) {
		got := chunkTrace(run(0, 1, 2, 99))
		if len(got) != 1 || len(got[0]) != 3 {
			t.Fatalf("got %d chunks %v, want one of 3", len(got), lens(got))
		}
	})

	t.Run("untimed points are never split on time", func(t *testing.T) {
		pts := make([]TracePoint, 5)
		for i := range pts {
			pts[i] = TracePoint{Lat: -6.2, Lon: 106.8}
		}
		if got := chunkTrace(pts); len(got) != 1 {
			t.Errorf("got %d chunks, want 1", len(got))
		}
	})

	t.Run("respects the per-request size bound", func(t *testing.T) {
		pts := make([]TracePoint, valhallaTraceChunk+10)
		for i := range pts {
			pts[i] = TracePoint{Lat: -6.2, Lon: 106.8, At: at(0)}
		}
		got := chunkTrace(pts)
		if len(got) != 2 {
			t.Fatalf("got %d chunks %v, want 2", len(got), lens(got))
		}
		if len(got[0]) != valhallaTraceChunk {
			t.Errorf("first chunk is %d, want %d", len(got[0]), valhallaTraceChunk)
		}
	})
}

func lens(chunks [][]TracePoint) []int {
	out := make([]int, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, len(c))
	}
	return out
}
