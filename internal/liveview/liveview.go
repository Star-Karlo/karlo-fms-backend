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
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// liveFields trims the upstream payload to what the map needs (drops metadata).
var liveFields = []string{"lat", "lon", "speed", "bearing", "ignition", "kecamatan", "kabupaten", "kota", "provinsi", "jalan"}

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
		out = append(out, VehicleLive{
			VehicleID: r.VehicleID, LicensePlate: r.LicensePlate, RegistryStatus: r.Status,
			FleetGroupName: r.FleetGroupName,
			Driver:         Driver{ID: r.DriverID, Name: r.DriverName, Phone: r.DriverPhone},
			Tracker:        Tracker{ID: r.TrackerID, IMEI: r.TrackerImei},
			DriveState:     StateOffline,
		})
		imeis = append(imeis, r.TrackerImei)
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
	v := VehicleLive{
		VehicleID: row.VehicleID, LicensePlate: row.LicensePlate, RegistryStatus: row.Status,
		FleetGroupName: row.FleetGroupName,
		Driver:         Driver{ID: row.DriverID, Name: row.DriverName, Phone: row.DriverPhone},
		Tracker:        Tracker{ID: row.TrackerID, IMEI: row.TrackerImei},
		DriveState:     StateOffline,
	}
	var pos *telemetry.Position
	if row.TrackerImei != "" {
		if p, err := s.tel.LiveOne(ctx, row.TrackerImei); err != nil {
			log.Printf("liveview: telemetry unavailable for %s: %v", row.TrackerImei, err)
		} else {
			pos = p
		}
	}
	enrich(&v, pos, time.Now())
	return &v, nil
}

// Handler exposes the live-view endpoints.
type Handler struct{ svc *Service }

// NewHandler builds the live-view HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the live-view endpoints (any authenticated user).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/live-view", h.list)
	r.Get("/v1/live-view/{vehicle_id}", h.one)
	r.Get("/v1/vehicles/{vehicle_id}/history", h.history)
	r.Get("/v1/analytics/fleet", h.fleetAnalytics)
	r.Get("/v1/vehicles/{vehicle_id}/behavior", h.behavior)
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
}

// BehaviorResult is the per-vehicle behavior analysis.
type BehaviorResult struct {
	OverspeedLimit float64         `json:"overspeed_limit"`
	Summary        BehaviorSummary `json:"summary"`
	Events         []BehaviorEvent `json:"events"`
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

// behavior godoc
//
//	@Summary	Per-vehicle driving behavior (overspeed + harsh accel/brake) from GPS
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path		int		true	"vehicle id"
//	@Param		from		query		string	true	"RFC3339"
//	@Param		to			query		string	true	"RFC3339"
//	@Param		limit		query		number	false	"overspeed limit km/h (default 80)"
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
	limit := 80.0
	if v := q.Get("limit"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			limit = f
		}
	}

	pts, err := h.svc.History(r.Context(), auth.TenantID(r.Context()), id, from, to, 5000)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "history unavailable: "+err.Error())
		return
	}

	// Harsh thresholds (km/h per second). Only trusted when samples are close in
	// time — GPS history is downsampled, so far-apart points are skipped.
	const harshAccel, harshBrake, maxGapSec = 10.0, -12.0, 30.0
	res := BehaviorResult{OverspeedLimit: limit, Events: []BehaviorEvent{}}
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
	httpx.JSON(w, http.StatusOK, res)
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
}

// History returns a vehicle's track over a time range (proxied from telemetry).
func (s *Service) History(ctx context.Context, tenantID, vehicleID int64, from, to time.Time, maxPoints int) ([]TripPoint, error) {
	var imei string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		row, err := store.New(tx).GetLiveViewVehicle(ctx, vehicleID)
		if err != nil {
			return err
		}
		imei = row.TrackerImei
		return nil
	})
	if err != nil {
		return nil, err
	}
	ps, err := s.tel.History(ctx, imei, from, to, nil, maxPoints)
	if err != nil {
		return nil, err
	}
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
	points, err := h.svc.History(r.Context(), auth.TenantID(r.Context()), id, from, to, maxPoints)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "history unavailable: "+err.Error())
		return
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
