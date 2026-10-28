package liveview

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// The real trip, through the new detector: same answer as the stop-based one.
//
// K-9467-BF filled at Rest Area Km 754 on the Surabaya–Gempol toll — 24% going
// in, a damped gauge climbing to 61% over the first six minutes of driving
// out. The climb is therefore seen while MOVING, which is exactly where the
// original walk refused to look, and the pin still has to land at the rest
// area rather than on the motorway where the number caught up.
func TestRisesFindTheRealFillAndPlaceItAtTheRestArea(t *testing.T) {
	tank := 340.0
	ps := loadK9467(t, "2026-10-26T11:30:00Z", "2026-10-27T01:55:00Z")
	refuels := dedupRefuels(refuelsFromRises(ps, trustedLevels(ps), &tank))

	if len(refuels) != 1 {
		t.Fatalf("want exactly the rest-area refuel, got %d: %+v", len(refuels), refuels)
	}
	r := refuels[0]
	if !strings.Contains(r.Address, "Rest Area Km 754") {
		t.Errorf("refuel placed at %q, want Rest Area Km 754 — the gauge caught up on the "+
			"road, but the fuel went in at the stop", r.Address)
	}
	wib := r.Time.In(time.FixedZone("WIB", 7*3600))
	if wib.Format("02 Jan 15:04") != "26 Oct 19:34" {
		t.Errorf("refuel at %s, want 26 Oct 19:34 WIB, when the truck stopped", wib.Format("02 Jan 15:04"))
	}
	if math.Abs(r.Pct-37) > 1 {
		t.Errorf("refuel %.1f%%, want 37%% (24%% → 61%%)", r.Pct)
	}
}

// The overnight false reading is still not a refuel.
//
// Parked at Rembang at 52%, the tracker reported 8% for the 37 minutes the
// engine was off and 54% once it ran again. That 8% → 54% climb is the one
// the old detector booked as 150 L of fuel that neither burned nor arrived.
// It is excluded here for the same reason as before — trustedLevels drops
// engine-off readings — and this pins that the rise walk did not reintroduce
// it by reading the raw series.
func TestTheOvernightGaugeArtefactIsStillNotAFill(t *testing.T) {
	tank := 340.0
	ps := loadK9467(t, "2026-10-26T20:00:00Z", "2026-10-27T01:55:00Z")
	if rs := dedupRefuels(refuelsFromRises(ps, trustedLevels(ps), &tank)); len(rs) != 0 {
		t.Errorf("the engine-off reading was booked as %d refuel(s): %+v", len(rs), rs)
	}
}

// A ninety-minute ramp at a standstill — the case that broke the stop-based
// detector and the reason this one exists.
//
// A truck fuelled at its loading point and was then loaded for an hour and a
// half. The sender is damped and the truck is not moving, so the gauge crawls
// from 40% to 72% across the whole stop and there is never a settled level
// "coming out". The old detector measured a rise of almost nothing and
// dropped the fill; the balance then came out at 40 − 72 = −32 and clamped to
// a confident 0 L.
func TestASlowRampAtAStandstillIsAFill(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)

	type fix struct {
		min   float64
		speed float64
		lvl   float64
	}
	var fixes []fix
	// Driving in, steady at 40%.
	for m := 0.0; m < 20; m += 2 {
		fixes = append(fixes, fix{m, 45, 40})
	}
	// Standing at the loading point from 20 min. The gauge creeps up over
	// ninety minutes: no jump anywhere, just a climb that never stops.
	lvl := 40.0
	for m := 20.0; m <= 110; m += 3 {
		fixes = append(fixes, fix{m, 0, math.Round(lvl)})
		lvl += 32.0 / 30.0 // 40 → 72 across the load
	}
	// Away again, holding at 72%.
	for m := 115.0; m <= 140; m += 5 {
		fixes = append(fixes, fix{m, 45, 72})
	}

	var ps []telemetry.Position
	for _, f := range fixes {
		speed := f.speed
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": f.lvl})
		ps = append(ps, telemetry.Position{
			Time:  at.Add(time.Duration(f.min * float64(time.Minute))),
			Speed: &speed, Ignition: &on, Metadata: md,
		})
	}

	rs := dedupRefuels(refuelsFromRises(ps, trustedLevels(ps), &tank))
	if len(rs) != 1 {
		t.Fatalf("a 32-point climb across a 90-minute load was not seen as a fill: got %d. "+
			"This is the trip that reported 0 L over 124 km.", len(rs))
	}
	if math.Abs(rs[0].Pct-32) > 2 {
		t.Errorf("fill measured at %.1f%%, want about 32%% (40%% → 72%%)", rs[0].Pct)
	}
	// Placed where the truck stood, not where the gauge happened to be read.
	if rs[0].Time.Sub(at).Minutes() > 25 {
		t.Errorf("fill placed %.0f min in, want at the stop that began at 20 min",
			rs[0].Time.Sub(at).Minutes())
	}
}

// A slosh is not a fill. The level reads high for one fix on a bad road and
// is back where it was by the next; counting it invents fuel that was never
// bought. This is the hold check, and it is the only thing between this
// detector and a noisy sender.
func TestABumpIsStillNotAFill(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	var ps []telemetry.Position
	for i, lvl := range []float64{40, 40, 40, 55, 40, 40, 39, 39} {
		speed := 40.0
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": lvl})
		ps = append(ps, telemetry.Position{
			Time: at.Add(time.Duration(i) * 90 * time.Second), Speed: &speed, Ignition: &on, Metadata: md,
		})
	}
	if rs := refuelsFromRises(ps, trustedLevels(ps), &tank); len(rs) != 0 {
		t.Errorf("a 15-point spike that vanished was booked as %d refuel(s): %+v", len(rs), rs)
	}
}

// A dip mid-climb must not split one fill into two that each fall under the
// threshold — which would drop both and leave the balance short by the whole
// tank. A damped sender wobbles on the way up; the climb is one event.
func TestAWobbleDoesNotSplitAFill(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	var ps []telemetry.Position
	// 40 → 72 with a one-point sag partway up, then held.
	for i, lvl := range []float64{40, 40, 48, 55, 54, 63, 70, 72, 72, 72, 72, 72} {
		speed := 0.0
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": lvl})
		ps = append(ps, telemetry.Position{
			Time: at.Add(time.Duration(i) * 3 * time.Minute), Speed: &speed, Ignition: &on, Metadata: md,
		})
	}
	rs := refuelsFromRises(ps, trustedLevels(ps), &tank)
	if len(rs) != 1 {
		t.Fatalf("the wobble split one fill into %d: %+v", len(rs), rs)
	}
	if math.Abs(rs[0].Pct-32) > 1 {
		t.Errorf("fill measured at %.1f%%, want 32%% — the whole climb, not a piece of it", rs[0].Pct)
	}
}

// A top-up below the sensor-only floor is held back, not thrown away.
//
// 20 litres into a 340-litre tank moves the gauge about six points — under
// refuelMinPct, so the sensor alone cannot tell it from noise. It becomes a
// candidate rather than a refuel, and only a filling station under the truck
// promotes it (promoteAtStations). What must never happen is the climb being
// discarded at this stage, because then no amount of corroboration can bring
// it back and the balance loses the litres for good.
func TestASmallTopUpSurvivesAsACandidate(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	var ps []telemetry.Position
	// Standing still for twenty minutes while the gauge climbs 40 → 46.
	for i, lvl := range []float64{40, 40, 41, 43, 45, 46, 46, 46, 46, 46} {
		speed := 0.0
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": lvl})
		ps = append(ps, telemetry.Position{
			Time: at.Add(time.Duration(i) * 3 * time.Minute), Speed: &speed, Ignition: &on, Metadata: md,
		})
	}
	trusted := trustedLevels(ps)

	// Not a refuel on the sensor alone.
	if rs := refuelsFromRises(ps, trusted, &tank); len(rs) != 0 {
		t.Errorf("a 6-point climb was accepted without corroboration: %+v", rs)
	}
	// But still a candidate, with the dwell that lets a station vouch for it.
	cands := risesInSeries(ps, trusted, &tank)
	if len(cands) != 1 {
		t.Fatalf("the top-up was discarded: got %d candidates, want 1", len(cands))
	}
	if cands[0].accepted {
		t.Error("a 6-point climb must not be accepted on the sensor alone")
	}
	if cands[0].dwellMin < stationDwellMin {
		t.Errorf("dwell %.0f min, want at least %.0f so a station can vouch for it",
			cands[0].dwellMin, stationDwellMin)
	}
}

// Noise is still noise. One or two points of sender wobble is below
// riseMinPct and must not even become a candidate — otherwise every truck
// parked near a filling station collects phantom top-ups.
func TestSenderWobbleIsNotEvenACandidate(t *testing.T) {
	tank := 340.0
	on := true
	at := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	var ps []telemetry.Position
	for i, lvl := range []float64{40, 41, 40, 41, 42, 41, 41, 41} {
		speed := 0.0
		md, _ := json.Marshal(map[string]any{"Fuel Level (CAN)": lvl})
		ps = append(ps, telemetry.Position{
			Time: at.Add(time.Duration(i) * 3 * time.Minute), Speed: &speed, Ignition: &on, Metadata: md,
		})
	}
	if cands := risesInSeries(ps, trustedLevels(ps), &tank); len(cands) != 0 {
		t.Errorf("sender wobble became %d candidate(s): %+v", len(cands), cands)
	}
}
