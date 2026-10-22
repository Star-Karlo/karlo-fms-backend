package liveview

import (
	"testing"
	"time"
)

var wib = time.FixedZone("WIB", 7*3600)

// The median gap is what decides whether the harsh-event counts mean
// anything, so it has to be the typical spacing and not the average — one
// overnight gap must not make a densely sampled day look sparse.
func TestMedianGapS(t *testing.T) {
	base := time.Date(2026, 10, 22, 8, 0, 0, 0, wib)
	at := func(offsets ...int) []TripPoint {
		out := make([]TripPoint, 0, len(offsets))
		for _, o := range offsets {
			out = append(out, TripPoint{Time: base.Add(time.Duration(o) * time.Second)})
		}
		return out
	}
	if got := medianGapS(at(0, 30, 60, 90)); got != 30 {
		t.Errorf("even spacing: got %v, want 30", got)
	}
	// Three 30s gaps and one of eight hours: the median still says 30.
	if got := medianGapS(at(0, 30, 60, 90, 28890)); got != 30 {
		t.Errorf("one long gap should not move the median: got %v", got)
	}
	if got := medianGapS(nil); got != 0 {
		t.Errorf("no points: got %v, want 0", got)
	}
	if got := medianGapS(at(0)); got != 0 {
		t.Errorf("one point has no gap: got %v, want 0", got)
	}
}

// The rollup's last row covers midnight until now, so "today so far" is
// answerable; "midnight yesterday to midday today" is not, because that row
// would contribute a whole day against half a day of question.
func TestIsEndOfDay(t *testing.T) {
	cases := []struct {
		t    time.Time
		want bool
	}{
		{time.Date(2026, 10, 22, 23, 59, 59, 0, wib), true},
		{time.Date(2026, 10, 22, 23, 59, 59, 999e6, wib), true},
		{time.Date(2026, 10, 23, 0, 0, 0, 0, wib), true},
		{time.Date(2026, 10, 22, 14, 42, 0, 0, wib), false},
		{time.Date(2026, 10, 22, 23, 58, 0, 0, wib), false},
	}
	for _, c := range cases {
		if got := isEndOfDay(c.t); got != c.want {
			t.Errorf("isEndOfDay(%s) = %v, want %v", c.t.Format(time.RFC3339), got, c.want)
		}
	}
}

// Without a telemetry client there is no rollup to true up from, and the
// answer must still say what it was derived from rather than leaving the
// fields empty and letting a reader assume the best.
func TestTrueUpLabelsEvenWithoutRollup(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, wib)
	pts := []TripPoint{
		{Time: base}, {Time: base.Add(15 * time.Minute)}, {Time: base.Add(30 * time.Minute)},
	}
	var res BehaviorResult
	res.Summary.DistanceKm = 1504.3
	s := &Service{} // no telemetry client

	s.trueUpFromRollup(t.Context(), 1, 1, base, base.Add(52*24*time.Hour), pts, &res)

	if res.Summary.DistanceSource != "track" {
		t.Errorf("source = %q, want track", res.Summary.DistanceSource)
	}
	if res.Summary.SampleGapS != 900 {
		t.Errorf("gap = %v, want 900", res.Summary.SampleGapS)
	}
	if !res.Summary.EventsSampled {
		t.Error("a 15-minute gap must mark the event counts as sampled: harsh detection needs pairs within 30s")
	}
	if res.Summary.DistanceKm != 1504.3 {
		t.Error("with no rollup available the track's own distance must survive untouched")
	}
}

// A dense window is exact, and must not be labelled as if it were not.
func TestDenseWindowIsNotMarkedSampled(t *testing.T) {
	base := time.Date(2026, 10, 22, 0, 0, 0, 0, wib)
	pts := []TripPoint{{Time: base}, {Time: base.Add(17 * time.Second)}, {Time: base.Add(34 * time.Second)}}
	var res BehaviorResult
	(&Service{}).trueUpFromRollup(t.Context(), 1, 1, base, base.Add(24*time.Hour), pts, &res)
	if res.Summary.EventsSampled {
		t.Error("17-second spacing resolves harsh events; it must not be flagged as sampled")
	}
}
