package report

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Column describes a generic report column (order preserved).
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Result is the generic report payload rendered by the frontend table + CSV.
type Result struct {
	Key     string           `json:"key"`
	Title   string           `json:"title"`
	Columns []Column         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Note    string           `json:"note,omitempty"` // e.g. missing-sensor explanation
}

// RunRequest is the generic report parameter set (a superset; each report uses
// what it needs).
type RunRequest struct {
	VehicleID *int64 `json:"vehicle_id"`
	// VehicleIDs runs a per-vehicle report for SEVERAL vehicles and stacks the
	// results into one table. Fleet operators think in terms of "these six
	// trucks", not one at a time, and re-running a report six times and pasting
	// the output together is the manual work this removes.
	VehicleIDs []int64 `json:"vehicle_ids,omitempty"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	AlertCode  string  `json:"alert_code"`
	Limit      float64 `json:"limit"` // overspeed limit km/h (default 60)
}

// catalogColumns is every report's column set, declared once.
//
// Lifted out of the Run() switch so the columns of a report can be known
// WITHOUT running it. That matters for two callers: the custom-report builder,
// which lets a user pick and rename columns before any data is fetched, and
// the AI composer, which cannot map a customer's spreadsheet headers onto
// fields it has never been told about. Running a report to discover its shape
// would mean a 5,000-point telemetry fetch just to populate a dropdown.
var catalogColumns = map[string][]Column{
	"fleet-daily-summary": {{"day", "Day"}, {"active", "Vehicles Reporting"}, {"unused", "Did Not Move"}, {"moving_h", "Moving (h)"}, {"idle_h", "Idle (h)"}, {"distance_km", "Distance (km)"}, {"idle_pct", "Idle % of Engine-On"}},
	"vehicle-utilisation": {{"license_plate", "Plate"}, {"vehicle_type", "Type"}, {"fleet_group", "Group"}, {"moving_h", "Moving (h)"}, {"idle_h", "Idle (h)"}, {"distance_km", "Distance (km)"}, {"days_used", "Days Used"}, {"idle_pct", "Idle %"}, {"max_speed", "Max km/h"}},
	"idle-leaderboard":    {{"license_plate", "Plate"}, {"vehicle_type", "Type"}, {"idle_h", "Idle (h)"}, {"moving_h", "Moving (h)"}, {"idle_pct", "Idle %"}, {"distance_km", "Distance (km)"}},
	"unused-vehicles":     {{"license_plate", "Plate"}, {"vehicle_type", "Type"}, {"fleet_group", "Group"}, {"idle_h", "Idle (h)"}},

	"door-open-duration":         {{"opened_at", "Opened"}, {"closed_at", "Closed"}, {"duration_min", "Duration (min)"}},
	"driver-management":          {{"employee_no", "Emp. No"}, {"full_name", "Name"}, {"phone", "Phone"}, {"license_no", "License"}, {"license_class", "Class"}, {"license_expiry", "Expiry"}, {"status", "Status"}},
	"driver-operational-summary": {{"full_name", "Driver"}, {"license_no", "License"}, {"status", "Status"}},
	"fuel-history":               {{"date", "Date"}, {"fuel_level", "Fuel %"}, {"event", "Event"}},
	"geofence-inout":             {{"geofence", "Geofence"}, {"event", "Event"}, {"at", "At"}, {"dwell_min", "Dwell (min)"}},
	"gps-connection-history":     {{"offline_from", "Offline From"}, {"offline_to", "Back Online"}, {"duration_min", "Gap (min)"}, {"last_location", "Last Known Location"}, {"lat", "Lat"}, {"lon", "Lon"}},
	"harsh-driving":              {{"date", "Date"}, {"license_plate", "Plate"}, {"event", "Event"}, {"speed", "Speed (km/h)"}, {"rate", "Δ km/h per s"}, {"location", "Location"}},
	"pto-active-duration":        {{"started_at", "Started"}, {"ended_at", "Ended"}, {"duration_min", "Duration (min)"}},
	"service-scheduling-detail":  {{"license_plate", "Plate"}, {"component", "Component"}, {"scheduler", "Scheduler"}, {"next_due_at", "Next Due"}, {"status", "Status"}, {"current_odometer", "Current km"}, {"next_odometer", "Due at km"}},
	"speeding-violations":        {{"date", "Date"}, {"license_plate", "Plate"}, {"location", "Location"}, {"speed", "Speed"}, {"limit", "Limit"}, {"over_by", "Over by"}},
	"temperature-history":        {{"date", "Date"}, {"temperature_c", "Temp °C"}},
	"vehicle-alerts-history":     {{"date", "Date"}, {"license_plate", "Plate"}, {"alert_name", "Alert"}, {"severity", "Severity"}, {"actual", "Actual"}, {"limit", "Limit"}, {"address", "Address"}, {"check_status", "Status"}},
	"vehicle-alerts-summary":     {{"alert_code", "Code"}, {"alert_name", "Alert"}, {"count", "Count"}, {"urgent", "Urgent"}, {"unchecked", "Unchecked"}},
	"vehicle-stop-history":       {{"start", "Start"}, {"end", "End"}, {"duration_min", "Duration (min)"}, {"location", "Location"}},
	"vehicle-trip-history":       {{"date", "Date"}, {"license_plate", "Plate"}, {"location", "Location"}, {"speed", "Speed (km/h)"}, {"acc", "ACC"}},
	"vehicle-trip-summary":       {{"trip", "Trip"}, {"start", "Start"}, {"end", "End"}, {"duration_min", "Duration (min)"}, {"distance_km", "Distance (km)"}, {"max_speed", "Max km/h"}, {"idle_engine_min", "Idle w/ Engine (min)"}},
	"vehicle-usage-summary":      {{"day", "Day"}, {"distance_km", "Distance (km)"}, {"moving_min", "Moving (min)"}, {"idle_min", "Idle (min)"}, {"utilization", "Utilization %"}},
}

// ColumnsFor returns a report's columns, or nil for an unknown key.
func ColumnsFor(key string) []Column { return catalogColumns[key] }

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func hav(aLat, aLon, bLat, bLon float64) float64 {
	const R = 6371.0
	dLat := (bLat - aLat) * math.Pi / 180
	dLon := (bLon - aLon) * math.Pi / 180
	x := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(aLat*math.Pi/180)*math.Cos(bLat*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(x), math.Sqrt(1-x))
}

// runMany stacks a per-vehicle report across several vehicles.
//
// Rows are tagged with the plate so a merged table is readable — several
// reports (trip summary, stop history, usage) have no vehicle column of their
// own because they were only ever run for one truck, and without it the
// combined output is rows from six vehicles with nothing saying which is which.
func (s *Service) runMany(ctx context.Context, tenantID int64, key string, req RunRequest) (*Result, error) {
	ids := req.VehicleIDs
	truncated := 0
	if len(ids) > maxVehiclesPerRun {
		truncated = len(ids) - maxVehiclesPerRun
		ids = ids[:maxVehiclesPerRun]
	}

	out := &Result{Key: key, Title: titleFor(key), Rows: []map[string]any{}}
	var failures []string
	// A vehicle that ran fine but had nothing to report simply contributes no
	// rows, so picking six trucks and seeing four in the output raises the
	// question "which two, and why". Naming them answers it.
	var empty []string

	for _, id := range ids {
		one := req
		one.VehicleIDs = nil
		vid := id
		one.VehicleID = &vid

		res, err := s.Run(ctx, tenantID, key, one)
		if err != nil {
			// One vehicle failing — no tracker, no telemetry — must not cost
			// the other five. Named at the end rather than swallowed.
			failures = append(failures, fmt.Sprintf("vehicle %d: %s", id, err.Error()))
			continue
		}

		plate := plateOf(res.Rows)
		if plate == "" {
			plate = s.plateFor(ctx, tenantID, id)
		}

		if len(out.Columns) == 0 {
			out.Columns = res.Columns
			// Prepend a plate column when the report has none of its own.
			if !hasColumn(out.Columns, "license_plate") {
				out.Columns = append([]Column{{Key: "license_plate", Label: "Plate"}}, out.Columns...)
			}
		}
		if len(res.Rows) == 0 {
			empty = append(empty, plateOrID(plate, id))
		}
		for _, row := range res.Rows {
			if _, ok := row["license_plate"]; !ok {
				row["license_plate"] = plate
			}
			out.Rows = append(out.Rows, row)
		}
		if res.Note != "" && out.Note == "" {
			out.Note = res.Note
		}
	}

	// Columns can still be empty when every vehicle failed or returned nothing.
	if len(out.Columns) == 0 {
		out.Columns = ColumnsFor(key)
		if !hasColumn(out.Columns, "license_plate") {
			out.Columns = append([]Column{{Key: "license_plate", Label: "Plate"}}, out.Columns...)
		}
	}

	var notes []string
	if out.Note != "" {
		notes = append(notes, out.Note)
	}
	if truncated > 0 {
		notes = append(notes, fmt.Sprintf("Only the first %d vehicles were included; %d more were left out.", maxVehiclesPerRun, truncated))
	}
	if len(empty) > 0 {
		notes = append(notes, fmt.Sprintf("No data in this period for: %s.", strings.Join(empty, ", ")))
	}
	if len(failures) > 0 {
		notes = append(notes, "Could not include — "+strings.Join(failures, "; "))
	}
	out.Note = strings.Join(notes, " ")
	return out, nil
}

func hasColumn(cols []Column, key string) bool {
	for _, c := range cols {
		if c.Key == key {
			return true
		}
	}
	return false
}

func plateOrID(plate string, id int64) string {
	if plate != "" {
		return plate
	}
	return fmt.Sprintf("vehicle %d", id)
}

func plateOf(rows []map[string]any) string {
	for _, r := range rows {
		if s, ok := r["license_plate"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// plateFor names a vehicle that produced no rows, so an empty section still
// says which truck it was empty for.
func (s *Service) plateFor(ctx context.Context, tenantID, vehicleID int64) string {
	var plate string
	_ = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		v, err := store.New(tx).GetVehicle(ctx, vehicleID)
		if err == nil {
			plate = v.LicensePlate
		}
		return nil
	})
	return plate
}

// track resolves a vehicle's labels + telemetry history for the range.
func (s *Service) track(ctx context.Context, tenantID int64, vehicleID int64, from, to time.Time) (store.GetVehicleRow, []telemetry.Position, error) {
	var veh store.GetVehicleRow
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		v, err := store.New(tx).GetVehicle(ctx, vehicleID)
		veh = v
		return err
	}); err != nil {
		return veh, nil, fmt.Errorf("vehicle not found")
	}
	if veh.TrackerImei == nil || *veh.TrackerImei == "" {
		return veh, nil, fmt.Errorf("vehicle has no tracker/IMEI")
	}
	pos, err := s.tel.History(ctx, *veh.TrackerImei, from, to, nil, 5000)
	if err != nil {
		return veh, nil, fmt.Errorf("telemetry history unavailable: %w", err)
	}
	// GPS quality filter: drop physically impossible fixes before analytics.
	return veh, telemetry.FilterTeleports(pos), nil
}

func plate(v store.GetVehicleRow) string { return v.LicensePlate }

// Run dispatches a report by key and returns a generic Result.
// maxVehiclesPerRun bounds a multi-vehicle report.
//
// Each vehicle costs one HTTP round trip to the telemetry service for up to
// 5,000 points, so this is a real cost, paid serially, while the user waits.
// Twenty is a large depot; beyond that the honest answer is a fleet-wide
// report rather than a slow one that eventually times out.
const maxVehiclesPerRun = 20

func (s *Service) Run(ctx context.Context, tenantID int64, key string, req RunRequest) (*Result, error) {
	// Several vehicles: run each and stack the results. Done before anything
	// else so every report gets it, and so each run goes through exactly the
	// same code path as a single-vehicle one.
	if len(req.VehicleIDs) > 1 {
		return s.runMany(ctx, tenantID, key, req)
	}
	if len(req.VehicleIDs) == 1 && req.VehicleID == nil {
		req.VehicleID = &req.VehicleIDs[0]
	}

	from, err1 := time.Parse(time.RFC3339, req.From)
	to, err2 := time.Parse(time.RFC3339, req.To)
	needRange := key != "service-scheduling-detail" && key != "driver-management" && key != "driver-operational-summary"
	if needRange && (err1 != nil || err2 != nil) {
		return nil, fmt.Errorf("from/to must be RFC3339")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 60
	}
	title := titleFor(key)
	res := &Result{Key: key, Title: title, Rows: []map[string]any{}}

	// Fleet-wide historical reports come from the rollup, not per-vehicle
	// telemetry, so they are handled before the per-vehicle requirement below.
	switch key {
	case "fleet-daily-summary", "vehicle-utilisation", "idle-leaderboard", "unused-vehicles":
		res.Columns = ColumnsFor(key)
		if err := s.runFleetHistory(ctx, tenantID, key, from, to, res); err != nil {
			return nil, err
		}
		return res, nil
	}

	// Telemetry per-vehicle reports need a vehicle.
	perVehicle := map[string]bool{
		"vehicle-trip-history": true, "vehicle-trip-summary": true, "vehicle-usage-summary": true,
		"vehicle-stop-history": true, "gps-connection-history": true, "speeding-violations": true,
		"fuel-history": true, "temperature-history": true, "door-open-duration": true, "pto-active-duration": true,
		"geofence-inout": true, "harsh-driving": true,
	}
	if perVehicle[key] && req.VehicleID == nil {
		return nil, fmt.Errorf("vehicle_id is required for this report")
	}

	switch key {
	case "vehicle-trip-history":
		veh, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		for _, p := range pos {
			res.Rows = append(res.Rows, map[string]any{
				"date": ts(p.Time), "license_plate": plate(veh), "location": joinLoc(p),
				"speed": numOr(p.Speed), "acc": accStr(p.Ignition),
			})
		}

	case "vehicle-trip-summary":
		veh, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		trips := segmentTrips(pos)
		for i, t := range trips {
			idle := any("—")
			if t.byIgnition {
				idle = round1(t.idleEngineMin)
			}
			res.Rows = append(res.Rows, map[string]any{
				"trip": i + 1, "start": ts(t.start), "end": ts(t.end),
				"duration_min": round1(t.end.Sub(t.start).Minutes()), "distance_km": round1(t.distKm),
				"max_speed": round1(t.maxSpeed), "idle_engine_min": idle,
			})
		}
		if len(trips) > 0 && trips[0].byIgnition {
			res.Note = "Trips are segmented by ignition on → off. \"Idle w/ Engine\" is time stationary with the engine running."
		}
		_ = veh

	case "vehicle-stop-history":
		_, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		for _, c := range clusters(pos) {
			if c.dwellMin < 5 { // ignore trivial pauses
				continue
			}
			res.Rows = append(res.Rows, map[string]any{
				"start": ts(c.start), "end": ts(c.end), "duration_min": round1(c.dwellMin), "location": c.loc,
			})
		}

	case "vehicle-usage-summary":
		_, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		for _, d := range usageByDay(pos) {
			res.Rows = append(res.Rows, map[string]any{
				"day": d.day, "distance_km": round1(d.distKm), "moving_min": round1(d.movingMin), "idle_min": round1(d.idleMin), "utilization": round1(d.util),
			})
		}

	case "speeding-violations":
		veh, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		for _, p := range pos {
			if p.Speed == nil || *p.Speed <= limit {
				continue
			}
			res.Rows = append(res.Rows, map[string]any{
				"date": ts(p.Time), "license_plate": plate(veh), "location": joinLoc(p),
				"speed": round1(*p.Speed), "limit": limit, "over_by": round1(*p.Speed - limit),
			})
		}

	case "gps-connection-history":
		_, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		// Last known fix before each gap = where tracking was lost (dead zones).
		res.Columns = ColumnsFor(key)
		const gapMin = 15.0
		for i := 1; i < len(pos); i++ {
			gap := pos[i].Time.Sub(pos[i-1].Time).Minutes()
			if gap >= gapMin {
				res.Rows = append(res.Rows, map[string]any{
					"offline_from": ts(pos[i-1].Time), "offline_to": ts(pos[i].Time), "duration_min": round1(gap),
					"last_location": joinLoc(pos[i-1]), "lat": numOr(pos[i-1].Lat), "lon": numOr(pos[i-1].Lon),
				})
			}
		}

	case "vehicle-alerts-history":
		res.Columns = ColumnsFor(key)
		rows, err := s.alertRows(ctx, tenantID, req, from, to)
		if err != nil {
			return nil, err
		}
		res.Rows = rows

	case "vehicle-alerts-summary":
		res.Columns = ColumnsFor(key)
		rows, err := s.alertSummary(ctx, tenantID, from, to)
		if err != nil {
			return nil, err
		}
		res.Rows = rows

	case "service-scheduling-detail":
		res.Columns = ColumnsFor(key)
		rows, err := s.serviceRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		res.Rows = rows

	case "driver-management":
		res.Columns = ColumnsFor(key)
		rows, err := s.driverRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		res.Rows = rows

	case "driver-operational-summary":
		res.Columns = ColumnsFor(key)
		rows, err := s.driverRows(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		// Reuse roster; per-driver trip metrics need driver↔trip attribution (not
		// yet tracked), so metrics are omitted for now.
		for _, r := range rows {
			res.Rows = append(res.Rows, map[string]any{"full_name": r["full_name"], "license_no": r["license_no"], "status": r["status"]})
		}
		res.Note = "Per-driver trip/distance/behavior metrics require driver-trip attribution, which isn't ingested yet — showing the roster."

	// ---- Sensor-dependent reports: structured but empty until the sensor/data
	// source exists. The columns are ready so the report renders + exports.
	case "fuel-history":
		res.Columns = ColumnsFor(key)
		res.Note = "No fuel-sensor data for this vehicle. Fuel history will populate for units with a fuel sensor."
	case "temperature-history":
		res.Columns = ColumnsFor(key)
		res.Note = "No temperature-sensor data. Populates for cold-chain units with a temperature probe."
	case "door-open-duration":
		res.Columns = ColumnsFor(key)
		res.Note = "No door-sensor data. Populates for units wired with a door sensor."
	case "pto-active-duration":
		res.Columns = ColumnsFor(key)
		res.Note = "No PTO-sensor data. Populates for units wired with a PTO input."
	case "geofence-inout":
		res.Columns = ColumnsFor(key)
		rows, err := s.geofenceRows(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Rows = rows
		if len(rows) == 0 {
			res.Note = "No geofence events in this period. Create geofences (Live View → Geofences) and attach a GEOFENCE rule to the vehicle to record enter/exit."
		}

	case "harsh-driving":
		veh, pos, err := s.track(ctx, tenantID, *req.VehicleID, from, to)
		if err != nil {
			return nil, err
		}
		res.Columns = ColumnsFor(key)
		const harshAccel, harshBrake, maxGapSec = 10.0, -12.0, 30.0
		for i := 1; i < len(pos); i++ {
			p, prev := pos[i], pos[i-1]
			if p.Speed == nil || prev.Speed == nil {
				continue
			}
			dt := p.Time.Sub(prev.Time).Seconds()
			if dt <= 0 || dt > maxGapSec {
				continue
			}
			rate := (*p.Speed - *prev.Speed) / dt
			var event string
			if rate >= harshAccel {
				event = "Harsh acceleration"
			} else if rate <= harshBrake {
				event = "Harsh braking"
			} else {
				continue
			}
			res.Rows = append(res.Rows, map[string]any{
				"date": ts(p.Time), "license_plate": plate(veh), "event": event,
				"speed": round1(*p.Speed), "rate": round1(rate), "location": joinLoc(p),
			})
		}

	default:
		return nil, fmt.Errorf("unknown report: %s", key)
	}
	// Never return a nil slice — the frontend expects an array (empty = no data).
	if res.Rows == nil {
		res.Rows = []map[string]any{}
	}
	return res, nil
}

func titleFor(key string) string {
	for _, m := range Catalog {
		if m.Key == key {
			return m.Title
		}
	}
	return key
}

func numOr(f *float64) any {
	if f == nil {
		return nil
	}
	return round1(*f)
}
func accStr(b *bool) any {
	if b == nil {
		return "—"
	}
	if *b {
		return "On"
	}
	return "Off"
}
func round1(f float64) float64 { return math.Round(f*10) / 10 }

// ---- telemetry-derived helpers ----

type tripSeg struct {
	start, end    time.Time
	distKm        float64
	maxSpeed      float64
	idleEngineMin float64 // minutes stationary with the engine (ignition) on
	byIgnition    bool
}

// segmentTrips splits a track into trips. When the device reports ignition,
// trips are the ignition on→off runs (the proper telematics definition) and
// each trip carries its engine-on idle time; otherwise it falls back to
// movement gaps.
func segmentTrips(pos []telemetry.Position) []tripSeg {
	hasIgn := false
	for _, p := range pos {
		if p.Ignition != nil {
			hasIgn = true
			break
		}
	}
	if !hasIgn {
		return segmentTripsByMovement(pos)
	}

	var trips []tripSeg
	var cur *tripSeg
	var prev *telemetry.Position
	ign := false
	flush := func() {
		if cur != nil && cur.end.Sub(cur.start).Minutes() >= 1 {
			trips = append(trips, *cur)
		}
		cur = nil
		prev = nil
	}
	for i := range pos {
		p := &pos[i]
		if p.Ignition != nil {
			ign = *p.Ignition
		}
		if !ign {
			flush()
			continue
		}
		if cur == nil {
			cur = &tripSeg{start: p.Time, end: p.Time, byIgnition: true}
		}
		if prev != nil {
			dt := p.Time.Sub(prev.Time).Minutes()
			if dt > 0 && dt <= 30 { // skip data gaps
				sp := 0.0
				if p.Speed != nil {
					sp = *p.Speed
				}
				if sp <= 3 {
					cur.idleEngineMin += dt // engine running, going nowhere
				}
			}
			if prev.Lat != nil && prev.Lon != nil && p.Lat != nil && p.Lon != nil {
				cur.distKm += hav(*prev.Lat, *prev.Lon, *p.Lat, *p.Lon)
			}
		}
		if p.Speed != nil && *p.Speed > cur.maxSpeed {
			cur.maxSpeed = *p.Speed
		}
		cur.end = p.Time
		prev = p
	}
	flush()
	return trips
}

// segmentTripsByMovement splits a track into trips separated by stops >= 15 min
// (fallback for devices without an ignition feed).
func segmentTripsByMovement(pos []telemetry.Position) []tripSeg {
	var trips []tripSeg
	var cur *tripSeg
	flush := func() {
		if cur != nil && cur.end.Sub(cur.start) > 0 {
			trips = append(trips, *cur)
		}
		cur = nil
	}
	for i, p := range pos {
		if p.Lat == nil || p.Lon == nil {
			continue
		}
		if cur == nil {
			cur = &tripSeg{start: p.Time, end: p.Time}
		}
		if i > 0 && pos[i-1].Lat != nil && pos[i-1].Lon != nil {
			gap := p.Time.Sub(pos[i-1].Time).Minutes()
			moved := hav(*pos[i-1].Lat, *pos[i-1].Lon, *p.Lat, *p.Lon)
			if gap >= 15 && moved < 0.1 { // long stationary gap = trip boundary
				flush()
				cur = &tripSeg{start: p.Time, end: p.Time}
			} else {
				cur.distKm += moved
			}
		}
		cur.end = p.Time
		if p.Speed != nil && *p.Speed > cur.maxSpeed {
			cur.maxSpeed = *p.Speed
		}
	}
	flush()
	return trips
}

type stopSeg struct {
	start, end time.Time
	dwellMin   float64
	loc        string
}

func clusters(pos []telemetry.Position) []stopSeg {
	var out []stopSeg
	i := 0
	for i < len(pos) {
		if pos[i].Lat == nil || pos[i].Lon == nil {
			i++
			continue
		}
		j := i
		for j+1 < len(pos) && pos[j+1].Lat != nil && pos[j+1].Lon != nil &&
			hav(*pos[i].Lat, *pos[i].Lon, *pos[j+1].Lat, *pos[j+1].Lon)*1000 <= 60 {
			j++
		}
		out = append(out, stopSeg{start: pos[i].Time, end: pos[j].Time, dwellMin: pos[j].Time.Sub(pos[i].Time).Minutes(), loc: joinLoc(pos[i])})
		i = j + 1
	}
	return out
}

type dayUsage struct {
	day       string
	distKm    float64
	movingMin float64
	idleMin   float64
	util      float64
}

func usageByDay(pos []telemetry.Position) []dayUsage {
	byDay := map[string]*dayUsage{}
	var order []string
	for i := 1; i < len(pos); i++ {
		p, prev := pos[i], pos[i-1]
		if p.Lat == nil || p.Lon == nil || prev.Lat == nil || prev.Lon == nil {
			continue
		}
		day := p.Time.Format("2006-01-02")
		u := byDay[day]
		if u == nil {
			u = &dayUsage{day: day}
			byDay[day] = u
			order = append(order, day)
		}
		dt := p.Time.Sub(prev.Time).Minutes()
		if dt <= 0 || dt > 30 { // ignore large gaps
			continue
		}
		sp := 0.0
		if p.Speed != nil {
			sp = *p.Speed
		}
		if sp > 3 {
			u.movingMin += dt
			u.distKm += hav(*prev.Lat, *prev.Lon, *p.Lat, *p.Lon)
		} else {
			u.idleMin += dt
		}
	}
	sort.Strings(order)
	out := make([]dayUsage, 0, len(order))
	for _, d := range order {
		u := byDay[d]
		total := u.movingMin + u.idleMin
		if total > 0 {
			u.util = u.movingMin / total * 100
		}
		out = append(out, *u)
	}
	return out
}

// ---- DB-derived helpers ----

func (s *Service) alertRows(ctx context.Context, tenantID int64, req RunRequest, from, to time.Time) ([]map[string]any, error) {
	var rows []map[string]any
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		list, err := store.New(tx).ListAlerts(ctx, store.ListAlertsParams{
			AlertCode: conv.StrPtr(req.AlertCode),
			From:      pgtype.Timestamptz{Time: from, Valid: true}, To: pgtype.Timestamptz{Time: to, Valid: true},
			Lim: 10000, Off: 0,
		})
		if err != nil {
			return err
		}
		for _, a := range list {
			rows = append(rows, map[string]any{
				"date": ts(a.OccurredAt.Time), "license_plate": deref(a.LicensePlate), "alert_name": deref(a.AlertName),
				"severity": deref(a.Severity), "actual": conv.NumF(a.ActualValue), "limit": conv.NumF(a.LimitValue),
				"address": deref(a.Address), "check_status": a.CheckStatus,
			})
		}
		return nil
	})
	return rows, err
}

func (s *Service) alertSummary(ctx context.Context, tenantID int64, from, to time.Time) ([]map[string]any, error) {
	type agg struct {
		name                  string
		count, urgent, unchkd int
	}
	byCode := map[string]*agg{}
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		list, err := store.New(tx).ListAlerts(ctx, store.ListAlertsParams{
			From: pgtype.Timestamptz{Time: from, Valid: true}, To: pgtype.Timestamptz{Time: to, Valid: true}, Lim: 100000, Off: 0,
		})
		if err != nil {
			return err
		}
		for _, a := range list {
			code := deref(a.AlertCode)
			g := byCode[code]
			if g == nil {
				g = &agg{name: deref(a.AlertName)}
				byCode[code] = g
			}
			g.count++
			if deref(a.Severity) == "urgent" {
				g.urgent++
			}
			if a.CheckStatus == "unchecked" {
				g.unchkd++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return byCode[codes[i]].count > byCode[codes[j]].count })
	rows := make([]map[string]any, 0, len(codes))
	for _, c := range codes {
		g := byCode[c]
		rows = append(rows, map[string]any{"alert_code": c, "alert_name": g.name, "count": g.count, "urgent": g.urgent, "unchecked": g.unchkd})
	}
	return rows, nil
}

func (s *Service) serviceRows(ctx context.Context, tenantID int64) ([]map[string]any, error) {
	var rows []map[string]any
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		list, err := store.New(tx).ListServiceSchedules(ctx, store.ListServiceSchedulesParams{Lim: 5000, Off: 0})
		if err != nil {
			return err
		}
		for _, sc := range list {
			rows = append(rows, map[string]any{
				"license_plate": deref(sc.LicensePlate), "component": deref(sc.Component), "scheduler": deref(sc.Scheduler),
				"next_due_at": conv.DateStr(sc.NextDueAt), "status": sc.Status,
				"current_odometer": conv.NumF(sc.CurrentOdometer), "next_odometer": conv.NumF(sc.NextOdometer),
			})
		}
		return nil
	})
	return rows, err
}

func (s *Service) driverRows(ctx context.Context, tenantID int64) ([]map[string]any, error) {
	var rows []map[string]any
	role := "driver"
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		list, err := store.New(tx).ListEmployees(ctx, store.ListEmployeesParams{Role: &role, Lim: 5000, Off: 0})
		if err != nil {
			return err
		}
		for _, e := range list {
			rows = append(rows, map[string]any{
				"employee_no": deref(e.EmployeeNo), "full_name": e.FullName, "phone": deref(e.Phone),
				"license_no": deref(e.LicenseNo), "license_class": deref(e.LicenseClass),
				"license_expiry": conv.DateStr(e.LicenseExpiry), "status": e.Status,
			})
		}
		return nil
	})
	return rows, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// geofenceRows builds Entry/Exit rows from GEOFENCE_ENTER/EXIT alerts written by
// the telemetry collector, pairing an exit with its enter to compute dwell.
func (s *Service) geofenceRows(ctx context.Context, tenantID, vehicleID int64, from, to time.Time) ([]map[string]any, error) {
	rows := []map[string]any{}
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `
			SELECT created_at, alert_code, COALESCE(alert_name, '')
			FROM alerts
			WHERE vehicle_id = $1 AND alert_code IN ('GEOFENCE_ENTER','GEOFENCE_EXIT')
			  AND created_at >= $2 AND created_at <= $3
			ORDER BY created_at`, vehicleID, from, to)
		if err != nil {
			return err
		}
		defer rs.Close()
		enteredAt := map[string]time.Time{} // geofence name -> enter time
		for rs.Next() {
			var at time.Time
			var code, name string
			if err := rs.Scan(&at, &code, &name); err != nil {
				return err
			}
			gf := strings.TrimPrefix(strings.TrimPrefix(name, "Entered "), "Exited ")
			row := map[string]any{"geofence": gf, "at": ts(at), "dwell_min": any("—")}
			if code == "GEOFENCE_ENTER" {
				row["event"] = "Enter"
				enteredAt[gf] = at
			} else {
				row["event"] = "Exit"
				if in, ok := enteredAt[gf]; ok {
					row["dwell_min"] = round1(at.Sub(in).Minutes())
					delete(enteredAt, gf)
				}
			}
			rows = append(rows, row)
		}
		return rs.Err()
	})
	return rows, err
}

// run godoc
//
//	@Summary	Run any catalog report by key (generic columns + rows)
//	@Tags		reports
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		key		path		string				true	"report key"
//	@Param		body	body		report.RunRequest	true	"parameters"
//	@Success	200		{object}	report.Result
//	@Router		/v1/reports/{key}/run [post]
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var req RunRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := h.svc.Run(r.Context(), auth.TenantID(r.Context()), key, req)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
