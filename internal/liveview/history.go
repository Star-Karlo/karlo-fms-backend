package liveview

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/karlo/fms-backend/internal/auth"
)

// Fleet history: what the fleet DID, as opposed to what it is doing.
//
// The numbers come from the tracking service's vehicle_day rollup, which
// pre-aggregates raw telemetry into per-device days. That indirection is the
// whole point — computing "idle hours by fleet group last week" from raw
// samples is a ~70s scan of 11M rows, and the report path that does it per
// vehicle over HTTP could never have answered it fleet-wide.
//
// This layer does the part the tracking service cannot: joining IMEIs to
// vehicles, which live in a different database behind RLS.

// historyTTL is how long a computed window is reused.
//
// The upstream rollup only refreshes hourly, so a shorter TTL would re-do the
// join for data that cannot have changed. Today's row does move within the
// hour, which is why this is minutes rather than an hour.
const historyTTL = 5 * time.Minute

// maxHistoryDays bounds the window.
//
// This was 8, on the assumption that the rollup ages out with the telemetry it
// is built from. It does not: `telemetry` and `mobile_telemetry` are
// hypertables with an 8-day retention policy, but `vehicle_day` is a plain
// table with no policy at all, and /v1/stats/daily happily serves every day it
// holds. The old cap was therefore discarding history that exists — which
// mattered most to anything comparing a period against an earlier one, since
// the comparison silently had almost nothing to compare against.
//
// 35 rather than unbounded because the response is per-device per-day: at ~1,900
// devices a 35-day window is already ~66,000 rows in one JSON body. That covers
// a full month plus a margin for month-over-month, which is the longest window
// anything here asks for, and it is cached for historyTTL.
const maxHistoryDays = 35

// DayTotals is one calendar day, fleet-wide.
type DayTotals struct {
	Day        string  `json:"day"`
	MovingH    float64 `json:"moving_h"`
	IdleH      float64 `json:"idle_h"`
	ParkingH   float64 `json:"parking_h"`
	DistanceKm float64 `json:"distance_km"`
	// Active counts vehicles that reported at all; Unused counts those that
	// reported but never moved — an idle asset, not an offline one.
	Active int `json:"active"`
	Unused int `json:"unused"`
	// IdlePct is idle as a share of engine-on time, over vehicles that ACTUALLY
	// MOVED that day. Including never-moved vehicles folds stuck-ignition
	// sensors into the fleet's headline waste number: 13 devices report the
	// engine on for 24h while covering 0 km, and they are a wiring fault, not
	// a driver leaving an engine running.
	IdlePct float64 `json:"idle_pct"`
}

// VehicleTotals is one vehicle summed over the window.
type VehicleTotals struct {
	VehicleID    int64   `json:"vehicle_id"`
	LicensePlate string  `json:"license_plate"`
	FleetGroup   *string `json:"fleet_group_name"`
	VehicleType  *string `json:"vehicle_type"`
	MovingH      float64 `json:"moving_h"`
	IdleH        float64 `json:"idle_h"`
	DistanceKm   float64 `json:"distance_km"`
	IdlePct      float64 `json:"idle_pct"`
	DaysUsed     int     `json:"days_used"`
	MaxSpeed     float64 `json:"max_speed"`
}

// FleetHistory is the whole response.
type FleetHistory struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Days     []DayTotals     `json:"days"`
	Vehicles []VehicleTotals `json:"vehicles"`
}

type historyEntry struct {
	at  time.Time
	val *FleetHistory
}

type historyCache struct {
	mu sync.Mutex
	m  map[string]historyEntry
}

func (c *historyCache) get(k string) *FleetHistory {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || time.Since(e.at) > historyTTL {
		return nil
	}
	return e.val
}

func (c *historyCache) put(k string, v *FleetHistory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]historyEntry{}
	}
	c.m[k] = historyEntry{at: time.Now(), val: v}
}

var histCache = &historyCache{}

// FleetDaily builds the fleet's daily history for the last `days` days.
// (Named apart from Service.History, which is one vehicle's position track.)
func (s *Service) FleetDaily(ctx context.Context, tenantID int64, days int) (*FleetHistory, error) {
	if days < 1 {
		days = 7
	}
	if days > maxHistoryDays {
		days = maxHistoryDays
	}

	// Day boundaries in fleet-local time, matching how the rollup bucketed
	// them. Asking in UTC would request a day the upstream never wrote.
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc)
	from := today.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	to := today.Format("2006-01-02")

	key := fmt.Sprintf("%d|%s|%s", tenantID, from, to)
	if hit := histCache.get(key); hit != nil {
		return hit, nil
	}

	// The registry, via the existing live path — it already resolves tenancy
	// through RLS and carries the tracker IMEI, fleet group and type.
	vehicles, err := s.List(ctx, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	byIMEI := make(map[string]*VehicleLive, len(vehicles))
	for i := range vehicles {
		if imei := vehicles[i].Tracker.IMEI; imei != "" {
			byIMEI[imei] = &vehicles[i]
		}
	}

	stats, err := s.tel.DailyStats(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("daily stats: %w", err)
	}

	dayAgg := map[string]*DayTotals{}
	// Idle share is accumulated separately because it must be weighted by
	// seconds, not averaged across vehicles — a truck that idled 10 minutes
	// and one that idled 10 hours are not two equal opinions.
	type engineOn struct{ idle, moving float64 }
	dayEngine := map[string]*engineOn{}
	vehAgg := map[int64]*VehicleTotals{}

	for _, st := range stats {
		v, ok := byIMEI[st.IMEI]
		if !ok {
			continue // a device this tenant cannot see; RLS already excluded it
		}
		moving, idle, parking := float64(st.MovingS)/3600, float64(st.IdleS)/3600, float64(st.ParkingS)/3600

		d := dayAgg[st.Day]
		if d == nil {
			d = &DayTotals{Day: st.Day}
			dayAgg[st.Day] = d
			dayEngine[st.Day] = &engineOn{}
		}
		d.MovingH += moving
		d.IdleH += idle
		d.ParkingH += parking
		d.DistanceKm += st.DistanceKm
		d.Active++
		if st.MovingS == 0 {
			d.Unused++
		} else {
			e := dayEngine[st.Day]
			e.idle += idle
			e.moving += moving
		}

		a := vehAgg[v.VehicleID]
		if a == nil {
			a = &VehicleTotals{
				VehicleID:    v.VehicleID,
				LicensePlate: v.LicensePlate,
				FleetGroup:   v.FleetGroupName,
				VehicleType:  v.VehicleType,
			}
			vehAgg[v.VehicleID] = a
		}
		a.MovingH += moving
		a.IdleH += idle
		a.DistanceKm += st.DistanceKm
		if st.MovingS > 0 {
			a.DaysUsed++
		}
		if st.MaxSpeed != nil && *st.MaxSpeed > a.MaxSpeed {
			a.MaxSpeed = *st.MaxSpeed
		}
	}

	out := &FleetHistory{From: from, To: to}
	for day, d := range dayAgg {
		if e := dayEngine[day]; e != nil && e.idle+e.moving > 0 {
			d.IdlePct = round1(100 * e.idle / (e.idle + e.moving))
		}
		d.MovingH, d.IdleH, d.ParkingH = round1(d.MovingH), round1(d.IdleH), round1(d.ParkingH)
		d.DistanceKm = round1(d.DistanceKm)
		out.Days = append(out.Days, *d)
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Day < out.Days[j].Day })

	for _, a := range vehAgg {
		if a.IdleH+a.MovingH > 0 {
			a.IdlePct = round1(100 * a.IdleH / (a.IdleH + a.MovingH))
		}
		a.MovingH, a.IdleH, a.DistanceKm = round1(a.MovingH), round1(a.IdleH), round1(a.DistanceKm)
		out.Vehicles = append(out.Vehicles, *a)
	}
	sort.Slice(out.Vehicles, func(i, j int) bool { return out.Vehicles[i].IdleH > out.Vehicles[j].IdleH })

	histCache.put(key, out)
	return out, nil
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// fleetHistory serves the fleet's daily history.
//
// @Summary     Fleet history (daily)
// @Description Per-day moving/idle/parked hours, distance and idle share, plus per-vehicle totals over the window. Days are Asia/Jakarta.
// @Tags        analytics
// @Produce     json
// @Param       days query int false "window length, 1-8 (default 7)"
// @Success     200 {object} FleetHistory
// @Failure     502 {object} map[string]string
// @Security    BearerAuth
// @Router      /v1/analytics/history [get]
func (h *Handler) fleetHistory(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	out, err := h.svc.FleetDaily(r.Context(), auth.EffectiveTenant(r), days)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "fleet history unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// VehicleDayPoint is one vehicle on one day, un-aggregated.
type VehicleDayPoint struct {
	Day        string  `json:"day"`
	MovingH    float64 `json:"moving_h"`
	IdleH      float64 `json:"idle_h"`
	DistanceKm float64 `json:"distance_km"`
	// MaxSpeed is the day's peak, carried for driver scoring: a speeding penalty
	// needs the peak per day, not the window's overall maximum.
	MaxSpeed float64 `json:"max_speed"`
}

// VehicleSeries is a vehicle's daily history, oldest first.
type VehicleSeries struct {
	VehicleID    int64
	LicensePlate string
	FleetGroup   *string
	VehicleType  *string
	Days         []VehicleDayPoint
}

// FleetDailySeries returns per-vehicle DAILY points rather than window totals.
//
// FleetDaily sums each vehicle's days into one row, which is all a report needs.
// Anything asking whether a change is SIGNIFICANT cannot work from a sum: a
// truck averaging 79 km/day over six days might have driven 79 every day or
// have alternated 21 and 461, and those two fleets need completely different
// responses. The spread is only visible per day, so it is kept here.
//
// Shares FleetDaily's upstream call and cache, so asking for both costs one
// fetch.
func (s *Service) FleetDailySeries(ctx context.Context, tenantID int64, days int) ([]VehicleSeries, error) {
	if days < 1 {
		days = 7
	}
	if days > maxHistoryDays {
		days = maxHistoryDays
	}
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc)
	from := today.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	to := today.Format("2006-01-02")

	vehicles, err := s.List(ctx, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	byIMEI := make(map[string]*VehicleLive, len(vehicles))
	for i := range vehicles {
		if imei := vehicles[i].Tracker.IMEI; imei != "" {
			byIMEI[imei] = &vehicles[i]
		}
	}

	stats, err := s.tel.DailyStats(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("daily stats: %w", err)
	}

	agg := map[int64]*VehicleSeries{}
	for _, st := range stats {
		v := byIMEI[st.IMEI]
		if v == nil {
			continue // a device this tenant cannot see
		}
		vs := agg[v.VehicleID]
		if vs == nil {
			vs = &VehicleSeries{
				VehicleID:    v.VehicleID,
				LicensePlate: v.LicensePlate,
				FleetGroup:   v.FleetGroupName,
				VehicleType:  v.VehicleType,
			}
			agg[v.VehicleID] = vs
		}
		maxSpeed := 0.0
		if st.MaxSpeed != nil {
			maxSpeed = *st.MaxSpeed
		}
		vs.Days = append(vs.Days, VehicleDayPoint{
			Day:        st.Day,
			MovingH:    float64(st.MovingS) / 3600,
			IdleH:      float64(st.IdleS) / 3600,
			DistanceKm: st.DistanceKm,
			MaxSpeed:   maxSpeed,
		})
	}

	out := make([]VehicleSeries, 0, len(agg))
	for _, vs := range agg {
		sort.Slice(vs.Days, func(i, j int) bool { return vs.Days[i].Day < vs.Days[j].Day })
		out = append(out, *vs)
	}
	return out, nil
}
