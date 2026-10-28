package liveview

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Fuel-trip analysis: actual fuel used over a trip, from the vehicle's fuel-level
// sensor (Teltonika "Fuel Level (CAN)", a percentage) rather than the configured
// km/L estimate. Distance comes from the GPS track (haversine); fuel used comes
// from the sensor's level drops (refuels — level rises — are separated out).
// Real efficiency (km/L) then falls out of distance ÷ litres.
//
// The level is coarse (integer %), so a short trip can show 0% used and thus a
// null km/L — surfaced honestly in Note rather than reported as "infinite".

// fuelTripFields asks the telemetry API for the fuel-carrying metadata blob in
// addition to position — heavier than the trip-history projection, but a fuel
// trip is requested on demand, not on every history load.
var fuelTripFields = []string{"lat", "lon", "speed", "time", "ignition", "jalan", "kota", "provinsi", "metadata"}

const (
	fuelPricePerLitre = 6800.0 // Rp/L, subsidised Solar (national default)
	// A level rise this large counts as a refuel. Set well above sensor noise:
	// on bumpy roads the fuel sloshes and the sender momentarily reads high, so a
	// small blip is NOT a refuel — only a sustained double-digit jump is.
	refuelMinPct  = 12.0
	maxTripGapSec = 600.0 // gap between fixes over this = offline, not engine-on time

	// After the engine starts, a CAN fuel gauge re-initialises: it dips, then
	// climbs back to the real level. Readings this soon after ignition-on are
	// the gauge, not the tank.
	gaugeSettleSec = 120.0
	// Suspected theft: a single-transmission fuel DROP while the truck is STOPPED.
	// Judged in LITRES when a tank size is set (theftMinLiters — what an operator
	// actually watches for), falling back to a percentage otherwise. A drop while
	// moving, or across a long offline gap, is ordinary consumption, not theft.
	theftMinLiters = 5.0  // litres lost in one transmission (needs a tank size)
	theftMinPct    = 15.0 // fallback threshold when the vehicle has no tank size
	theftMaxGapMin = 30.0

	// A drop has to be big enough that the SENSOR could have meant it.
	//
	// These senders report whole percent, so one step on a 340 L tank is 3.4 L
	// — and the 5 L floor above is less than two of them. K-9467-BF's sender
	// wanders in a 54-58% band while parked, and every downward wobble in that
	// band cleared the bar: four "thefts" in two days, 7 to 14 L each, none of
	// them fuel. Three steps is the smallest move that is clearly the tank and
	// not the quantiser.
	theftMinSensorSteps = 3.0

	// ...and slow enough that fuel could physically have left.
	//
	// Two of those four drained their litres in FOUR SECONDS and THREE. That is
	// 200+ L/min; a forecourt pump manages about 40, and a hose and jerrycan far
	// less. A drop faster than this is the sender changing its mind, whatever
	// else it might be — nothing was siphoned.
	theftMaxLitresPerMin = 60.0
)

// Demo fuel-theft: one clearly-flagged sample event for a specific truck +
// window, so the historical view can be shown end-to-end before real siphoning
// data exists. Guarded tightly by IMEI + date so it never appears elsewhere.
//
// OFF BY DEFAULT. The IMEI below belongs to a real vehicle in a real account,
// so with the demo on, that customer sees two fabricated thefts — with
// locations and rupiah amounts — rendered exactly like genuine detections, and
// nothing in the UI distinguishes them. A demo that is indistinguishable from
// evidence has to be opt-in, per environment.
//
// Set FUEL_DEMO_THEFT=true in a demo environment to show it.
var demoTheftEnabled = config.EnvBool("FUEL_DEMO_THEFT", false)

const mockTheftIMEI = "354002392595920"

var (
	mockTheftFrom = time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	mockTheftTo   = time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) // through Aug 8 (exclusive of Aug 9)
)

// mockTheftSpots pins the demo thefts to FIXED moments inside the window, so each
// one shows up once, at the same place, only when the queried range covers that
// moment — instead of floating to 25%/70% of whatever slice is on screen. Times
// are UTC (WIB−7); the spot snaps onto the real route position nearest that time.
var mockTheftSpots = []struct {
	at           time.Time
	liters       float64
	fbLat, fbLon float64 // fallback location if the route has no fix near `at`
	fbAddr       string
}{
	{time.Date(2026, 8, 7, 4, 34, 0, 0, time.UTC), 44, -7.05, 111.30, "Grobogan, Jawa Tengah"}, // ~11:34 WIB
	{time.Date(2026, 8, 7, 9, 55, 0, 0, time.UTC), 49, -6.75, 111.55, "Rembang, Jawa Tengah"},  // ~16:55 WIB
}

// fuelPriceByProvince overrides the national Solar/Biosolar price (Rp/L) where a
// region differs — subsidised Biosolar is broadly flat nationally, but non-
// subsidised diesel (Dexlite/Pertamina Dex) is higher and varies. Left mostly
// empty on purpose: fill from a verified source (there is no reliable official
// price API) rather than shipping guessed numbers. Missing province -> default.
var fuelPriceByProvince = map[string]float64{}

// detectTheft is the real detection: a fuel-level DROP of dropPct over gapMin
// minutes that's too large + too fast to be consumption is flagged as theft.
// mock tags the event when the underlying reading is fabricated (the detector
// itself is identical either way).
func detectTheft(dropPct, gapMin float64, prev, pt telemetry.Position, tank *float64, mock bool) (TheftEvent, bool) {
	// One transmission, short gap — a drop spread over a long offline gap is
	// consumption, not a siphon.
	if gapMin < 0 || gapMin > theftMaxGapMin {
		return TheftEvent{}, false
	}
	// Theft happens at a STOP — you can't siphon a moving truck, and a drop while
	// driving is ordinary burn. (Mock/demo events carry no speed and pass.)
	if pt.Speed != nil && *pt.Speed > movingSpeed {
		return TheftEvent{}, false
	}
	// ...and "stopped" means the TRUCK is still, not merely that its speed
	// rounded to zero. K-9467-BF was shunted a few metres twice on 21 October:
	// speed 0, Movement 1, position changed, fuel dipped three points and came
	// back within ninety seconds. That is the tank sloshing, and it was being
	// reported as a siphon.
	for _, md := range []json.RawMessage{prev.Metadata, pt.Metadata} {
		if moving, known := deviceMoving(md); known && moving {
			return TheftEvent{}, false
		}
	}
	// An ignition transition is not a loss either. The other two of those four
	// landed exactly on the engine stopping: 58% with the engine running,
	// 54% three seconds later with it off. The level the sender reports shifts
	// with the engine state; the fuel does not.
	if prev.Ignition != nil && pt.Ignition != nil && *prev.Ignition != *pt.Ignition {
		return TheftEvent{}, false
	}
	hasTank := tank != nil && *tank > 0
	var liters float64
	if hasTank {
		// Judge in litres: a single-transmission loss of more than
		// theftMinLiters, and of at least three of the sensor's own steps so
		// a 1%-resolution sender's wobble cannot raise the alarm.
		liters = *tank * dropPct / 100
		if liters < theftMinLiters || liters < theftMinSensorSteps*(*tank)/100 {
			return TheftEvent{}, false
		}
		// Could fuel have left this fast? Below a second of elapsed time the
		// question is meaningless — two readings in the same moment — and the
		// answer is the same either way: not a siphon.
		if gapMin <= 0 || liters/gapMin > theftMaxLitresPerMin {
			return TheftEvent{}, false
		}
	} else if dropPct < theftMinPct {
		return TheftEvent{}, false // no tank size — fall back to the % threshold
	}
	te := TheftEvent{
		Time: pt.Time, Lat: pt.Lat, Lon: pt.Lon,
		Address: joinAddr(pt.Jalan, pt.Kota, pt.Provinsi), Pct: round1(dropPct), IsMock: mock,
	}
	if hasTank {
		te.Liters, te.CostRp = round1(liters), round0(liters*fuelPrice(pt.Provinsi))
	}
	return te, true
}

// dedupThefts collapses theft events that are within ~250 m of an earlier one —
// a single siphoning shows up across several nearby readings, but it's one spot.
func dedupThefts(ts []TheftEvent) []TheftEvent {
	out := []TheftEvent{}
	for _, t := range ts {
		dup := false
		for _, k := range out {
			if t.Lat != nil && t.Lon != nil && k.Lat != nil && k.Lon != nil && haversine(*t.Lat, *t.Lon, *k.Lat, *k.Lon) < 0.25 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, t)
		}
	}
	return out
}

// dedupRefuels collapses refuel readings that are the SAME fill — within ~250 m
// and an hour of an earlier one — summing the rise, so one top-up shows as one
// event rather than several. Separate fills (a different place or a different
// time) stay apart.
func dedupRefuels(rs []RefuelEvent) []RefuelEvent {
	out := []RefuelEvent{}
	for _, r := range rs {
		merged := false
		for i := range out {
			k := &out[i]
			near := r.Lat != nil && r.Lon != nil && k.Lat != nil && k.Lon != nil &&
				haversine(*r.Lat, *r.Lon, *k.Lat, *k.Lon) < 0.25
			soon := r.Time.Sub(k.Time) < time.Hour && k.Time.Sub(r.Time) < time.Hour // |Δt| < 1h
			if near && soon {
				k.Pct = round1(k.Pct + r.Pct)
				k.Liters = round1(k.Liters + r.Liters)
				k.CostRp = round0(k.CostRp + r.CostRp)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, r)
		}
	}
	return out
}

// fuelPt is one fuel-level reading with the fix it came on, so an event keeps
// its time and place.
type fuelPt struct {
	lvl float64
	p   telemetry.Position
}

// trustedLevels keeps the fuel readings a CAN gauge can be believed on: the
// engine running, and past the first gaugeSettleSec after it started.
//
// Observed on K-9467-BF, 26-27 October 2026. Parked all night at 52%, the
// tracker reported 8% for the 37 minutes the engine was off after a brief
// start, and 54% the moment it ran again: 150 L that neither burned nor came
// back, and it showed up as a refuel. At a pump on the Surabaya–Gempol toll,
// the gauge read 15% eleven seconds after the engine restarted, then 32, 48,
// 55, 61 over six minutes of driving: a damped gauge catching up with a fill,
// not fuel arriving at 30 km/h. With the engine off, or in the first two
// minutes after it starts, the reading is the gauge, not the tank.
//
// Ignition is followed over every fix, not only those carrying a level: the
// engine often starts on a fix that has no fuel reading. A device that never
// reports ignition keeps every reading; there is nothing to judge it by.
func trustedLevels(ps []telemetry.Position) []fuelPt {
	var out []fuelPt
	on, known := false, false
	var onSince time.Time // zero: the engine was already running when the window opened
	for _, p := range ps {
		if p.Ignition != nil {
			if *p.Ignition && known && !on {
				onSince = p.Time
			}
			on, known = *p.Ignition, true
		}
		lvl, ok := fuelLevelPct(p.Metadata)
		if !ok {
			continue
		}
		if p.Ignition == nil && !known {
			out = append(out, fuelPt{lvl, p})
			continue
		}
		if !on || p.Time.Sub(onSince).Seconds() < gaugeSettleSec {
			continue
		}
		out = append(out, fuelPt{lvl, p})
	}
	return out
}

// nearestFixCoords finds the fix closest in time to t that actually has a GPS
// coordinate, and returns its lat/lon plus address parts. Used to place an event
// (a refuel) whose own fix reported a sensor value but no position, so it can be
// pinned on the map instead of falling at (0,0). Returns nil lat if no fix in the
// window has coords at all.
func nearestFixCoords(ps []telemetry.Position, t time.Time) (lat, lon *float64, address, province string) {
	var best *telemetry.Position
	var bestGap time.Duration
	for i := range ps {
		if ps[i].Lat == nil || ps[i].Lon == nil {
			continue
		}
		gap := ps[i].Time.Sub(t)
		if gap < 0 {
			gap = -gap
		}
		if best == nil || gap < bestGap {
			best, bestGap = &ps[i], gap
		}
	}
	if best == nil {
		return nil, nil, "", ""
	}
	return best.Lat, best.Lon, joinAddr(best.Jalan, best.Kota, best.Provinsi), best.Provinsi
}

// mockTheftEvents fabricates the demo: the fixed-timestamp thefts (see
// mockTheftSpots) whose moment falls inside [from,to]. Each is placed on the real
// route fix nearest its time (fallback coords if none within 2h), so it appears
// once, in the same place, only when the window covers it.
func mockTheftEvents(from, to time.Time, ps []telemetry.Position, tank *float64) []TheftEvent {
	tankL := 250.0 // a typical truck tank, so the % reads sensibly
	if tank != nil && *tank > 0 {
		tankL = *tank
	}
	out := []TheftEvent{}
	for _, s := range mockTheftSpots {
		if s.at.Before(from) || !s.at.Before(to) {
			continue // this theft's moment isn't in the queried window
		}
		te := TheftEvent{IsMock: true, Time: s.at, Pct: round1(s.liters / tankL * 100), Liters: s.liters, CostRp: round0(s.liters * fuelPricePerLitre)}
		// Snap onto the actual route: the fix closest in time to the fixed moment.
		var best *telemetry.Position
		var bestGap time.Duration
		for i := range ps {
			p := ps[i]
			if p.Lat == nil || p.Lon == nil || p.Time.IsZero() {
				continue
			}
			gap := p.Time.Sub(s.at)
			if gap < 0 {
				gap = -gap
			}
			if best == nil || gap < bestGap {
				best, bestGap = &ps[i], gap
			}
		}
		if best != nil && bestGap <= 2*time.Hour {
			te.Lat, te.Lon, te.Address = best.Lat, best.Lon, joinAddr(best.Jalan, best.Kota, best.Provinsi)
		} else {
			lat, lon := s.fbLat, s.fbLon
			te.Lat, te.Lon, te.Address = &lat, &lon, s.fbAddr
		}
		out = append(out, te)
	}
	return out
}

// fuelPrice returns the Rp/L for a province, or the national default.
func fuelPrice(province string) float64 {
	if p, ok := fuelPriceByProvince[province]; ok && p > 0 {
		return p
	}
	return fuelPricePerLitre
}

// RefuelEvent is one detected refuel (a fuel-level rise) with where + how much.
type RefuelEvent struct {
	Time     time.Time `json:"time"`
	Lat      *float64  `json:"lat"`
	Lon      *float64  `json:"lon"`
	Address  string    `json:"address"`
	Province string    `json:"province"`
	Pct      float64   `json:"pct"`      // level rise
	Liters   float64   `json:"liters"`   // tank × pct
	PriceRp  float64   `json:"price_rp"` // Rp/L applied (regional)
	CostRp   float64   `json:"cost_rp"`  // liters × price
}

// TheftEvent is a suspected fuel theft (a large, fast level DROP that burning
// fuel can't explain — e.g. siphoning while parked). IsMock flags demo data.
type TheftEvent struct {
	Time    time.Time `json:"time"`
	Lat     *float64  `json:"lat"`
	Lon     *float64  `json:"lon"`
	Address string    `json:"address"`
	Pct     float64   `json:"pct"`     // level drop
	Liters  float64   `json:"liters"`  // tank × pct
	CostRp  float64   `json:"cost_rp"` // liters × price (value lost)
	IsMock  bool      `json:"is_mock"` // demo/sample event, not real telemetry
}

// FuelTrip is the actual-fuel picture for one vehicle over a time range.
type FuelTrip struct {
	Points     int     `json:"points"`
	DistanceKm float64 `json:"distance_km"`

	FuelReadings int      `json:"fuel_readings"` // points that carried a fuel level
	FuelStartPct *float64 `json:"fuel_start_pct"`
	FuelEndPct   *float64 `json:"fuel_end_pct"`

	TankLiters    *float64 `json:"tank_liters"`     // from the vehicle record
	FuelRatioKmpl *float64 `json:"fuel_ratio_kmpl"` // configured, for comparison

	UsedPct    *float64 `json:"used_pct"`    // total drop (consumption), refuel-adjusted
	UsedLiters *float64 `json:"used_liters"` // tank × used_pct (nil = no tank set)
	CostRp     *float64 `json:"cost_rp"`     // used_liters × price

	RefuelPct    *float64      `json:"refuel_pct"`
	RefuelLiters *float64      `json:"refuel_liters"`
	RefuelEvents int           `json:"refuel_events"`
	Refuels      []RefuelEvent `json:"refuels"` // where + how much + cost, per refuel

	// Suspected fuel thefts (large fast drops). Always surfaced on the history —
	// no toggle — because a theft is not something to opt into seeing.
	Thefts []TheftEvent `json:"thefts"`

	KmplActual     *float64 `json:"kmpl_actual"`     // distance ÷ used_liters (sensor-measured)
	KmplConfigured *float64 `json:"kmpl_configured"` // = fuel_ratio_kmpl (echo)

	// Operating time over the window, from ignition + speed per fix.
	EngineHours *float64 `json:"engine_hours"` // ignition-on time
	IdleHours   *float64 `json:"idle_hours"`   // engine-on but speed ≈ 0
	MovingHours *float64 `json:"moving_hours"` // engine-on and moving

	// Cost efficiency. Fuel cost basis is sensor-measured when a fuel sensor +
	// tank are set (captures idle burn); otherwise the configured km/L rate
	// (which does NOT see idle, so it under-counts on idle-heavy trips).
	CostBasis   string   `json:"cost_basis,omitempty"` // "sensor" | "rate" | ""
	CostPerKm   *float64 `json:"cost_per_km"`          // fuel cost ÷ distance
	CostPerHour *float64 `json:"cost_per_hour"`        // fuel cost ÷ engine-on hours

	Note string `json:"note,omitempty"`
}

// fuelTrip godoc
//
//	@Summary		Actual fuel used over a trip (from the fuel-level sensor)
//	@Description	Distance travelled (GPS) + fuel-level sensor drops → litres used, refuels, and REAL km/L for the window. Falls back to a note when the vehicle has no fuel sensor or the level did not move.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			vehicle_id	path		int		true	"vehicle id"
//	@Param			from		query		string	true	"RFC3339"
//	@Param			to			query		string	true	"RFC3339"
//	@Success		200	{object}	liveview.FuelTrip
//	@Router			/v1/vehicles/{vehicle_id}/fuel-trip [get]
func (h *Handler) fuelTrip(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	h.fuelTripFor(w, r, id)
}

// fuelTripFor is the body of the endpoint, shared with the by-plate route so the
// two cannot drift apart in their parameters or their output.
func (h *Handler) fuelTripFor(w http.ResponseWriter, r *http.Request, id int64) {
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}
	out, err := h.svc.FuelTrip(r.Context(), auth.EffectiveTenant(r), id, from, to)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "fuel trip unavailable: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// FuelTrip computes the actual-fuel analysis for a vehicle over [from,to].
func (s *Service) FuelTrip(ctx context.Context, tenantID, vehicleID int64, from, to time.Time) (*FuelTrip, error) {
	var imei string
	var ratio, tank *float64
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT t.imei, v.fuel_ratio_kmpl, v.fuel_tank_liters
			 FROM vehicles v JOIN trackers t ON t.id = v.tracker_id
			 WHERE v.id = $1`, vehicleID).Scan(&imei, &ratio, &tank)
	}); err != nil {
		return nil, err
	}

	ps, err := s.tel.History(ctx, imei, from, to, fuelTripFields, 5000)
	if err != nil {
		return nil, err
	}
	ps = telemetry.FilterTeleports(ps)

	out := &FuelTrip{Points: len(ps), TankLiters: tank, FuelRatioKmpl: ratio, KmplConfigured: ratio, Refuels: []RefuelEvent{}}

	// Distance from the GPS track, and operating time from ignition + speed.
	// Time is summed over the gaps BETWEEN consecutive fixes; a gap longer than
	// maxTripGapSec is treated as offline (parked, not reporting) and skipped so
	// an overnight stop doesn't inflate engine hours.
	var lastLat, lastLon *float64
	var engineSec, idleSec, movingSec float64
	// Fuel burned from the ECU's instantaneous fuel-rate (L/h), integrated over
	// engine-on time. This is the OBD path: vehicles that report "Fuel Rate" but
	// have no tank-level sensor still get real consumption/km-L/cost (and it
	// captures idle burn, which the configured km/L rate does not).
	var rateLiters float64
	for i := range ps {
		p := ps[i]
		if p.Lat != nil && p.Lon != nil {
			if lastLat != nil {
				out.DistanceKm += haversine(*lastLat, *lastLon, *p.Lat, *p.Lon)
			}
			lastLat, lastLon = p.Lat, p.Lon
		}
		if i > 0 && p.Ignition != nil && *p.Ignition {
			dt := p.Time.Sub(ps[i-1].Time).Seconds()
			if dt > 0 && dt <= maxTripGapSec {
				engineSec += dt
				if p.Speed != nil && *p.Speed > movingSpeed {
					movingSec += dt
				} else {
					idleSec += dt
				}
				if r, ok := fuelRate(p.Metadata); ok && r > 0 {
					rateLiters += r * dt / 3600.0 // L/h × h
				}
			}
		}
	}
	out.DistanceKm = round1(out.DistanceKm)
	if engineSec > 0 {
		out.EngineHours = ptrF(round1(engineSec / 3600))
		out.IdleHours = ptrF(round1(idleSec / 3600))
		out.MovingHours = ptrF(round1(movingSec / 3600))
	}

	// Rp/km and Rp/hour, from the best fuel-cost basis available. Runs on both
	// exit paths (with or without a usable fuel sensor), so it's a closure.
	finish := func() {
		// Thefts: for the demo truck + window show exactly two mock spots at
		// DIFFERENT locations; otherwise dedupe close real detections (one
		// siphoning shows as several nearby readings) into one spot each.
		//
		// Note the demo REPLACES real detections rather than adding to them, so
		// while it was unconditionally on, any genuine siphoning on this truck
		// in this window was silently discarded. Another reason it defaults off.
		if demoTheftEnabled && imei == mockTheftIMEI && from.Before(mockTheftTo) && to.After(mockTheftFrom) {
			out.Thefts = mockTheftEvents(from, to, ps, tank)
		} else {
			out.Thefts = dedupThefts(out.Thefts)
		}

		var costRp float64
		switch {
		case out.CostRp != nil && *out.CostRp > 0:
			costRp, out.CostBasis = *out.CostRp, "sensor"
		case ratio != nil && *ratio > 0 && out.DistanceKm > 0:
			costRp, out.CostBasis = out.DistanceKm / *ratio * fuelPricePerLitre, "rate"
		default:
			return
		}
		if out.DistanceKm > 0 {
			out.CostPerKm = ptrF(round0(costRp / out.DistanceKm))
		}
		if engineSec > 0 {
			out.CostPerHour = ptrF(round0(costRp / (engineSec / 3600)))
		}
	}

	// Fuel level (%) per point, keeping the point so a refuel keeps its location.
	var fps []fuelPt
	for _, p := range ps {
		if lvl, ok := fuelLevelPct(p.Metadata); ok {
			fps = append(fps, fuelPt{lvl, p})
		}
	}
	out.FuelReadings = len(fps)

	if len(fps) < 2 {
		// No tank-level sensor. If the ECU reported a fuel RATE, we still have real
		// consumption — just not refuels/theft (those need a level to see a jump).
		if rateLiters > 0 {
			used := round1(rateLiters)
			costRp := rateLiters * fuelPricePerLitre
			cost := round0(costRp)
			out.UsedLiters, out.CostRp, out.CostBasis = &used, &cost, "obd_rate"
			if tank != nil && *tank > 0 {
				up := round1(rateLiters / *tank * 100)
				out.UsedPct = &up
			}
			if out.DistanceKm > 0 {
				out.KmplActual = ptrF(round1(out.DistanceKm / rateLiters))
				out.CostPerKm = ptrF(round0(costRp / out.DistanceKm))
			}
			if engineSec > 0 {
				out.CostPerHour = ptrF(round0(costRp / (engineSec / 3600)))
			}
			out.Note = "Fuel from the engine's OBD fuel-rate — consumption, km/L and cost. Refuels and theft need a tank-level sensor."
			return out, nil
		}
		out.Note = "No fuel-sensor readings in this window — showing distance only."
		finish() // still yields Rp/km + Rp/hour from the configured km/L rate
		return out, nil
	}

	// Theft: walk the raw series for large fast drops. Consumption is NOT summed
	// per drop — that counts a noisy sensor's down-and-up jitter as burnt fuel
	// and over-counts badly — it is derived from the tank BALANCE below
	// (start − end + refuels − thefts).
	for i := 1; i < len(fps); i++ {
		if d := fps[i-1].lvl - fps[i].lvl; d > 0 {
			// A big drop over a short interval is theft, not consumption — the
			// SAME detector the mock runs through.
			if te, ok := detectTheft(d, fps[i].p.Time.Sub(fps[i-1].p.Time).Minutes(), fps[i-1].p, fps[i].p, tank, false); ok {
				out.Thefts = append(out.Thefts, te)
			}
		}
	}

	// The balance is taken between readings the gauge can be believed on. The
	// raw first and last readings are not that: a window that ends while the
	// engine is off can end on a level the tank never had (see trustedLevels).
	trusted := trustedLevels(ps)
	if len(trusted) < 2 {
		// The engine was off, or only just started, for the whole window. Falling
		// back to the raw readings would bring back exactly the level the tank
		// never had, so say plainly that the gauge could not be read.
		out.Note = "The engine was off for almost all of this period, and the fuel gauge cannot be " +
			"read with the engine off. Distance and any suspected losses are shown; fuel used and refuels are not."
		finish()
		return out, nil
	}
	start, end := trusted[0].lvl, trusted[len(trusted)-1].lvl
	out.FuelStartPct, out.FuelEndPct = &start, &end

	// Refuels: every climb in the trusted series that sticks. See
	// refuelsFromRises for why this is no longer judged across stops.
	// A known SPBU under the truck vouches for a fill too small for the
	// sensor's own floor — it never invents one. See promoteAtStations.
	out.Refuels = s.promoteAtStations(ctx, tenantID, risesInSeries(ps, trusted, tank))
	// Two stops at the same pump (the truck pulled forward) are one fill.
	out.Refuels = dedupRefuels(out.Refuels)
	out.RefuelEvents = len(out.Refuels)
	var refuelPct float64
	for _, re := range out.Refuels {
		refuelPct += re.Pct
	}
	var theftPct float64
	for _, tf := range out.Thefts {
		theftPct += tf.Pct
	}
	// Fuel actually burned, by tank balance: what left the tank is the net level
	// fall plus everything that was added (refuels) minus what was siphoned
	// (thefts). This reconciles with the level + refuels a user can see, and does
	// not accumulate sensor jitter. Clamp at 0 (can't burn a negative amount).
	usedPct := start - end + refuelPct - theftPct
	// Kept before the clamp, because the clamp destroys the evidence that the
	// sum failed: a balance of -18 and a trip that genuinely burned nothing
	// both leave 0 behind, and they want opposite words.
	balanceFailed := usedPct < 0
	if usedPct < 0 {
		usedPct = 0
	}
	out.UsedPct = ptrF(round1(usedPct))
	if refuelPct > 0 {
		out.RefuelPct = ptrF(round1(refuelPct))
	}

	if tank != nil && *tank > 0 {
		usedL := *tank * usedPct / 100
		out.UsedLiters = ptrF(round1(usedL))
		out.CostRp = ptrF(round0(usedL * fuelPricePerLitre))
		if refuelPct > 0 {
			out.RefuelLiters = ptrF(round1(*tank * refuelPct / 100))
		}
		if usedL > 0 && out.DistanceKm > 0 {
			out.KmplActual = ptrF(round1(out.DistanceKm / usedL))
		}
	}

	// The note has to distinguish "nothing happened" from "the sum failed",
	// because the clamp above makes them look identical and they are not.
	//
	// Observed on K-9467-BF, 20-21 October: the balance came out NEGATIVE — the
	// level rose 46 points over a refuel the sensor sampled in pieces, and
	// refuels + thefts outweighed the fall — so used clamped to 0 and the panel
	// reported "0 L, Rp 0, fuel level did not change measurably" for a truck
	// that had driven 74 km and burned 26.6 L by its own CAN counter. Every
	// part of that sentence was wrong, and it read as reassurance.
	switch {
	case tank == nil || *tank == 0:
		out.Note = "Set a Fuel Tank (L) on the vehicle to convert % used into litres and km/L."
	case balanceFailed:
		// Deliberately silent, at the product owner's request (28 Oct 2026).
		//
		// This branch used to carry a paragraph explaining that the tank
		// balance had come out below zero. It was accurate and it was the
		// thing that finally made this fault visible — but it is English, it
		// is four lines long, and it sits on a panel a customer reads, where
		// CLAUDE.md asks for Bahasa and for brevity.
		//
		// The case is NOT silent to the reader: the console shows a dash
		// instead of a figure and a one-line Bahasa caveat of its own, so
		// nothing is reported as measured when it is not. This is the long
		// English version being dropped, not the warning.
		//
		// The branch stays rather than being deleted, so a failed balance
		// cannot fall through to the sentences below and be described as a
		// level that did not move — which is exactly the wrong explanation
		// this branch was added to stop.
	case usedPct == 0 && start != end:
		// The sum came out at exactly zero while the gauge moved. Rarer than
		// the case above and not the same thing: here the terms cancelled
		// rather than went negative.
		out.Note = "Fuel used came out at zero even though the level moved, so the refuels or " +
			"suspected losses recorded for this period cancel the fall exactly. The litres and " +
			"cost cannot be relied on."
	case usedPct == 0:
		out.Note = "Fuel level did not change measurably this trip (coarse 1% sensor) — km/L needs a longer trip."
	}
	finish()
	return out, nil
}

// fuelLevelPct pulls "Fuel Level (CAN)" (a percentage) from a point's metadata.
func fuelLevelPct(md json.RawMessage) (float64, bool) {
	if len(md) == 0 {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal(md, &m); err != nil {
		return 0, false
	}
	for _, k := range []string{"Fuel Level (CAN)", "Fuel Level", "fuel_level"} {
		if v, ok := m[k]; ok {
			if f, ok := ioNum(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// deviceMoving reads the tracker's own Movement flag.
//
// Distinct from speed, and that difference is the whole point: a truck being
// shunted a few metres around a yard reports speed 0 with Movement 1, and the
// fuel in its tank sloshes exactly as much as it would at 40 km/h. Judging
// "is it stationary" by speed alone called that a theft.
func deviceMoving(md json.RawMessage) (bool, bool) {
	if len(md) == 0 {
		return false, false
	}
	var m map[string]any
	if err := json.Unmarshal(md, &m); err != nil {
		return false, false
	}
	for _, k := range []string{"Movement", "movement"} {
		if v, ok := m[k]; ok {
			if f, ok := ioNum(v); ok {
				return f > 0, true
			}
		}
	}
	return false, false
}

// fuelRate reads the ECU's instantaneous fuel consumption rate (litres/hour)
// from an OBD device's metadata. Distinct from fuel LEVEL: rate is what's being
// burned now, which integrates to consumption; it can't reveal a refuel or theft.
func fuelRate(md json.RawMessage) (float64, bool) {
	if len(md) == 0 {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal(md, &m); err != nil {
		return 0, false
	}
	for _, k := range []string{"Fuel Rate", "Fuel Rate (CAN)", "fuel_rate"} {
		if v, ok := m[k]; ok {
			if f, ok := ioNum(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

// ioNum coerces a JSON metadata value (number or numeric string) to float64.
func ioNum(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func ptrF(f float64) *float64  { return &f }
func round0(f float64) float64 { return float64(int64(f + 0.5)) }

// joinAddr builds a compact address from the geocoded parts, skipping blanks.
func joinAddr(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += p
	}
	return out
}
