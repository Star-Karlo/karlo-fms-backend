package driverscore

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSeverityPoints(t *testing.T) {
	for sev, want := range map[string]float64{"yellow": 1, "red": 2, "black": 4, "": 1} {
		if got := severityPoints(sev); got != want {
			t.Errorf("severityPoints(%q)=%v want %v", sev, got, want)
		}
	}
}

func TestHarshPointsAndPenalty(t *testing.T) {
	c := &Collector{policy: DefaultPolicy} // HarshPer100kmFull = 8
	pd := newPerDriver()
	pd.distanceKm = 1000
	// 2 yellow + 1 red + 1 black braking = 2*1 + 1*2 + 1*4 = 8 points, 4 events.
	pd.harsh["braking"] = map[string]int{"yellow": 2, "red": 1, "black": 1}

	pts, n := pd.harshPoints("braking")
	if !almost(pts, 8) || n != 4 {
		t.Fatalf("harshPoints = %v pts, %d events; want 8, 4", pts, n)
	}

	// rate = 100 * 8 / 1000 = 0.8 points/100km. severity(0.8, 0, 8) = 0.1.
	// penalty = weightHarshBrake(15) * 0.1 = 1.5.
	if p := c.harshPenalty(pd, "braking", weightHarshBrake); !almost(p, 1.5) {
		t.Errorf("brake penalty = %v, want 1.5", p)
	}

	// A pillar with no events costs nothing.
	if p := c.harshPenalty(pd, "cornering", weightHarshCorner); p != 0 {
		t.Errorf("empty pillar penalty = %v, want 0", p)
	}
}

func TestHarshPenaltyExposureNormalised(t *testing.T) {
	c := &Collector{policy: DefaultPolicy}
	// Same absolute events, different distance: the driver who drove twice as
	// far for the same events is penalised half as much (rate, not count).
	mk := func(dist float64) *perDriver {
		pd := newPerDriver()
		pd.distanceKm = dist
		pd.harsh["acceleration"] = map[string]int{"red": 10} // 20 points
		return pd
	}
	short := c.harshPenalty(mk(500), "acceleration", weightHarshAccel)
	long := c.harshPenalty(mk(1000), "acceleration", weightHarshAccel)
	if !(short > long) {
		t.Errorf("shorter distance should penalise more: short=%v long=%v", short, long)
	}
}

func TestHarshPenaltySaturates(t *testing.T) {
	c := &Collector{policy: DefaultPolicy}
	pd := newPerDriver()
	pd.distanceKm = 100
	// 100 black events over 100 km = 400 points/100km, far past the full rate:
	// the penalty caps at the pillar weight, never more.
	pd.harsh["cornering"] = map[string]int{"black": 100}
	if p := c.harshPenalty(pd, "cornering", weightHarshCorner); !almost(p, weightHarshCorner) {
		t.Errorf("saturated penalty = %v, want %v", p, weightHarshCorner)
	}
}
