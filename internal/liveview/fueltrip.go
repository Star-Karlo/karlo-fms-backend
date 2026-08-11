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
)

// fuelPriceByProvince overrides the national Solar/Biosolar price (Rp/L) where a
// region differs — subsidised Biosolar is broadly flat nationally, but non-
// subsidised diesel (Dexlite/Pertamina Dex) is higher and varies. Left mostly
// empty on purpose: fill from a verified source (there is no reliable official
// price API) rather than shipping guessed numbers. Missing province -> default.
var fuelPriceByProvince = map[string]float64{}

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
	type fuelPt struct {
		lvl float64
		p   telemetry.Position
	}
	var fps []fuelPt
	for _, p := range ps {
		if lvl, ok := fuelLevelPct(p.Metadata); ok {
			fps = append(fps, fuelPt{lvl, p})
		}
	}
	out.FuelReadings = len(fps)
	if len(fps) < 2 {
		out.Note = "No fuel-sensor readings in this window — showing distance only."
		finish() // still yields Rp/km + Rp/hour from the configured km/L rate
		return out, nil
	}

	start, end := fps[0].lvl, fps[len(fps)-1].lvl
	out.FuelStartPct, out.FuelEndPct = &start, &end

	// Walk the level series: sum drops as consumption, large rises as refuels.
	var consumedPct, refuelPct float64
	prev := fps[0].lvl
	for _, fp := range fps[1:] {
		cur := fp.lvl
		switch d := prev - cur; {
		case d > 0:
			consumedPct += d
		case -d >= refuelMinPct:
			risePct := -d
			refuelPct += risePct
			out.RefuelEvents++
			// Record where + how much (litres/cost need a tank size).
			re := RefuelEvent{
				Time: fp.p.Time, Lat: fp.p.Lat, Lon: fp.p.Lon,
				Address: joinAddr(fp.p.Jalan, fp.p.Kota, fp.p.Provinsi),
				Province: fp.p.Provinsi, Pct: round1(risePct),
			}
			if tank != nil && *tank > 0 {
				liters := *tank * risePct / 100
				price := fuelPrice(fp.p.Provinsi)
				re.Liters, re.PriceRp, re.CostRp = round1(liters), price, round0(liters*price)
			}
			out.Refuels = append(out.Refuels, re)
			// (small rises are sensor noise — ignored, neither consumed nor refuel)
		}
		prev = cur
	}
	out.UsedPct = ptrF(round1(consumedPct))
	if refuelPct > 0 {
		out.RefuelPct = ptrF(round1(refuelPct))
	}

	if tank != nil && *tank > 0 {
		usedL := *tank * consumedPct / 100
		out.UsedLiters = ptrF(round1(usedL))
		out.CostRp = ptrF(round0(usedL * fuelPricePerLitre))
		if refuelPct > 0 {
			out.RefuelLiters = ptrF(round1(*tank * refuelPct / 100))
		}
		if usedL > 0 && out.DistanceKm > 0 {
			out.KmplActual = ptrF(round1(out.DistanceKm / usedL))
		}
	}

	switch {
	case tank == nil || *tank == 0:
		out.Note = "Set a Fuel Tank (L) on the vehicle to convert % used into litres and km/L."
	case consumedPct == 0:
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
