package insight

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Battery watch, and the one place a finding turns directly into scheduled work.
//
// A low battery is unusual among fleet alerts in that the remedy is known,
// cheap, and entirely schedulable: replace the battery before it strands a
// truck. So rather than stopping at "battery low", this exposes an action that
// books it into the existing maintenance module — one POST creates a service
// schedule for the battery component, and from then on it lives in the same
// list as every other due service.
//
// THRESHOLDS, set from the fleet's own readings rather than assumed. Across
// ~1,400 reporting vehicles the median is 25.7V and the lower quartile 25.3V,
// confirming a 24V commercial system (two 12V in series):
//
//	>= 24.0   healthy                          1362 vehicles
//	23.0-24.0 weak, worth booking                 8
//	20.0-23.0 deeply discharged, replace now      13
//	 6.0-20.0 NOT A BATTERY FINDING               34
//	 < 6.0    no reading at all                   10
//
// The band below 20V is the one worth explaining. A 24V starting battery at
// 15V cannot crank an engine — yet these vehicles are transmitting live
// telemetry, so the engine plainly runs. The reading therefore is not measuring
// the starting battery: it is a sensor or wiring fault. Reporting it as "replace
// the battery" would send a mechanic to do the wrong job and leave the actual
// fault in place, so it is reported as what it is.
//
// Do not confuse this with the device's own `Battery Voltage` field, which
// reads ~4100 — millivolts of the tracker's internal backup cell, nothing to do
// with the vehicle.
const (
	batteryHealthy     = 24.0
	batteryCritical    = 23.0
	batteryImplausible = 20.0
	batteryFloor       = 6.0
)

// BatteryFinding is one vehicle whose battery needs attention.
type BatteryFinding struct {
	VehicleID    int64    `json:"vehicle_id"`
	LicensePlate string   `json:"license_plate"`
	FleetGroup   string   `json:"fleet_group,omitempty"`
	BatteryV     float64  `json:"battery_v"`
	Severity     Severity `json:"severity"`
	// Kind separates a real battery problem from an implausible reading:
	// "battery" | "sensor_fault". They need different people and different
	// parts, so collapsing them into one list wastes a trip.
	Kind           string `json:"kind"`
	Recommendation string `json:"recommendation"`
	Detail         string `json:"detail,omitempty"`
	// Scheduled reports whether a battery service is ALREADY booked for this
	// vehicle, so the client shows "scheduled" rather than offering the action
	// again — and so a second click cannot create a duplicate.
	Scheduled bool `json:"scheduled"`
}

// lowBattery finds vehicles below the healthy threshold, newest state first.
func (h *Handler) lowBattery(ctx context.Context, tenantID int64) ([]BatteryFinding, error) {
	vehicles, err := h.live.List(ctx, tenantID, nil)
	if err != nil {
		return nil, err
	}

	out := []BatteryFinding{}
	ids := []int64{}
	for _, v := range vehicles {
		if v.BatteryV == nil || *v.BatteryV < batteryFloor || *v.BatteryV >= batteryHealthy {
			continue
		}
		b := BatteryFinding{
			VehicleID: v.VehicleID, LicensePlate: v.LicensePlate, BatteryV: round1(*v.BatteryV),
		}
		switch {
		case *v.BatteryV < batteryImplausible:
			// See the threshold note: the engine is demonstrably running, so
			// this is not what the starting battery is doing.
			b.Severity = SevWarning
			b.Kind = "sensor_fault"
			b.Recommendation = "check the voltage sensor or wiring"
			b.Detail = "too low for a running 24V system — the reading, not the battery, is the likely fault"
		case *v.BatteryV < batteryCritical:
			b.Severity = SevCritical
			b.Kind = "battery"
			b.Recommendation = "replace now"
			b.Detail = "deeply discharged; likely to fail to crank"
		default:
			b.Severity = SevWarning
			b.Kind = "battery"
			b.Recommendation = "book a replacement"
			b.Detail = "weak but serviceable"
		}
		out = append(out, b)
		ids = append(ids, v.VehicleID)
	}
	if len(out) == 0 {
		return out, nil
	}

	// Mark the ones already booked, so the UI does not offer to schedule twice.
	booked := map[int64]bool{}
	err = db.Tx(ctx, h.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT vehicle_id FROM service_schedules
WHERE vehicle_id = ANY($1) AND lower(component) LIKE '%batter%'
  AND coalesce(status,'') <> 'done'`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			booked[id] = true
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Scheduled = booked[out[i].VehicleID]
	}

	sort.Slice(out, func(i, j int) bool { return out[i].BatteryV < out[j].BatteryV })
	return out, nil
}

// batteryWatch godoc
//
//	@Summary		Vehicles with a weak or failing battery, and whether a service is booked
//	@Description	Readings below 6V are ignored as absent sensors rather than flat batteries. Each finding carries a recommendation and a `scheduled` flag; POST to the schedule-service action to book a replacement into the maintenance module.
//	@Tags			insights
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		insight.BatteryFinding
//	@Failure		503	{object}	map[string]string
//	@Router			/v1/insights/battery-watch [get]
func (h *Handler) batteryWatch(w http.ResponseWriter, r *http.Request) {
	if !h.requireLive(w) {
		return
	}
	out, err := h.lowBattery(r.Context(), auth.TenantID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ScheduleServiceRequest optionally overrides when the work is due.
type ScheduleServiceRequest struct {
	// DueInDays defaults by severity when omitted.
	DueInDays *int    `json:"due_in_days"`
	Component *string `json:"component"`
	Note      *string `json:"note"`
}

// scheduleBatteryService godoc
//
//	@Summary		Book a battery replacement into the maintenance schedule
//	@Description	Turns a battery finding into a real service schedule, so it appears in Maintenance and in the action queue alongside every other due job. Idempotent: if an open battery service already exists for the vehicle it is returned rather than duplicated. A critical reading defaults to due today, a weak one to seven days out.
//	@Tags			insights
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			vehicle_id	path		int							true	"vehicle id"
//	@Param			body		body		insight.ScheduleServiceRequest	false	"optional overrides"
//	@Success		200			{object}	map[string]interface{}
//	@Failure		400			{object}	map[string]string
//	@Failure		404			{object}	map[string]string
//	@Router			/v1/insights/battery-watch/{vehicle_id}/schedule-service [post]
func (h *Handler) scheduleBatteryService(w http.ResponseWriter, r *http.Request) {
	if !h.requireLive(w) {
		return
	}
	vehicleID, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil || vehicleID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	var req ScheduleServiceRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	tenantID := auth.TenantID(r.Context())

	// Read the current reading so the due date reflects how bad it actually is,
	// rather than trusting the caller to say.
	findings, err := h.lowBattery(r.Context(), tenantID)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	var found *BatteryFinding
	for i := range findings {
		if findings[i].VehicleID == vehicleID {
			found = &findings[i]
			break
		}
	}
	if found == nil {
		httpx.Error(w, http.StatusBadRequest,
			"this vehicle's battery is not currently reporting low; nothing to schedule")
		return
	}
	if found.Kind == "sensor_fault" {
		// Refused rather than silently booked. A reading below 20V on a vehicle
		// that is transmitting means the measurement is wrong, not the battery —
		// so a battery job would consume a workshop slot, fit a part that was
		// never faulty, and leave the actual wiring problem in place.
		httpx.Error(w, http.StatusBadRequest,
			"this reading is too low to be a running 24V system, so the sensor or wiring is the likely fault rather than the battery — book an electrical check instead")
		return
	}

	dueDays := 7
	if found.Severity == SevCritical {
		dueDays = 0 // below cranking threshold: today, not next week
	}
	if req.DueInDays != nil && *req.DueInDays >= 0 {
		dueDays = *req.DueInDays
	}
	component := "Battery"
	if req.Component != nil && *req.Component != "" {
		component = *req.Component
	}
	due := time.Now().AddDate(0, 0, dueDays)

	var id int64
	var created bool
	err = db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		// Lock the vehicle row so two clicks cannot both find "no existing
		// schedule" and both insert. RLS scopes the lookup, so a foreign
		// vehicle id simply finds nothing.
		var exists bool
		if e := tx.QueryRow(r.Context(),
			`SELECT true FROM vehicles WHERE id = $1 FOR UPDATE`, vehicleID).Scan(&exists); e != nil {
			return fmt.Errorf("vehicle %d not found", vehicleID)
		}
		e := tx.QueryRow(r.Context(), `
SELECT id FROM service_schedules
WHERE vehicle_id = $1 AND lower(component) LIKE '%batter%' AND coalesce(status,'') <> 'done'
LIMIT 1`, vehicleID).Scan(&id)
		if e == nil {
			return nil // already booked; idempotent
		}
		if e != pgx.ErrNoRows {
			return e
		}
		created = true
		return tx.QueryRow(r.Context(), `
INSERT INTO service_schedules (tenant_id, vehicle_id, component, scheduler, day_interval, next_due_at, status)
VALUES ($1, $2, $3, 'date', NULL, $4, 'scheduled')
RETURNING id`, tenantID, vehicleID, component, due).Scan(&id)
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"schedule_id":  id,
		"created":      created,
		"vehicle_id":   vehicleID,
		"component":    component,
		"next_due_at":  due.Format("2006-01-02"),
		"battery_v":    found.BatteryV,
		"severity":     found.Severity,
		"already_open": !created,
	})
}
