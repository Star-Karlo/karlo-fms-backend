package liveview

import (
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

const (
	// A climb is held together across dips this small. A damped sender wobbles
	// on its way up, and one low reading in the middle of a fill must not split
	// a 32-point climb into two that each fall under refuelMinPct.
	riseNoisePct = 2.0
	// After the climb stops, the level has to STAY up to count as fuel. A bump
	// — a slosh on a bad road, a sender spike — comes back down within a few
	// readings; fuel in a tank does not. The level is checked over this long
	// after the peak, and must not fall back below the floor by more than the
	// climb itself.
	riseHoldMin = 10.0
	// How far back to look for the stop a climb belongs to. A damped gauge
	// reports a fill minutes after it happened, often once the truck is moving
	// again, so the first RISING reading is routinely on the road rather than
	// at the pump.
	riseStopLookbackMin = 20.0

	// The smallest climb worth considering at all. The sender's own step is
	// 1%, so three of them is the floor below which a "rise" is the gauge
	// talking to itself. Candidates between here and refuelMinPct are only
	// believed with a filling station under the truck — see stationRadiusM.
	riseMinPct = 3.0
	// A truck standing this close to a known SPBU, for this long, is evidence
	// a fill happened that does not come from the sensor at all. Where it
	// holds, a climb is believed at riseMinPct instead of refuelMinPct: a
	// 20-litre top-up moves a 340-litre tank about six points, which the
	// noise floor would otherwise throw away along with the twenty litres.
	//
	// Ten minutes, not two: a fill takes minutes, but so does queueing, and
	// the dwell is only ever used to LOWER the bar on a climb that already
	// happened. It never creates a refuel on its own — a rest area is also
	// lunch, a toilet and a mandatory rest, and a truck that stopped at one
	// without fuelling must not be charged for a tank.
	stationRadiusM  = 250.0
	stationDwellMin = 10.0
)

// stopRun is one period of the truck standing still.
type stopRun struct {
	begin, finish time.Time
	at            telemetry.Position // the fix the stop began on
}

// stopRuns finds every period the truck stood still.
//
// Split out of refuelsAtStops so the rise detector can ask the same question
// without re-deriving it. A run begins on a fix reporting walking pace or
// slower and continues over fixes that are stopped OR carry no speed at all;
// it ends at the first moving fix.
func stopRuns(ps []telemetry.Position) []stopRun {
	stopped := func(p telemetry.Position) bool { return p.Speed != nil && *p.Speed <= movingSpeed }
	var out []stopRun
	for i := 0; i < len(ps); {
		if !stopped(ps[i]) {
			i++
			continue
		}
		si, j := i, i
		for j+1 < len(ps) && (ps[j+1].Speed == nil || stopped(ps[j+1])) {
			j++
		}
		out = append(out, stopRun{begin: ps[si].Time, finish: ps[j].Time, at: ps[si]})
		i = j + 1
	}
	return out
}

// riseCandidate is one climb that stuck, with what is known about where it
// happened. Below refuelMinPct it is held back until something other than the
// sensor agrees — see promoteAtStations.
type riseCandidate struct {
	event    RefuelEvent
	rise     float64
	dwellMin float64 // how long the truck stood where this is placed
	accepted bool    // cleared refuelMinPct on the sensor alone
}

// refuelsFromRises finds fills by walking the trusted level series for climbs
// that stick, instead of asking what the gauge read either side of a stop.
//
// WHY THIS REPLACED THE STOP-BASED DETECTOR.
//
// refuelsAtStops asked: what was the settled level going into this stop, and
// what was it 3 to 20 minutes after the truck moved again? That rests on a
// fill being fast and the gauge settling soon after — true at a pump on a
// toll road, and false in the case that broke it.
//
// A truck fuelled at its LOADING POINT on 28 Oct 2026 and then sat there being
// loaded for ninety minutes. The sender on that truck is heavily damped: at a
// rest area it read 15, 32, 48, 55, 61 over six minutes of driving, chasing a
// fill already in the tank. At a standstill, with no sloshing to help the
// float, that ramp stretched across the whole load. There was never a settled
// level coming out, so the measured rise fell under refuelMinPct and the fill
// was dropped — leaving 40% − 72% = −32, clamped to a confident "0 L" over a
// day's driving.
//
// A climb does not care how long it takes. Fuel that entered the tank is in
// the tank, whether the number arrives in ten seconds or ninety minutes, so
// the total climb is the fill. That is one input — the trusted series — and
// one failure mode, against the five ways a stop could be missed.
//
// WHAT STILL HAS TO BE JUDGED. A rise must STICK: a slosh reads high for one
// fix and is gone, and counting it would invent fuel. That is the hold check
// below, and it is the only thing standing between this and a sensor's noise.
//
// WHERE THE REFUEL IS PLACED. At the stop the climb belongs to, not at the
// first rising reading — because damping means the number usually arrives
// after the truck has pulled away, which would drop the pin on the motorway.
// The stop containing the first rise wins; failing that, one that ended within
// riseStopLookbackMin before it; failing both, the rising fix itself, which is
// the honest answer when a truck fuelled without ever registering a stop.
func refuelsFromRises(ps []telemetry.Position, trusted []fuelPt, tank *float64) []RefuelEvent {
	var out []RefuelEvent
	for _, c := range risesInSeries(ps, trusted, tank) {
		if c.accepted {
			out = append(out, c.event)
		}
	}
	return out
}

// risesInSeries is the walk itself, returning every climb that stuck —
// including the small ones, which only a filling station can vouch for.
func risesInSeries(ps []telemetry.Position, trusted []fuelPt, tank *float64) []riseCandidate {
	if len(trusted) < 2 {
		return nil
	}
	stops := stopRuns(ps)

	var out []riseCandidate
	for i := 1; i < len(trusted); i++ {
		if trusted[i].lvl <= trusted[i-1].lvl {
			continue
		}
		// A climb starts. floor is the level it left, peak the highest it
		// reaches, and j walks to the end of the run.
		floor := trusted[i-1].lvl
		peak, peakIdx := trusted[i].lvl, i
		j := i
		for j+1 < len(trusted) {
			next := trusted[j+1].lvl
			if next > peak {
				peak, peakIdx = next, j+1
				j++
				continue
			}
			if peak-next <= riseNoisePct {
				j++ // a dip within noise: the climb continues
				continue
			}
			break
		}

		rise := peak - floor
		if rise >= riseMinPct && risenAndHeld(trusted, peakIdx, floor, rise) {
			ev, dwell := riseEvent(ps, stops, trusted[i], rise, tank)
			out = append(out, riseCandidate{
				event: ev, rise: rise, dwellMin: dwell,
				accepted: rise >= refuelMinPct,
			})
		}
		// Resume after the run either way. A climb that failed the test is
		// still a climb, and re-examining its own steps would find the same
		// thing again from a higher floor.
		i = j
	}
	return out
}

// risenAndHeld reports whether the level stayed up after the climb.
//
// Fuel in a tank does not go back where it came from. A slosh does: the sender
// reads high for a fix or two and returns. The level is followed for
// riseHoldMin after the peak, and any reading that gives back most of the
// climb disqualifies it.
//
// A climb at the very end of the window has nothing to check against and is
// ACCEPTED. The alternative is dropping a fill because the trip ended shortly
// after it, which is the failure this whole function exists to stop.
func risenAndHeld(trusted []fuelPt, peakIdx int, floor, rise float64) bool {
	deadline := trusted[peakIdx].p.Time.Add(time.Duration(riseHoldMin * float64(time.Minute)))
	for k := peakIdx + 1; k < len(trusted); k++ {
		if trusted[k].p.Time.After(deadline) {
			break
		}
		if trusted[k].lvl < floor+rise/2 {
			return false
		}
	}
	return true
}

// riseEvent builds the refuel, placed at the stop the climb belongs to.
func riseEvent(ps []telemetry.Position, stops []stopRun, first fuelPt, rise float64, tank *float64) (RefuelEvent, float64) {
	at, when := first.p, first.p.Time
	var dwell float64
	for _, s := range stops {
		switch {
		case !when.Before(s.begin) && !when.After(s.finish):
			at, when, dwell = s.at, s.begin, s.finish.Sub(s.begin).Minutes() // began while standing here
		case when.After(s.finish) && when.Sub(s.finish).Minutes() <= riseStopLookbackMin:
			at, when, dwell = s.at, s.begin, s.finish.Sub(s.begin).Minutes() // gauge caught up after pulling away
		}
	}

	re := RefuelEvent{
		Time: when, Lat: at.Lat, Lon: at.Lon,
		Address:  joinAddr(at.Jalan, at.Kota, at.Provinsi),
		Province: at.Provinsi, Pct: round1(rise),
	}
	if re.Lat == nil || re.Lon == nil {
		if lat, lon, addr, prov := nearestFixCoords(ps, when); lat != nil {
			re.Lat, re.Lon = lat, lon
			if re.Address == "" {
				re.Address = addr
			}
			if re.Province == "" {
				re.Province = prov
			}
		}
	}
	if tank != nil && *tank > 0 {
		liters := *tank * rise / 100
		price := fuelPrice(re.Province)
		re.Liters, re.PriceRp, re.CostRp = round1(liters), price, round0(liters*price)
	}
	return re, dwell
}
