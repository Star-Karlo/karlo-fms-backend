package liveview

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// VehicleState is the accumulated (not window-scoped) state of a vehicle:
// lifetime engine hours, latest odometer, and service context.
type VehicleState struct {
	EngineHours    float64  `json:"engine_hours"` // lifetime ignition-on hours
	OdometerKm     *float64 `json:"odometer_km"`
	LastServiceKm  *float64 `json:"last_service_km"`
	LastServiceAt  *string  `json:"last_service_at"`
	KmSinceService *float64 `json:"km_since_service"`
}

// VehicleAnalytics bundles a vehicle's history-derived calculations for one
// window into a single response, so an integrator gets everything in one call
// instead of stitching /fuel-trip + /behavior + /driving-events + state.
// Each section mirrors its dedicated endpoint and is null when it can't be
// computed (e.g. no fuel sensor), never failing the whole request.
type VehicleAnalytics struct {
	VehicleID int64             `json:"vehicle_id"`
	From      time.Time         `json:"from"`
	To        time.Time         `json:"to"`
	Fuel      *FuelTrip         `json:"fuel"`     // distance, litres, cost, km/L, Rp/km, Rp/hour, refuels, engine/idle/moving hours
	Behavior  *BehaviorResult   `json:"behavior"` // overspeed, harsh accel/brake, idle stops (GPS)
	Driving   *DrivingHistory   `json:"driving"`  // harsh events by axis (accelerometer)
	State     *VehicleState     `json:"state"`    // lifetime engine hours, odometer, service context
	Errors    map[string]string `json:"errors,omitempty"`
}

// VehicleState reads the accumulated telemetry + service context for a vehicle.
func (s *Service) VehicleState(ctx context.Context, tenantID, vehicleID int64) (*VehicleState, error) {
	var st VehicleState
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var sec int64
		err := tx.QueryRow(ctx,
			`SELECT engine_hours_seconds, last_odometer_km::float8
			 FROM vehicle_telemetry_state WHERE vehicle_id = $1`, vehicleID).
			Scan(&sec, &st.OdometerKm)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil {
			st.EngineHours = math.Round(float64(sec)/3600*10) / 10
		}
		var svcKm *float64
		var svcAt *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT last_odometer::float8, last_done_at
			 FROM service_schedules
			 WHERE vehicle_id = $1 AND last_done_at IS NOT NULL
			 ORDER BY last_done_at DESC NULLS LAST LIMIT 1`, vehicleID).
			Scan(&svcKm, &svcAt); err != nil && err != pgx.ErrNoRows {
			return err
		}
		if svcAt != nil {
			d := svcAt.Format("2006-01-02")
			st.LastServiceAt = &d
		}
		st.LastServiceKm = svcKm
		if svcKm != nil && st.OdometerKm != nil && *st.OdometerKm > *svcKm {
			diff := math.Round((*st.OdometerKm-*svcKm)*10) / 10
			st.KmSinceService = &diff
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Analytics assembles the consolidated per-vehicle analytics for one window.
// Each section is best-effort: a section that errors is left nil with a note in
// Errors, so one missing input never fails the whole response. Shared by the
// JWT endpoint and the IMEI-keyed integration endpoint.
func (s *Service) Analytics(ctx context.Context, tenantID, vehicleID int64, from, to time.Time, limit, idleThreshold float64) VehicleAnalytics {
	out := VehicleAnalytics{VehicleID: vehicleID, From: from, To: to}
	errs := map[string]string{}
	if fuel, e := s.FuelTrip(ctx, tenantID, vehicleID, from, to); e != nil {
		errs["fuel"] = e.Error()
	} else {
		out.Fuel = fuel
	}
	if beh, e := s.Behavior(ctx, tenantID, vehicleID, from, to, limit, idleThreshold); e != nil {
		errs["behavior"] = e.Error()
	} else {
		out.Behavior = beh
	}
	if drv, e := s.DrivingHistory(ctx, tenantID, vehicleID, from, to); e != nil {
		errs["driving"] = e.Error()
	} else {
		out.Driving = drv
	}
	if st, e := s.VehicleState(ctx, tenantID, vehicleID); e != nil {
		errs["state"] = e.Error()
	} else {
		out.State = st
	}
	if len(errs) > 0 {
		out.Errors = errs
	}
	return out
}

// VehicleIDByIMEI resolves a device IMEI to this tenant's vehicle id. RLS scopes
// the lookup to the caller's tenant, so the same IMEI registered in another
// account is invisible here. Returns ok=false when no vehicle carries the IMEI.
func (s *Service) VehicleIDByIMEI(ctx context.Context, tenantID int64, imei string) (int64, bool, error) {
	var id int64
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT v.id FROM vehicles v JOIN trackers t ON t.id = v.tracker_id WHERE t.imei = $1`, imei).Scan(&id)
	})
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
