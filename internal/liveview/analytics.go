package liveview

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
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

// analytics godoc
//
//	@Summary		Consolidated vehicle analytics (history + calculations)
//	@Description	Everything derived for one vehicle over [from,to] in a single call: fuel (litres/cost/km-L/Rp-km/Rp-hour/refuels/engine-idle-moving hours), GPS behaviour (overspeed, harsh accel/brake, idle stops), accelerometer harsh events by axis, and accumulated state (lifetime engine hours, odometer, km since service). Any section that can't be computed is null (with a note in `errors`), never failing the whole response.
//	@Tags			analytics
//	@Security		BearerAuth
//	@Produce		json
//	@Param			vehicle_id		path	int		true	"vehicle id"
//	@Param			from			query	string	true	"RFC3339"
//	@Param			to				query	string	true	"RFC3339"
//	@Param			limit			query	number	false	"overspeed limit km/h (default 60)"
//	@Param			idle_threshold	query	number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Success		200	{object}	liveview.VehicleAnalytics
//	@Router			/v1/vehicles/{vehicle_id}/analytics [get]
func (h *Handler) analytics(w http.ResponseWriter, r *http.Request) {
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
	limit := 60.0
	if v := q.Get("limit"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			limit = f
		}
	}
	idleThreshold := 120.0
	if v := q.Get("idle_threshold"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			idleThreshold = f
		}
	}

	ctx := r.Context()
	tenantID := auth.TenantID(ctx)
	out := VehicleAnalytics{VehicleID: id, From: from, To: to}
	errs := map[string]string{}

	if fuel, e := h.svc.FuelTrip(ctx, tenantID, id, from, to); e != nil {
		errs["fuel"] = e.Error()
	} else {
		out.Fuel = fuel
	}
	if beh, e := h.svc.Behavior(ctx, tenantID, id, from, to, limit, idleThreshold); e != nil {
		errs["behavior"] = e.Error()
	} else {
		out.Behavior = beh
	}
	if drv, e := h.svc.DrivingHistory(ctx, tenantID, id, from, to); e != nil {
		errs["driving"] = e.Error()
	} else {
		out.Driving = drv
	}
	if st, e := h.svc.VehicleState(ctx, tenantID, id); e != nil {
		errs["state"] = e.Error()
	} else {
		out.State = st
	}
	if len(errs) > 0 {
		out.Errors = errs
	}
	httpx.JSON(w, http.StatusOK, out)
}
