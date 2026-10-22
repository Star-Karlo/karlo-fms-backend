package liveview

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Asking for history by the number on the truck.
//
// Every history endpoint is addressed by this system's vehicle id, which is
// the right key inside FMS and the wrong one at its edge: an integration has a
// nomor polisi — it is on the delivery note, in the ERP, on the WhatsApp
// message from the yard — and no way to turn it into an id without first
// fetching and searching the vehicle list. That is a second request, a
// pagination loop and a matching rule in every client, re-implemented slightly
// differently each time.
//
// PLATES ARE MATCHED LOOSELY, on purpose. "B 9012 UBC", "B9012UBC" and
// "b-9012-ubc" are the same truck, and which spelling a caller has depends on
// who typed it. Separators and case are stripped from both sides before
// comparing, exactly as the registry's own search does.
//
// The fleet summary endpoint is the other half: the figures on the history
// panel — distance, max speed, harsh events, idle — for a list of trucks in
// one request, with the caller choosing the fields. Pulling the full track for
// forty vehicles to add up four numbers is the thing it exists to prevent.

// plateNoise is everything ignored when comparing plates.
var plateNoise = regexp.MustCompile(`[^A-Za-z0-9]`)

// NormalisePlate strips separators and case for comparison.
func NormalisePlate(s string) string {
	return strings.ToUpper(plateNoise.ReplaceAllString(s, ""))
}

// VehicleIDByPlate resolves a nomor polisi to this tenant's vehicle id.
//
// An archived vehicle still resolves: history questions are usually asked
// about trucks that have left the fleet, and refusing would make the endpoint
// useless for exactly the case it is needed for. An ambiguous plate (two rows
// normalising to the same thing, which the registry permits) resolves to the
// active one, then the lowest id, rather than failing — the caller asked about
// a truck, not about a data-quality problem.
func (s *Service) VehicleIDByPlate(ctx context.Context, tenantID int64, plate string) (int64, string, bool, error) {
	norm := NormalisePlate(plate)
	if norm == "" {
		return 0, "", false, nil
	}
	var id int64
	var found string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, license_plate FROM vehicles
			 WHERE upper(regexp_replace(license_plate, '[^A-Za-z0-9]', '', 'g')) = $1
			 ORDER BY (status = 'active') DESC, id
			 LIMIT 1`, norm).Scan(&id, &found)
	})
	if err == pgx.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return id, found, true, nil
}

// resolvePlate answers 404 itself when the plate is unknown.
func (h *Handler) resolvePlate(w http.ResponseWriter, r *http.Request) (int64, bool) {
	plate := chi.URLParam(r, "plate")
	id, _, ok, err := h.svc.VehicleIDByPlate(r.Context(), auth.EffectiveTenant(r), plate)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no vehicle with plate "+plate+" in this account")
		return 0, false
	}
	return id, true
}

// historyByPlate godoc
//
//	@Summary		Vehicle historical track, addressed by nomor polisi
//	@Description	Identical to /v1/vehicles/{vehicle_id}/history, with the plate in place of the id. Spacing, dashes and case are ignored, so "B 9012 UBC", "B9012UBC" and "b-9012-ubc" all resolve to the same truck.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			plate		path		string	true	"nomor polisi, e.g. B 9012 UBC"
//	@Param			from		query		string	true	"RFC3339"
//	@Param			to			query		string	true	"RFC3339"
//	@Param			max_points	query		int		false	"cap on returned points"
//	@Param			limit		query		number	false	"overspeed limit km/h for the over_limit flag (default 60)"
//	@Param			source		query		string	false	"tracker | camera | both"
//	@Success		200	{object}	httpx.List[liveview.TripPoint]
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/vehicles/by-plate/{plate}/history [get]
func (h *Handler) historyByPlate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolvePlate(w, r)
	if !ok {
		return
	}
	h.historyFor(w, r, id)
}

// liveByPlate godoc
//
//	@Summary	Live position and status, addressed by nomor polisi
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		plate	path		string	true	"nomor polisi"
//	@Success	200		{object}	liveview.VehicleLive
//	@Failure	404		{object}	map[string]string
//	@Router		/v1/vehicles/by-plate/{plate}/live [get]
func (h *Handler) liveByPlate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolvePlate(w, r)
	if !ok {
		return
	}
	v, err := h.svc.One(r.Context(), auth.EffectiveTenant(r), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "vehicle not found")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

// behaviorByPlate godoc
//
//	@Summary		Driving behaviour for a window, addressed by nomor polisi
//	@Description	Identical to /v1/vehicles/{vehicle_id}/behavior, with the plate in place of the id: overspeed events, harsh acceleration and braking, idle stops and distance, derived from the GPS track. Spacing, dashes and case in the plate are ignored.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			plate			path		string	true	"nomor polisi, e.g. B 9012 UBC"
//	@Param			from			query		string	true	"RFC3339"
//	@Param			to				query		string	true	"RFC3339"
//	@Param			limit			query		number	false	"overspeed limit km/h (default 60)"
//	@Param			idle_threshold	query		number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Success		200	{object}	liveview.BehaviorResult
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/vehicles/by-plate/{plate}/behavior [get]
func (h *Handler) behaviorByPlate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolvePlate(w, r)
	if !ok {
		return
	}
	h.behaviorFor(w, r, id)
}

// fuelTripByPlate godoc
//
//	@Summary		Fuel and operating cost for a window, addressed by nomor polisi
//	@Description	Identical to /v1/vehicles/{vehicle_id}/fuel-trip, with the plate in place of the id: litres used, refuels, suspected thefts, cost and engine/idle/moving hours. Measured from the tank sensor where a vehicle has one, estimated from its configured km/L where it does not — the response says which.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			plate	path		string	true	"nomor polisi"
//	@Param			from	query		string	true	"RFC3339"
//	@Param			to		query		string	true	"RFC3339"
//	@Success		200	{object}	liveview.FuelTrip
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/vehicles/by-plate/{plate}/fuel-trip [get]
func (h *Handler) fuelTripByPlate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolvePlate(w, r)
	if !ok {
		return
	}
	h.fuelTripFor(w, r, id)
}

// drivingEventsByPlate godoc
//
//	@Summary		Accelerometer harsh-driving events, addressed by nomor polisi
//	@Description	Identical to /v1/vehicles/{vehicle_id}/driving-events, with the plate in place of the id. These come from the device's accelerometer, so only Karlo-fitted trackers report them: an empty list means NOT MEASURED on this vehicle, not clean driving.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			plate	path		string	true	"nomor polisi"
//	@Param			from	query		string	true	"RFC3339"
//	@Param			to		query		string	true	"RFC3339"
//	@Success		200	{object}	liveview.DrivingHistory
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/vehicles/by-plate/{plate}/driving-events [get]
func (h *Handler) drivingEventsByPlate(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolvePlate(w, r)
	if !ok {
		return
	}
	h.drivingEventsFor(w, r, id)
}

// ---------------------------------------------------------------------------
// Fleet summary
// ---------------------------------------------------------------------------

// SummaryRequest asks for the history panel's figures for several vehicles.
type SummaryRequest struct {
	// Plates and VehicleIDs both select vehicles; give either or both. Empty
	// means every vehicle in the account that has a tracker — convenient, and
	// slow enough on a large fleet that it is not the default anyone reaches
	// for by accident.
	Plates     []string `json:"plates" example:"B 9012 UBC,B 9013 UBD"`
	VehicleIDs []int64  `json:"vehicle_ids"`
	// From and To are RFC3339. A window longer than 31 days is refused: the
	// figures are computed from the raw track, and a year of forty trucks is
	// not a request, it is an outage.
	From string `json:"from" example:"2026-10-21T00:00:00+07:00"`
	To   string `json:"to" example:"2026-10-22T00:00:00+07:00"`
	// Fields chooses what to compute and return. Empty means the default set
	// (everything in the history panel). Unknown names are rejected rather
	// than ignored, because a typo that silently drops a column is found in
	// production by the person reading the spreadsheet.
	Fields []string `json:"fields" example:"distance_km,max_speed"`
	// OverspeedLimit (km/h) decides what counts as overspeed; default 60.
	OverspeedLimit float64 `json:"overspeed_limit" example:"60"`
	// IdleThresholdMin is how long a stop must last to count as idle; default 5.
	IdleThresholdMin float64 `json:"idle_threshold_min" example:"5"`
}

// SummaryRow is one vehicle's figures. Only the requested fields are present.
type SummaryRow struct {
	VehicleID    int64  `json:"vehicle_id"`
	LicensePlate string `json:"license_plate"`
	// Error is set when this vehicle's figures could not be computed — the
	// rest of the fleet still comes back, with this row saying why it did not.
	Error  string             `json:"error,omitempty"`
	Fields map[string]float64 `json:"fields"`
}

// SummaryResponse is the fleet summary.
type SummaryResponse struct {
	From   time.Time    `json:"from"`
	To     time.Time    `json:"to"`
	Fields []string     `json:"fields"`
	Items  []SummaryRow `json:"items"`
	// NotFound lists plates that matched no vehicle, so a caller can tell a
	// typo from a quiet truck.
	NotFound []string `json:"not_found,omitempty"`
}

// summaryFields is the catalogue: name -> how to read it off the computed
// behaviour. Adding a figure to the API is adding a line here.
var summaryFields = map[string]func(b *BehaviorSummary) float64{
	"distance_km":      func(b *BehaviorSummary) float64 { return b.DistanceKm },
	"max_speed":        func(b *BehaviorSummary) float64 { return b.MaxSpeed },
	"avg_moving_speed": func(b *BehaviorSummary) float64 { return b.AvgMovingSpeed },
	"overspeed_count":  func(b *BehaviorSummary) float64 { return float64(b.OverspeedCount) },
	"harsh_accel":      func(b *BehaviorSummary) float64 { return float64(b.HarshAccel) },
	"harsh_brake":      func(b *BehaviorSummary) float64 { return float64(b.HarshBrake) },
	"idle_count":       func(b *BehaviorSummary) float64 { return float64(b.IdleCount) },
	"total_idle_min":   func(b *BehaviorSummary) float64 { return b.TotalIdleMin },
	"longest_stop_min": func(b *BehaviorSummary) float64 { return b.LongestStopMin },
	"points":           func(b *BehaviorSummary) float64 { return float64(b.Points) },
}

// defaultFields is the history panel, in the order it is read on screen.
var defaultFields = []string{
	"distance_km", "max_speed", "avg_moving_speed", "overspeed_count",
	"harsh_accel", "harsh_brake", "idle_count", "total_idle_min",
}

// maxSummaryVehicles bounds one request. Forty is a depot; four hundred is a
// report, and a report belongs in the report module where it runs in the
// background and arrives as a file.
const maxSummaryVehicles = 50

// summary godoc
//
//	@Summary		Historical summary for several vehicles at once
//	@Description	The figures from the history panel — distance, max speed, harsh events, idle — for a list of trucks named by plate or id, in ONE request. No track points come back; `fields` chooses which figures to compute, defaulting to the panel's own set. Up to 50 vehicles and 31 days per call. A vehicle whose telemetry cannot be read comes back as a row carrying `error`, so one dead tracker does not fail the batch.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		liveview.SummaryRequest	true	"vehicles, window and fields"
//	@Success		200		{object}	liveview.SummaryResponse
//	@Failure		400		{object}	map[string]string
//	@Router			/v1/vehicles/history-summary [post]
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	var req SummaryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	from, err1 := time.Parse(time.RFC3339, req.From)
	to, err2 := time.Parse(time.RFC3339, req.To)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}
	if !to.After(from) {
		httpx.Error(w, http.StatusBadRequest, "to must be after from")
		return
	}
	if to.Sub(from) > 31*24*time.Hour {
		httpx.Error(w, http.StatusBadRequest, "the window must be 31 days or less")
		return
	}
	fields := req.Fields
	if len(fields) == 0 {
		fields = defaultFields
	}
	for _, f := range fields {
		if _, ok := summaryFields[f]; !ok {
			httpx.Error(w, http.StatusBadRequest, "unknown field "+strconv.Quote(f)+" — valid: "+strings.Join(fieldNames(), ", "))
			return
		}
	}
	limit := req.OverspeedLimit
	if limit <= 0 {
		limit = 60
	}
	idle := req.IdleThresholdMin
	if idle <= 0 {
		idle = 5
	}

	tenantID := auth.EffectiveTenant(r)
	targets, missing, err := h.svc.resolveTargets(r.Context(), tenantID, req.Plates, req.VehicleIDs)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(targets) > maxSummaryVehicles {
		httpx.Error(w, http.StatusBadRequest, "at most "+strconv.Itoa(maxSummaryVehicles)+" vehicles per request; this asked for "+strconv.Itoa(len(targets)))
		return
	}

	out := SummaryResponse{From: from, To: to, Fields: fields, Items: []SummaryRow{}, NotFound: missing}
	for _, t := range targets {
		row := SummaryRow{VehicleID: t.id, LicensePlate: t.plate, Fields: map[string]float64{}}
		res, err := h.svc.Behavior(r.Context(), tenantID, t.id, from, to, limit, idle)
		if err != nil {
			row.Error = err.Error()
			out.Items = append(out.Items, row)
			continue
		}
		for _, f := range fields {
			row.Fields[f] = summaryFields[f](&res.Summary)
		}
		out.Items = append(out.Items, row)
	}
	httpx.JSON(w, http.StatusOK, out)
}

type target struct {
	id    int64
	plate string
}

// resolveTargets turns plates and ids into vehicles, keeping the caller's
// order and reporting plates that matched nothing.
func (s *Service) resolveTargets(ctx context.Context, tenantID int64, plates []string, ids []int64) ([]target, []string, error) {
	var out []target
	var missing []string
	seen := map[int64]bool{}

	if len(plates) == 0 && len(ids) == 0 {
		err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT v.id, v.license_plate FROM vehicles v
				 WHERE v.status <> 'archive' AND v.tracker_id IS NOT NULL
				 ORDER BY v.license_plate`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var t target
				if err := rows.Scan(&t.id, &t.plate); err != nil {
					return err
				}
				out = append(out, t)
			}
			return rows.Err()
		})
		return out, nil, err
	}

	for _, p := range plates {
		id, found, ok, err := s.VehicleIDByPlate(ctx, tenantID, p)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			missing = append(missing, p)
			continue
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, target{id: id, plate: found})
		}
	}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		var plate string
		err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT license_plate FROM vehicles WHERE id = $1`, id).Scan(&plate)
		})
		if err == pgx.ErrNoRows {
			missing = append(missing, strconv.FormatInt(id, 10))
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		seen[id] = true
		out = append(out, target{id: id, plate: plate})
	}
	return out, missing, nil
}

func fieldNames() []string {
	out := make([]string, 0, len(summaryFields))
	for k := range summaryFields {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
