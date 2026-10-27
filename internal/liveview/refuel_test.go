package liveview

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// K-9467-BF from 26 Oct 18:30 to 27 Oct 08:55 WIB, as the tracker sent it
// (positions rounded, metadata cut to the fuel level and Movement).
//
// It holds one real refuel and one false one. At Rest Area Km 754 on the
// Surabaya–Gempol toll the truck stood eleven minutes with the engine cycling
// at the pump: 24% going in, and a damped gauge that climbed to 61% over the
// first six minutes of driving out. Parked overnight at Rembang Industri Raya
// at 52%, the tracker reported 8% for the 37 minutes the engine was off after
// a brief start, and 54% once it ran again. The old detector showed both as
// refuels, the first at the wrong stop and from the wrong level, and a window
// ending at 08:20 counted the false 8% as 150 L burned.
func loadK9467(t *testing.T, from, to string) []telemetry.Position {
	t.Helper()
	b, err := os.ReadFile("testdata/k9467bf_2026-10-26_27.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []telemetry.Position
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	lo, _ := time.Parse(time.RFC3339, from)
	hi, _ := time.Parse(time.RFC3339, to)
	var out []telemetry.Position
	for _, p := range all {
		if !p.Time.Before(lo) && !p.Time.After(hi) {
			out = append(out, p)
		}
	}
	return out
}

func TestRefuelsAtStopsFindsTheRealFillOnly(t *testing.T) {
	tank := 340.0
	ps := loadK9467(t, "2026-10-26T11:30:00Z", "2026-10-27T01:55:00Z")
	refuels := dedupRefuels(refuelsAtStops(ps, trustedLevels(ps), &tank))

	if len(refuels) != 1 {
		t.Fatalf("want exactly the rest-area refuel, got %d: %+v", len(refuels), refuels)
	}
	r := refuels[0]
	if !strings.Contains(r.Address, "Rest Area Km 754") {
		t.Errorf("refuel placed at %q, want the pump stop at Rest Area Km 754", r.Address)
	}
	wib := r.Time.In(time.FixedZone("WIB", 7*3600))
	if wib.Format("02 Jan 15:04") != "26 Oct 19:34" {
		t.Errorf("refuel at %s, want 26 Oct 19:34 WIB, when the truck stopped", wib.Format("02 Jan 15:04"))
	}
	// 24% in, 61% out: measured from the level going in, not from the 15% dip
	// the gauge makes at engine start.
	if math.Abs(r.Pct-37) > 1 {
		t.Errorf("refuel %.1f%%, want 37%% (24%% → 61%%)", r.Pct)
	}
}

// A window that ends while the engine is off must not end on the false 8%.
// This is the order window the TMS card used: it closed at 08:20, mid-reading,
// and booked the 52% → 8% fall as 150 L burned.
func TestTrustedLevelsIgnoreTheEngineOffReading(t *testing.T) {
	ps := loadK9467(t, "2026-10-26T13:00:00Z", "2026-10-27T01:20:00Z") // 20:00–08:20 WIB
	trusted := trustedLevels(ps)
	if len(trusted) < 2 {
		t.Fatal("the evening's driving should leave trusted readings")
	}
	if end := trusted[len(trusted)-1].lvl; end < 50 {
		t.Errorf("window ends on %.0f%%; the tank held 52-58%% all night", end)
	}
	tank := 340.0
	if rs := refuelsAtStops(ps, trusted, &tank); len(rs) != 0 {
		t.Errorf("a parked truck refuelled %d times: %+v", len(rs), rs)
	}
}

// With the engine off for the whole window there is no level to believe, and
// FuelTrip must say so rather than fall back to the raw readings.
func TestTrustedLevelsEmptyWhenTheEngineNeverRan(t *testing.T) {
	ps := loadK9467(t, "2026-10-26T23:00:00Z", "2026-10-27T01:20:00Z") // 06:00–08:20 WIB, parked
	if n := len(trustedLevels(ps)); n >= 2 {
		t.Errorf("%d trusted readings from a parked truck with the engine off", n)
	}
}

// One high reading on the move is fuel sloshing over a bump, not a fill.
func TestRefuelsAtStopsIgnoresABump(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	var ps []telemetry.Position
	for i, lvl := range []float64{40, 40, 40, 55, 40, 40, 39, 39} {
		speed := 40.0
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": lvl})
		ps = append(ps, telemetry.Position{Time: at.Add(time.Duration(i) * 90 * time.Second), Speed: &speed, Ignition: &on, Metadata: md})
	}
	if rs := refuelsAtStops(ps, trustedLevels(ps), &tank); len(rs) != 0 {
		t.Errorf("a bump was booked as %d refuel(s): %+v", len(rs), rs)
	}
}

// A stop too short for a pump is not a refuel, however the gauge moves.
func TestRefuelsAtStopsNeedsAStopLongEnoughToFill(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	type fix struct {
		sec   int
		speed float64
		lvl   float64
	}
	fixes := []fix{
		{0, 40, 40}, {300, 40, 40}, {600, 40, 40},
		{660, 0, 40}, {720, 0, 40}, // one minute standing
		{780, 40, 58}, {1200, 40, 58}, {1500, 40, 58}, {1800, 40, 58},
	}
	var ps []telemetry.Position
	for _, f := range fixes {
		speed := f.speed
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": f.lvl})
		ps = append(ps, telemetry.Position{Time: at.Add(time.Duration(f.sec) * time.Second), Speed: &speed, Ignition: &on, Metadata: md})
	}
	if rs := refuelsAtStops(ps, trustedLevels(ps), &tank); len(rs) != 0 {
		t.Errorf("a one-minute stop was booked as a refuel: %+v", rs)
	}
}
