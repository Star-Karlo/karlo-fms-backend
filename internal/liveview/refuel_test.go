package liveview

import (
	"encoding/json"
	"os"
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
	if rs := refuelsFromRises(ps, trusted, &tank); len(rs) != 0 {
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
