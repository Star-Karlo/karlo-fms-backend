// Package liveview joins the vehicle registry with live positions from the
// telemetry service. It degrades gracefully: if telemetry is unreachable the
// registry still renders (vehicles come back offline, not a 500).
package liveview

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/routing"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// liveFields trims the upstream payload to what the map needs (drops metadata).
var liveFields = []string{"lat", "lon", "speed", "bearing", "ignition", "kecamatan", "kabupaten", "kota", "provinsi", "jalan"}

// historyFields trims the trip-history payload to what TripPoint needs. Without
// this the upstream returns the full per-point metadata blob (~2.5x larger and
// slower), which pushed the request past the telemetry client timeout.
var historyFields = []string{"lat", "lon", "speed", "bearing", "ignition", "time", "jalan", "kecamatan", "kota", "provinsi"}

// Service composes the registry store with the telemetry client.
type Service struct {
	pool *db.Pool
	tel  *telemetry.Client
}

// NewService builds the live-view service.
func NewService(pool *db.Pool, tel *telemetry.Client) *Service {
	return &Service{pool: pool, tel: tel}
}

// List returns every active, tracked vehicle enriched with its live fix.
func (s *Service) List(ctx context.Context, tenantID int64, fleetGroupID *int64) ([]VehicleLive, error) {
	var rows []store.ListLiveViewVehiclesRow
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var err error
		rows, err = store.New(tx).ListLiveViewVehicles(ctx, fleetGroupID)
		return err
	})
	if err != nil {
		return nil, err
	}

	out := make([]VehicleLive, 0, len(rows))
	imeis := make([]string, 0, len(rows))
	for _, r := range rows {
		var tid int64
		if r.TrackerID != nil {
			tid = *r.TrackerID
		}
		imei := ""
		if r.TrackerImei != nil {
			imei = *r.TrackerImei
		}
		out = append(out, VehicleLive{
			VehicleID: r.VehicleID, LicensePlate: r.LicensePlate, VehicleType: r.VehicleType, RegistryStatus: r.Status,
			FleetGroupName: r.FleetGroupName,
			Driver:         Driver{ID: r.DriverID, Name: r.DriverName, Phone: r.DriverPhone},
			Tracker:        Tracker{ID: tid, IMEI: imei},
			CameraDeviceID: r.CameraDeviceID,
			DriveState:     StateOffline,
		})
		if imei != "" {
			imeis = append(imeis, imei)
		}
	}

	// Fetch live fixes; on telemetry failure, log and return offline vehicles.
	byIMEI := map[string]*telemetry.Position{}
	if len(imeis) > 0 {
		positions, err := s.tel.LiveMany(ctx, imeis, liveFields)
		if err != nil {
			log.Printf("liveview: telemetry unavailable, returning offline set: %v", err)
		} else {
			for i := range positions {
				byIMEI[positions[i].IMEI] = &positions[i]
			}
		}
	}

	now := time.Now()
	for i := range out {
		enrich(&out[i], byIMEI[out[i].Tracker.IMEI], now)
	}

	// Camera fallback: for a vehicle with a linked dashcam but no live tracker
	// fix, stand its position in from the camera's own GPS. One telemetry call,
	// only when at least one such vehicle exists. The camera is authoritative for
	// per-axis behaviour elsewhere; here it is simply the location of last resort.
	needCam := false
	for i := range out {
		if out[i].Position == nil && out[i].CameraDeviceID != "" {
			needCam = true
			break
		}
	}
	if needCam {
		if cams, err := s.tel.Dashcams(ctx, ""); err != nil {
			log.Printf("liveview: camera positions unavailable: %v", err)
		} else {
			byDevice := make(map[string]*telemetry.Dashcam, len(cams))
			for i := range cams {
				byDevice[cams[i].DeviceID] = &cams[i]
			}
			for i := range out {
				if out[i].Position != nil || out[i].CameraDeviceID == "" {
					continue
				}
				if d := byDevice[out[i].CameraDeviceID]; d != nil {
					enrichCamera(&out[i], d.Lat, d.Lon, d.Online, now)
				}
			}
		}
	}

	// Attach collector state (battery / GSM / odometer) plus the latest service
	// odometer — one cheap local query fleet-wide, powers the list filters.
	type telState struct{ mv, gsm, odo, svcKm *float64 }
	states := map[int64]telState{}
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `
			SELECT s.vehicle_id, s.last_voltage_mv::float8, s.last_gsm_signal::float8,
			       s.last_odometer_km::float8, sv.last_service_km
			FROM vehicle_telemetry_state s
			LEFT JOIN (
				SELECT vehicle_id, max(last_odometer)::float8 AS last_service_km
				FROM service_schedules WHERE last_odometer IS NOT NULL GROUP BY vehicle_id
			) sv ON sv.vehicle_id = s.vehicle_id`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var vid int64
			var st telState
			if err := rs.Scan(&vid, &st.mv, &st.gsm, &st.odo, &st.svcKm); err != nil {
				return err
			}
			states[vid] = st
		}
		return rs.Err()
	}); err != nil {
		log.Printf("liveview: telemetry state join failed: %v", err) // non-fatal
	}
	for i := range out {
		if st, ok := states[out[i].VehicleID]; ok {
			if st.mv != nil {
				v := math.Round(*st.mv/100) / 10
				out[i].BatteryV = &v
			}
			out[i].GsmSignal = st.gsm
			if st.svcKm != nil {
				out[i].HasServiceRecord = true
				if st.odo != nil && *st.odo > *st.svcKm {
					d := math.Round((*st.odo - *st.svcKm))
					out[i].KmSinceService = &d
				}
			}
		}
	}
	return out, nil
}

// One returns a single vehicle enriched with its live fix.
func (s *Service) One(ctx context.Context, tenantID, vehicleID int64) (*VehicleLive, error) {
	var row store.GetLiveViewVehicleRow
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var err error
		row, err = store.New(tx).GetLiveViewVehicle(ctx, vehicleID)
		return err
	})
	if err != nil {
		return nil, err
	}
	var tid int64
	if row.TrackerID != nil {
		tid = *row.TrackerID
	}
	imei := ""
	if row.TrackerImei != nil {
		imei = *row.TrackerImei
	}
	v := VehicleLive{
		VehicleID: row.VehicleID, LicensePlate: row.LicensePlate, VehicleType: row.VehicleType, RegistryStatus: row.Status,
		FleetGroupName: row.FleetGroupName,
		Driver:         Driver{ID: row.DriverID, Name: row.DriverName, Phone: row.DriverPhone},
		Tracker:        Tracker{ID: tid, IMEI: imei},
		CameraDeviceID: row.CameraDeviceID,
		DriveState:     StateOffline,
	}
	now := time.Now()
	var pos *telemetry.Position
	if imei != "" {
		if p, err := s.tel.LiveOne(ctx, imei); err != nil {
			log.Printf("liveview: telemetry unavailable for %s: %v", imei, err)
		} else {
			pos = p
		}
	}
	enrich(&v, pos, now)

	// No tracker fix but a linked camera — stand the position in from its GPS.
	if v.Position == nil && v.CameraDeviceID != "" {
		if cams, err := s.tel.Dashcams(ctx, ""); err == nil {
			for i := range cams {
				if cams[i].DeviceID == v.CameraDeviceID {
					enrichCamera(&v, cams[i].Lat, cams[i].Lon, cams[i].Online, now)
					break
				}
			}
		}
	}
	return &v, nil
}

// Handler exposes the live-view endpoints.
type Handler struct {
	svc *Service
	// matcher is optional; nil means road snapping is unavailable and its
	// endpoint answers 503 while everything else keeps working.
	matcher routing.Matcher
}

// NewHandler builds the live-view HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the live-view endpoints (any authenticated user).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/live-view", h.list)
	r.Get("/v1/live-view/{vehicle_id}", h.one)
	r.Get("/v1/vehicles/{vehicle_id}/history", h.history)
	r.Get("/v1/vehicles/{vehicle_id}/history/snapped", h.snapHistory)
	r.Get("/v1/analytics/history", h.fleetHistory)
	r.Get("/v1/analytics/fleet", h.fleetAnalytics)
	r.Get("/v1/vehicles/{vehicle_id}/behavior", h.behavior)
	r.Get("/v1/vehicles/{vehicle_id}/fuel-trip", h.fuelTrip)
	r.Get("/v1/vehicles/{vehicle_id}/driving-events", h.drivingEvents)
	r.Get("/v1/vehicles/{vehicle_id}/analytics", h.analytics)
	r.Get("/v1/coverage", h.coverage)
}

// coverage godoc
//
//	@Summary	Signal / terrain coverage grid (proxied GeoJSON from the telemetry service)
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		metric		query	string	false	"signal | terrain | altitude"
//	@Param		provider	query	string	false	"comma-separated: telkomsel,indosat,xl (omit for all)"
//	@Param		bbox		query	string	false	"minLon,minLat,maxLon,maxLat"
//	@Success	200	{object}	map[string]interface{}
//	@Router		/v1/coverage [get]
func (h *Handler) coverage(w http.ResponseWriter, r *http.Request) {
	raw, err := h.svc.tel.Coverage(r.Context(), r.URL.RawQuery)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "coverage unavailable: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// The grid only moves when the nightly rollup runs, and the payload is a few
	// MB — worth caching hard. "private" because this rides an authenticated
	// request: the browser may reuse it, shared proxies must not.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(raw)
}

// VehSpeed pairs a vehicle with a speed value.
type VehSpeed struct {
	VehicleID int64   `json:"vehicle_id"`
	Plate     string  `json:"license_plate"`
	Speed     float64 `json:"speed"`
}

// Bucket is a labeled count.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// FleetAnalytics is the live speed/behavior summary derived from GPS telemetry.
type FleetAnalytics struct {
	Total          int            `json:"total"`
	Online         int            `json:"online"`
	States         map[string]int `json:"states"`
	SpeedBuckets   []Bucket       `json:"speed_buckets"`
	OverspeedLimit float64        `json:"overspeed_limit"`
	OverspeedCount int            `json:"overspeed_count"`
	Overspeeders   []VehSpeed     `json:"overspeeders"`
	AvgMovingSpeed float64        `json:"avg_moving_speed"`
	TopSpeeds      []VehSpeed     `json:"top_speeds"`
	Provinces      []Bucket       `json:"provinces"`
}

// fleetAnalytics serves the live fleet speed/behavior summary.
//
// @Summary     Fleet speed & behavior analytics (live, from GPS)
// @Description Live fleet-wide speed distribution, overspeeder + top-speed lists, average moving speed and province spread, derived from the latest GPS positions.
// @Tags        analytics
// @Produce     json
// @Param       limit query number false "overspeed threshold km/h (default 60)"
// @Success     200 {object} FleetAnalytics
// @Failure     500 {object} map[string]string
// @Security    BearerAuth
// @Router      /v1/analytics/fleet [get]
func (h *Handler) fleetAnalytics(w http.ResponseWriter, r *http.Request) {
	limit := 60.0
	if v := r.URL.Query().Get("limit"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			limit = f
		}
	}
	vehicles, err := h.svc.List(r.Context(), auth.TenantID(r.Context()), nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := FleetAnalytics{
		States:         map[string]int{"moving": 0, "idle": 0, "parking": 0, "offline": 0},
		OverspeedLimit: limit,
		SpeedBuckets: []Bucket{
			{Label: "0", Count: 0}, {Label: "1-20", Count: 0}, {Label: "21-40", Count: 0},
			{Label: "41-60", Count: 0}, {Label: "61-80", Count: 0}, {Label: "80+", Count: 0},
		},
	}
	provCount := map[string]int{}
	var movingSum float64
	var movingN int
	var all []VehSpeed

	for _, v := range vehicles {
		out.Total++
		out.States[v.DriveState]++
		if v.Online {
			out.Online++
		}
		if v.Position == nil || v.Position.Speed == nil {
			continue
		}
		sp := *v.Position.Speed
		vs := VehSpeed{VehicleID: v.VehicleID, Plate: v.LicensePlate, Speed: sp}
		all = append(all, vs)
		switch {
		case sp <= 0:
			out.SpeedBuckets[0].Count++
		case sp <= 20:
			out.SpeedBuckets[1].Count++
		case sp <= 40:
			out.SpeedBuckets[2].Count++
		case sp <= 60:
			out.SpeedBuckets[3].Count++
		case sp <= 80:
			out.SpeedBuckets[4].Count++
		default:
			out.SpeedBuckets[5].Count++
		}
		if sp > 0 {
			movingSum += sp
			movingN++
		}
		if sp > limit {
			out.OverspeedCount++
		}
		if v.Position.Provinsi != "" {
			provCount[v.Position.Provinsi]++
		}
	}
	if movingN > 0 {
		out.AvgMovingSpeed = movingSum / float64(movingN)
	}
	// Top speeds + overspeeders (sorted desc).
	sort.Slice(all, func(i, j int) bool { return all[i].Speed > all[j].Speed })
	for i := 0; i < len(all) && i < 10; i++ {
		out.TopSpeeds = append(out.TopSpeeds, all[i])
	}
	for _, v := range all {
		if v.Speed > limit {
			out.Overspeeders = append(out.Overspeeders, v)
			if len(out.Overspeeders) >= 20 {
				break
			}
		}
	}
	// Province distribution (top 8).
	type pc struct {
		name string
		n    int
	}
	var provs []pc
	for k, n := range provCount {
		provs = append(provs, pc{k, n})
	}
	sort.Slice(provs, func(i, j int) bool { return provs[i].n > provs[j].n })
	for i := 0; i < len(provs) && i < 8; i++ {
		out.Provinces = append(out.Provinces, Bucket{Label: provs[i].name, Count: provs[i].n})
	}
	if out.Overspeeders == nil {
		out.Overspeeders = []VehSpeed{}
	}
	if out.TopSpeeds == nil {
		out.TopSpeeds = []VehSpeed{}
	}
	if out.Provinces == nil {
		out.Provinces = []Bucket{}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// BehaviorEvent is a detected driving event on a vehicle's track.
type BehaviorEvent struct {
	Type    string    `json:"type"` // overspeed | harsh_accel | harsh_brake
	Time    time.Time `json:"time"`
	Lat     *float64  `json:"lat"`
	Lon     *float64  `json:"lon"`
	Speed   float64   `json:"speed"`
	Value   float64   `json:"value"` // km/h over limit, or km/h/s for harsh
	Address string    `json:"address"`
}

// BehaviorSummary aggregates a track's driving behavior.
type BehaviorSummary struct {
	Points         int     `json:"points"`
	DistanceKm     float64 `json:"distance_km"`
	MaxSpeed       float64 `json:"max_speed"`
	AvgMovingSpeed float64 `json:"avg_moving_speed"`
	OverspeedCount int     `json:"overspeed_count"`
	HarshAccel     int     `json:"harsh_accel"`
	HarshBrake     int     `json:"harsh_brake"`
	IdleCount      int     `json:"idle_count"`       // stops over the idle threshold
	TotalIdleMin   float64 `json:"total_idle_min"`   // summed dwell of those stops
	LongestStopMin float64 `json:"longest_stop_min"` // longest single stop
}

// IdleStop is a place the vehicle sat longer than the idle threshold.
type IdleStop struct {
	Lat      *float64  `json:"lat"`
	Lon      *float64  `json:"lon"`
	Start    time.Time `json:"start_time"`
	End      time.Time `json:"end_time"`
	DwellMin float64   `json:"dwell_min"`
}

// BehaviorResult is the per-vehicle behavior analysis.
type BehaviorResult struct {
	OverspeedLimit float64         `json:"overspeed_limit"`
	IdleThreshold  float64         `json:"idle_threshold_min"`
	Summary        BehaviorSummary `json:"summary"`
	Events         []BehaviorEvent `json:"events"`
	Stops          []IdleStop      `json:"stops"`
	// Vehicle fuel config, echoed so the client can turn distance into an
	// estimated litres consumed for this trip. Null when the vehicle has no
	// figures set — the estimate is then simply not shown.
	FuelRatioKmpl  *float64 `json:"fuel_ratio_kmpl"`
	FuelTankLiters *float64 `json:"fuel_tank_liters"`
}

// fuelConfig reads a vehicle's fuel figures, tenant-scoped. Returns nils when
// the vehicle has none — not an error, just nothing to estimate with.
func (s *Service) fuelConfig(ctx context.Context, tenantID, vehicleID int64) (ratio, tank *float64) {
	_ = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT fuel_ratio_kmpl, fuel_tank_liters FROM vehicles WHERE id = $1`, vehicleID).
			Scan(&ratio, &tank)
	})
	return ratio, tank
}

// haversine returns the great-circle distance in km between two lat/lon points.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// stopRadiusM: points within this many meters of a cluster anchor are "the same
// place". Mirrors the frontend (tripAnalysis.ts) so the API and UI agree.
const stopRadiusM = 60.0

// stopCluster is a run of consecutive co-located points (the vehicle sat still);
// DwellMin is the time span across the cluster.
type stopCluster struct {
	StartIdx, EndIdx int
	Lat, Lon         float64
	Start, End       time.Time
	DwellMin         float64
}

// stopClusters groups consecutive fixes that stay within stopRadiusM of the
// cluster anchor. Points without a fix are skipped (never in a cluster).
func stopClusters(pts []TripPoint) []stopCluster {
	var out []stopCluster
	i := 0
	for i < len(pts) {
		if pts[i].Lat == nil || pts[i].Lon == nil {
			i++
			continue
		}
		j := i
		sumLat, sumLon, n := *pts[i].Lat, *pts[i].Lon, 1
		for j+1 < len(pts) && pts[j+1].Lat != nil && pts[j+1].Lon != nil &&
			haversine(*pts[i].Lat, *pts[i].Lon, *pts[j+1].Lat, *pts[j+1].Lon)*1000 <= stopRadiusM {
			j++
			sumLat += *pts[j].Lat
			sumLon += *pts[j].Lon
			n++
		}
		out = append(out, stopCluster{
			StartIdx: i, EndIdx: j, Lat: sumLat / float64(n), Lon: sumLon / float64(n),
			Start: pts[i].Time, End: pts[j].Time, DwellMin: pts[j].Time.Sub(pts[i].Time).Minutes(),
		})
		i = j + 1
	}
	return out
}

// behavior godoc
//
//	@Summary	Per-vehicle driving behavior (overspeed + harsh accel/brake) from GPS
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path		int		true	"vehicle id"
//	@Param		from		query		string	true	"RFC3339"
//	@Param		to			query		string	true	"RFC3339"
//	@Param		limit			query	number	false	"overspeed limit km/h (default 60)"
//	@Param		idle_threshold	query	number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Success	200	{object}	liveview.BehaviorResult
//	@Router		/v1/vehicles/{vehicle_id}/behavior [get]
func (h *Handler) behavior(w http.ResponseWriter, r *http.Request) {
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

	res, err := h.svc.Behavior(r.Context(), auth.TenantID(r.Context()), id, from, to, limit, idleThreshold)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "history unavailable: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

// Behavior computes the GPS-derived driving behaviour (overspeed, harsh
// accel/brake, idle stops, distance) for a vehicle over [from,to]. Extracted
// from the handler so the consolidated analytics endpoint can reuse it.
func (s *Service) Behavior(ctx context.Context, tenantID, vehicleID int64, from, to time.Time, limit, idleThreshold float64) (*BehaviorResult, error) {
	pts, err := s.History(ctx, tenantID, vehicleID, from, to, 5000)
	if err != nil {
		return nil, err
	}

	// Harsh thresholds (km/h per second). Only trusted when samples are close in
	// time — GPS history is downsampled, so far-apart points are skipped.
	const harshAccel, harshBrake, maxGapSec = 10.0, -12.0, 30.0
	res := BehaviorResult{OverspeedLimit: limit, IdleThreshold: idleThreshold, Events: []BehaviorEvent{}, Stops: []IdleStop{}}
	var movingSum float64
	var movingN int
	res.Summary.Points = len(pts)

	for i, p := range pts {
		if p.Speed == nil {
			continue
		}
		sp := *p.Speed
		if sp > res.Summary.MaxSpeed {
			res.Summary.MaxSpeed = sp
		}
		if sp > 0 {
			movingSum += sp
			movingN++
		}
		if sp > limit {
			res.Summary.OverspeedCount++
			res.Events = append(res.Events, BehaviorEvent{Type: "overspeed", Time: p.Time, Lat: p.Lat, Lon: p.Lon, Speed: sp, Value: sp - limit, Address: p.Address})
		}
		if i > 0 && pts[i-1].Speed != nil {
			prev := pts[i-1]
			dt := p.Time.Sub(prev.Time).Seconds()
			if dt > 0 && dt <= maxGapSec {
				rate := (sp - *prev.Speed) / dt // km/h per second
				if rate >= harshAccel {
					res.Summary.HarshAccel++
					res.Events = append(res.Events, BehaviorEvent{Type: "harsh_accel", Time: p.Time, Lat: p.Lat, Lon: p.Lon, Speed: sp, Value: rate, Address: p.Address})
				} else if rate <= harshBrake {
					res.Summary.HarshBrake++
					res.Events = append(res.Events, BehaviorEvent{Type: "harsh_brake", Time: p.Time, Lat: p.Lat, Lon: p.Lon, Speed: sp, Value: rate, Address: p.Address})
				}
			}
		}
		if i > 0 && p.Lat != nil && p.Lon != nil && pts[i-1].Lat != nil && pts[i-1].Lon != nil {
			res.Summary.DistanceKm += haversine(*pts[i-1].Lat, *pts[i-1].Lon, *p.Lat, *p.Lon)
		}
	}
	if movingN > 0 {
		res.Summary.AvgMovingSpeed = movingSum / float64(movingN)
	}
	res.Summary.DistanceKm = math.Round(res.Summary.DistanceKm*10) / 10

	// Idle stops: clusters that sat longer than the threshold.
	for _, c := range stopClusters(pts) {
		if c.EndIdx <= c.StartIdx || c.DwellMin < idleThreshold {
			continue
		}
		dwell := math.Round(c.DwellMin*10) / 10
		lat, lon := c.Lat, c.Lon
		res.Stops = append(res.Stops, IdleStop{Lat: &lat, Lon: &lon, Start: c.Start, End: c.End, DwellMin: dwell})
		res.Summary.IdleCount++
		res.Summary.TotalIdleMin += dwell
		if dwell > res.Summary.LongestStopMin {
			res.Summary.LongestStopMin = dwell
		}
	}
	res.Summary.TotalIdleMin = math.Round(res.Summary.TotalIdleMin*10) / 10

	res.FuelRatioKmpl, res.FuelTankLiters = s.fuelConfig(ctx, tenantID, vehicleID)
	return &res, nil
}

// TripPoint is one point of a vehicle's historical track.
type TripPoint struct {
	Lat      *float64  `json:"lat"`
	Lon      *float64  `json:"lon"`
	Speed    *float64  `json:"speed"`
	Bearing  *float64  `json:"bearing"`
	Ignition *bool     `json:"ignition"`
	Time     time.Time `json:"time"`
	Address  string    `json:"address"`
	// OverLimit is true when Speed exceeds the requested limit at this point.
	OverLimit bool `json:"over_limit"`
	// IdleMin is how long the vehicle sat at this point's stop (0 while moving).
	IdleMin float64 `json:"idle_min"`
}

// History returns a vehicle's track over a time range (proxied from telemetry).
func (s *Service) History(ctx context.Context, tenantID, vehicleID int64, from, to time.Time, maxPoints int) ([]TripPoint, error) {
	var imei, camera string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		row, err := store.New(tx).GetLiveViewVehicle(ctx, vehicleID)
		if err != nil {
			return err
		}
		if row.TrackerImei != nil {
			imei = *row.TrackerImei
		}
		camera = row.CameraDeviceID
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Source rule: a tracker's GPS wins; a vehicle with only a camera falls back
	// to the dashcam's own GPS; a vehicle with neither has no track (empty, not a
	// 404 from an empty-IMEI proxy call).
	if imei == "" {
		if camera == "" {
			return []TripPoint{}, nil
		}
		return s.cameraHistory(ctx, camera, from, to, maxPoints)
	}
	ps, err := s.tel.History(ctx, imei, from, to, historyFields, maxPoints)
	if err != nil {
		return nil, err
	}
	// GPS quality filter: drop physically impossible fixes before any analysis.
	ps = telemetry.FilterTeleports(ps)
	out := make([]TripPoint, 0, len(ps))
	for _, p := range ps {
		addr := ""
		for _, part := range []string{p.Jalan, p.Kecamatan, p.Kota, p.Provinsi} {
			if part != "" {
				if addr != "" {
					addr += ", "
				}
				addr += part
			}
		}
		out = append(out, TripPoint{Lat: p.Lat, Lon: p.Lon, Speed: p.Speed, Bearing: p.Bearing, Ignition: p.Ignition, Time: p.Time, Address: addr})
	}
	return out, nil
}

// cameraHistory builds a trip track from a dashcam's own GPS, for a vehicle with
// no tracker. It is thinner than a tracker's: lat/lon/time only — no speed,
// bearing, ignition or reverse-geocoded address — so the speed/idle overlays
// simply have nothing to colour. Downsampled to maxPoints the same way the GPS
// history endpoint caps its output.
func (s *Service) cameraHistory(ctx context.Context, deviceID string, from, to time.Time, maxPoints int) ([]TripPoint, error) {
	pts, err := s.tel.DashcamTrack(ctx, deviceID, from, to)
	if err != nil {
		return nil, err
	}
	stride := 1
	if maxPoints > 0 && len(pts) > maxPoints {
		stride = (len(pts) + maxPoints - 1) / maxPoints
	}
	out := make([]TripPoint, 0, len(pts)/stride+1)
	for i := 0; i < len(pts); i += stride {
		lat, lon := pts[i].Lat, pts[i].Lon
		out = append(out, TripPoint{Lat: &lat, Lon: &lon, Time: pts[i].Time})
	}
	return out, nil
}

// history godoc
//
//	@Summary	Vehicle historical track (trip history)
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path		int		true	"vehicle id"
//	@Param		from		query		string	true	"RFC3339"
//	@Param		to			query		string	true	"RFC3339"
//	@Param		max_points	query		int		false	"cap on returned points"
//	@Param		limit		query		number	false	"overspeed limit km/h for the over_limit flag (default 60)"
//	@Success	200	{object}	httpx.List[liveview.TripPoint]
//	@Router		/v1/vehicles/{vehicle_id}/history [get]
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
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
	maxPoints := 2000
	if v, e := strconv.Atoi(q.Get("max_points")); e == nil && v > 0 {
		maxPoints = v
	}
	limit := 60.0
	if v := q.Get("limit"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			limit = f
		}
	}
	points, err := h.svc.History(r.Context(), auth.TenantID(r.Context()), id, from, to, maxPoints)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "history unavailable: "+err.Error())
		return
	}
	// Annotate each point with an overspeed flag and its stop dwell.
	for _, c := range stopClusters(points) {
		if c.EndIdx > c.StartIdx && c.DwellMin > 0 {
			d := math.Round(c.DwellMin*10) / 10
			for k := c.StartIdx; k <= c.EndIdx; k++ {
				points[k].IdleMin = d
			}
		}
	}
	for i := range points {
		if points[i].Speed != nil && *points[i].Speed > limit {
			points[i].OverLimit = true
		}
	}
	httpx.JSON(w, http.StatusOK, httpx.List[TripPoint]{Items: points, Total: int64(len(points))})
}

// list godoc
//
//	@Summary	Live map feed: active vehicles + their latest positions
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		fleet_group_id	query		int	false	"filter fleet group"
//	@Success	200	{object}	httpx.List[liveview.VehicleLive]
//	@Router		/v1/live-view [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var fg *int64
	if v := r.URL.Query().Get("fleet_group_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			fg = &id
		}
	}
	items, err := h.svc.List(r.Context(), auth.TenantID(r.Context()), fg)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[VehicleLive]{Items: items, Total: int64(len(items))})
}

// one godoc
//
//	@Summary	Live detail for a single vehicle
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path		int	true	"vehicle id"
//	@Success	200	{object}	liveview.VehicleLive
//	@Failure	404	{object}	map[string]string
//	@Router		/v1/live-view/{vehicle_id} [get]
func (h *Handler) one(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	v, err := h.svc.One(r.Context(), auth.TenantID(r.Context()), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "vehicle not found")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}
