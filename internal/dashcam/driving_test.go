package dashcam

import "testing"

func TestIsDrivingEvent(t *testing.T) {
	// Confirmed codes map; unknown / non-behaviour codes do not.
	if b, ok := isDrivingEvent(codeHarshBrake); !ok || b.kind != "braking" || b.axis != "X" {
		t.Errorf("harsh brake: got %+v ok=%v", b, ok)
	}
	if b, ok := isDrivingEvent(codeHarshAccel); !ok || b.kind != "acceleration" || b.axis != "X" {
		t.Errorf("harsh accel: got %+v ok=%v", b, ok)
	}
	// A DMS/ADAS or speed code is not a scored driving-behaviour event.
	for _, code := range []int{1, 26, 27, 100, 200, 999} {
		if _, ok := isDrivingEvent(code); ok {
			t.Errorf("code %d should not be a driving event", code)
		}
	}
	// The unconfirmed sentinel (-1) must never resolve — that would silently
	// classify a real code-(-1) as cornering/road-shock.
	if _, ok := isDrivingEvent(-1); ok {
		t.Error("sentinel -1 must not map to a behaviour")
	}
}

func TestClassifySeverity(t *testing.T) {
	g := func(v float64) *float64 { return &v }
	cases := []struct {
		name   string
		gforce *float64
		code   int
		want   string
	}{
		{"black at 0.8g", g(0.8), codeHarshBrake, "black"},
		{"black above", g(1.2), codeHarshBrake, "black"},
		{"red at 0.4g", g(0.4), codeHarshBrake, "red"},
		{"red band", g(0.6), codeHarshBrake, "red"},
		{"yellow at 0.3g", g(0.3), codeHarshBrake, "yellow"},
		{"yellow below threshold", g(0.1), codeHarshBrake, "yellow"},
		// No G value: fall back to the code's inherent severity. Harsh brake is
		// "important" -> yellow; a panic (urgent) -> red.
		{"no g, important code", nil, codeHarshBrake, "yellow"},
		{"no g, urgent code", nil, 1 /* PANIC */, "red"},
	}
	for _, c := range cases {
		if got := classifySeverity(c.gforce, c.code); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestGreenBehaviour(t *testing.T) {
	cases := map[int]behaviour{
		1: {"acceleration", "X"},
		2: {"braking", "X"},
		3: {"cornering", "Y"},
		4: {"road_shock", "Z"}, // raw-axis vertical bump / road shock
	}
	for typ, want := range cases {
		if b, ok := greenBehaviour(typ); !ok || b != want {
			t.Errorf("greenBehaviour(%d) = %+v,%v want %+v", typ, b, ok, want)
		}
	}
	for _, bad := range []int{0, 5, -1, 99} {
		if _, ok := greenBehaviour(bad); ok {
			t.Errorf("greenBehaviour(%d) should be false", bad)
		}
	}
}

func TestSeverityFromGforce(t *testing.T) {
	for g, want := range map[float64]string{0.1: "yellow", 0.3: "yellow", 0.4: "red", 0.79: "red", 0.8: "black", 1.5: "black"} {
		if got := severityFromGforce(g); got != want {
			t.Errorf("severityFromGforce(%v) = %q want %q", g, got, want)
		}
	}
}

func TestGreenGforce(t *testing.T) {
	// Teltonika value is g×100: 45 -> 0.45 g -> red.
	v := 45.0
	g := greenGforce(&v)
	if g == nil || *g != 0.45 {
		t.Fatalf("greenGforce(45) = %v, want 0.45", g)
	}
	if sev := severityFromGforce(*g); sev != "red" {
		t.Errorf("severity of 0.45g = %q, want red", sev)
	}
	if greenGforce(nil) != nil {
		t.Error("nil value must yield nil g")
	}
}
