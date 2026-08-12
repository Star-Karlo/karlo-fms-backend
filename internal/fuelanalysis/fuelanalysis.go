// Package fuelanalysis estimates fuel use from telemetry distance and each
// vehicle's configured efficiency — no fuel sensor required.
//
//	litres  = distance_km / fuel_ratio_kmpl
//	cost    = litres * price_per_litre
//	idle    = idle_hours * idle_litres_per_hour   (diesel burned standing still)
//	range   = fuel_tank_litres * fuel_ratio_kmpl  (a full-tank range)
//
// Distance and idle hours come from liveview's daily rollup (the same telemetry
// source the driver scorecard uses); the efficiency, tank size and — optionally
// — the fuel price come from the request or the vehicle record. A vehicle with
// no configured km/L, or no telemetry distance, still lists — with a null
// estimate — so gaps are visible rather than silently zero.
package fuelanalysis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Defaults for the Indonesian market. Solar/Biosolar (subsidised diesel) is the
// common fleet fuel; idle burn for a heavy diesel is ~2–3 L/h. All overridable
// per request so a fleet on Dexlite or with metered idle can be exact.
const (
	defaultPricePerLitre = 6800.0 // Rp/L, subsidised Solar
	defaultIdleLph       = 2.5    // litres/hour at idle
	defaultDays          = 30
	maxDays              = 35
)

type Handler struct {
	pool *db.Pool
	live *liveview.Service
}

func NewHandler(pool *db.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/fuel/analysis", h.analysis)
}

// Row is one vehicle's fuel picture over the window.
type Row struct {
	VehicleID    int64    `json:"vehicle_id"`
	LicensePlate string   `json:"license_plate"`
	VehicleType  *string  `json:"vehicle_type"`
	FleetGroup   *string  `json:"fleet_group"`
	DistanceKm   float64  `json:"distance_km"`
	IdleHours    float64  `json:"idle_hours"`
	Kmpl         *float64 `json:"kmpl"`        // configured efficiency
	TankLitres   *float64 `json:"tank_litres"` // configured tank size
	Litres       *float64 `json:"litres"`      // estimated fuel used (nil = no km/L set)
	Cost         *float64 `json:"cost"`        // estimated fuel cost
	IdleLitres   float64  `json:"idle_litres"` // estimated fuel burned idling
	IdleCost     float64  `json:"idle_cost"`   //
	RangeKm      *float64 `json:"range_km"`    // full-tank range
	Refuels      *float64 `json:"refuels"`     // litres / tank size
}

// Totals sums the fleet for the header cards.
type Totals struct {
	Vehicles   int     `json:"vehicles"`
	DistanceKm float64 `json:"distance_km"`
	Litres     float64 `json:"litres"`
	Cost       float64 `json:"cost"`
	IdleLitres float64 `json:"idle_litres"`
	IdleCost   float64 `json:"idle_cost"`
}

// Response is the analysis payload.
type Response struct {
	Days          int     `json:"days"`
	PricePerLitre float64 `json:"price_per_litre"`
	IdleLph       float64 `json:"idle_lph"`
	Rows          []Row   `json:"rows"`
	Totals        Totals  `json:"totals"`
}

// analysis godoc
//
//	@Summary		Fleet fuel analysis (estimated from telemetry distance)
//	@Description	Per-vehicle estimated fuel use and cost over the last N days: distance ÷ configured km/L, plus idle burn and full-tank range. Price and idle rate are overridable.
//	@Tags			fuel
//	@Security		BearerAuth
//	@Produce		json
//	@Param			days	query		int		false	"window in days (default 30, max 35)"
//	@Param			price	query		number	false	"price per litre (default 6800)"
//	@Param			idle_lph	query	number	false	"idle litres/hour (default 2.5)"
//	@Param			vehicle_id	query	int	false	"scope to one vehicle (0/omitted = whole fleet)"
//	@Success		200	{object}	fuelanalysis.Response
//	@Router			/v1/fuel/analysis [get]
func (h *Handler) analysis(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.EffectiveTenant(r)
	days := clampInt(qInt(r, "days", defaultDays), 1, maxDays)
	price := qFloat(r, "price", defaultPricePerLitre)
	idleLph := qFloat(r, "idle_lph", defaultIdleLph)
	// Optional: scope to one vehicle (the per-vehicle telemetry panel). 0 = fleet.
	onlyVehicle := int64(qInt(r, "vehicle_id", 0))

	// Telemetry distance + idle per vehicle over the window.
	series, err := h.live.FleetDailySeries(r.Context(), tenantID, days)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "telemetry unavailable")
		return
	}
	type usage struct{ dist, idle float64 }
	byVehicle := map[int64]usage{}
	for _, v := range series {
		u := byVehicle[v.VehicleID]
		for _, d := range v.Days {
			u.dist += d.DistanceKm
			u.idle += d.IdleH
		}
		byVehicle[v.VehicleID] = u
	}

	// The registry side: fuel config per vehicle (RLS-scoped).
	type vrow struct {
		id                int64
		plate             string
		vtype, fleetGroup *string
		tank, kmpl        *float64
	}
	var vehicles []vrow
	if err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT v.id, v.license_plate, v.vehicle_type, fg.name, v.fuel_tank_liters, v.fuel_ratio_kmpl
FROM vehicles v LEFT JOIN fleet_groups fg ON fg.id = v.fleet_group_id
WHERE v.status <> 'archive'
  AND ($1::bigint = 0 OR v.id = $1)
ORDER BY v.license_plate`, onlyVehicle)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x vrow
			if err := rows.Scan(&x.id, &x.plate, &x.vtype, &x.fleetGroup, &x.tank, &x.kmpl); err != nil {
				return err
			}
			vehicles = append(vehicles, x)
		}
		return rows.Err()
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "fuel analysis failed")
		return
	}

	out := Response{Days: days, PricePerLitre: price, IdleLph: idleLph, Rows: []Row{}}
	for _, v := range vehicles {
		u := byVehicle[v.id]
		row := Row{
			VehicleID: v.id, LicensePlate: v.plate, VehicleType: v.vtype, FleetGroup: v.fleetGroup,
			DistanceKm: round1(u.dist), IdleHours: round1(u.idle),
			Kmpl: v.kmpl, TankLitres: v.tank,
			IdleLitres: round1(u.idle * idleLph), IdleCost: round0(u.idle * idleLph * price),
		}
		// Fuel estimate only where an efficiency is configured.
		if v.kmpl != nil && *v.kmpl > 0 {
			litres := u.dist / *v.kmpl
			cost := litres * price
			row.Litres = ptr(round1(litres))
			row.Cost = ptr(round0(cost))
			out.Totals.Litres += litres
			out.Totals.Cost += cost
			if v.tank != nil && *v.tank > 0 {
				rng := *v.tank * *v.kmpl
				row.RangeKm = ptr(round0(rng))
				row.Refuels = ptr(round1(litres / *v.tank))
			}
		}
		out.Totals.DistanceKm += u.dist
		out.Totals.IdleLitres += u.idle * idleLph
		out.Totals.IdleCost += u.idle * idleLph * price
		out.Totals.Vehicles++
		out.Rows = append(out.Rows, row)
	}
	out.Totals.DistanceKm = round1(out.Totals.DistanceKm)
	out.Totals.Litres = round1(out.Totals.Litres)
	out.Totals.Cost = round0(out.Totals.Cost)
	out.Totals.IdleLitres = round1(out.Totals.IdleLitres)
	out.Totals.IdleCost = round0(out.Totals.IdleCost)
	httpx.JSON(w, http.StatusOK, out)
}

func qInt(r *http.Request, k string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(k))); err == nil {
		return v
	}
	return def
}
func qFloat(r *http.Request, k string, def float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get(k)), 64); err == nil && v > 0 {
		return v
	}
	return def
}
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func ptr(f float64) *float64   { return &f }
func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
func round0(f float64) float64 { return float64(int64(f + 0.5)) }
