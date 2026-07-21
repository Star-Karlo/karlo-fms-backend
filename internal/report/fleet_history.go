package report

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/karlo/fms-backend/internal/liveview"
)

// Fleet-wide historical reports, from the daily telemetry rollup.
//
// WHY THESE EXIST: until now, twelve of the seventeen reports covered ONE
// vehicle, because each fetches that vehicle's telemetry over HTTP. Of the five
// fleet-wide ones, three are registry lists that are empty in practice, which
// left alerts as the only fleet-wide source with data in it. So "a report on
// how the fleet performed last week" had nothing to be built from, and both a
// person and the AI composer would end up reaching for alerts whatever they
// actually asked for.
//
// The vehicle_day rollup already computes what was missing — time moving, time
// idling, distance, per vehicle per day — so these are cheap: one call to the
// tracking service for pre-aggregated rows, then a join to the vehicle registry
// for plates and types. No per-vehicle fan-out.

// fleetHistoryDays is the window these can cover.
//
// The rollup keeps roughly a week of hot data; asking for more silently returns
// less, so the limit is applied here and stated in the report rather than
// leaving a month-long request looking like a quiet week.
const fleetHistoryDays = 8

// WithFleetHistory supplies the joined daily history. Optional: without it
// these reports return an explanatory note instead of failing, so a deployment
// where the rollup is not yet running still serves every other report.
func (s *Service) WithFleetHistory(lv *liveview.Service) *Service {
	s.fleet = lv
	return s
}

// fleetDaily fetches the window, bounded.
func (s *Service) fleetDaily(ctx context.Context, tenantID int64, from, to time.Time) (*liveview.FleetHistory, int, error) {
	if s.fleet == nil {
		return nil, 0, fmt.Errorf("fleet history is not available on this deployment")
	}
	days := int(to.Sub(from).Hours()/24) + 1
	requested := days
	if days > fleetHistoryDays {
		days = fleetHistoryDays
	}
	if days < 1 {
		days = 1
	}
	h, err := s.fleet.FleetDaily(ctx, tenantID, days)
	return h, requested, err
}

func truncNote(requested int) string {
	if requested <= fleetHistoryDays {
		return ""
	}
	return fmt.Sprintf("Only the last %d days are available; the rollup keeps about a week of history.", fleetHistoryDays)
}

// runFleetHistory handles every fleet-wide historical key.
func (s *Service) runFleetHistory(ctx context.Context, tenantID int64, key string, from, to time.Time, res *Result) error {
	h, requested, err := s.fleetDaily(ctx, tenantID, from, to)
	if err != nil {
		return err
	}
	res.Note = truncNote(requested)

	switch key {
	case "fleet-daily-summary":
		for _, d := range h.Days {
			res.Rows = append(res.Rows, map[string]any{
				"day":         d.Day,
				"active":      d.Active,
				"unused":      d.Unused,
				"moving_h":    round1(d.MovingH),
				"idle_h":      round1(d.IdleH),
				"distance_km": round1(d.DistanceKm),
				"idle_pct":    round1(d.IdlePct),
			})
		}
		if res.Note == "" {
			res.Note = `"Idle %" is idle time as a share of engine-on time, counting only vehicles that moved that day — a stuck ignition sensor would otherwise dominate it.`
		}

	case "vehicle-utilisation":
		rows := append([]liveview.VehicleTotals(nil), h.Vehicles...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].DistanceKm > rows[j].DistanceKm })
		for _, v := range rows {
			res.Rows = append(res.Rows, vehicleRow(v))
		}

	case "idle-leaderboard":
		// Vehicles that actually WORKED. A truck reading 100% idle with no
		// distance is a wiring fault, and it would otherwise fill the top of
		// the one list here that is meant to be acted on.
		var rows []liveview.VehicleTotals
		for _, v := range h.Vehicles {
			if v.MovingH >= 0.5 {
				rows = append(rows, v)
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].IdleH > rows[j].IdleH })
		for _, v := range rows {
			res.Rows = append(res.Rows, vehicleRow(v))
		}
		if res.Note == "" {
			res.Note = "Vehicles that never moved are excluded — a reading of 100% idle with no distance is a sensor fault rather than a driver leaving an engine running. See Unused Vehicles for those."
		}

	case "unused-vehicles":
		var rows []liveview.VehicleTotals
		for _, v := range h.Vehicles {
			if v.DaysUsed == 0 {
				rows = append(rows, v)
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].IdleH > rows[j].IdleH })
		for _, v := range rows {
			res.Rows = append(res.Rows, vehicleRow(v))
		}
		if res.Note == "" {
			res.Note = "Vehicles that reported but never moved in the period. Either genuinely parked, or a tracker reporting from a truck that is working — both worth knowing."
		}

	default:
		return fmt.Errorf("unknown fleet history report: %s", key)
	}
	return nil
}

func vehicleRow(v liveview.VehicleTotals) map[string]any {
	return map[string]any{
		"license_plate": v.LicensePlate,
		"vehicle_type":  strOrDash(v.VehicleType),
		"fleet_group":   strOrDash(v.FleetGroup),
		"moving_h":      round1(v.MovingH),
		"idle_h":        round1(v.IdleH),
		"distance_km":   round1(v.DistanceKm),
		"idle_pct":      round1(v.IdlePct),
		"days_used":     v.DaysUsed,
		"max_speed":     round1(v.MaxSpeed),
	}
}

func strOrDash(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	return *s
}
