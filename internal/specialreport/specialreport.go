// Package specialreport serves fixed-format reports for outside systems that
// expect a legacy response shape rather than this API's own conventions.
//
// The first one, driver-truck-distance, reproduces the old GPS platform's
// "truck last location vs driver last location" feed field for field, so a
// consumer built against that feed (keyed by master-data ObjectIds and nomor
// polisi) can switch to FMS without changing its parser.
package specialreport

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Battery thresholds for the tracker's external supply, in volts. Inferred
// from the legacy feed this report replaces: every reading under 8 V was
// flagged BATT_LOW and every reading under 24.5 V BATT_WARNING. The fleet is
// 24 V trucks; a 12 V vehicle would read as permanently BATT_WARNING.
const (
	battLowV     = 8.0
	battWarningV = 24.5
)

// Response is the report envelope: the legacy feed always answered
// {"message":"success","data":[...]}.
type Response struct {
	Message string `json:"message" example:"success"`
	Data    []Row  `json:"data"`
}

// Row is one truck.
type Row struct {
	// ID is the truck's master-data ObjectId. Null for a vehicle the
	// master-data projection has not paired yet.
	ID           *string `json:"_id" example:"60192bfd28e7a4430e73d8ee"`
	PoliceNumber string  `json:"policeNumber" example:"H-9070-OH"`
	// LastLocation is the tracker's latest fix — or the assigned driver's
	// phone fix when that is newer than anything the tracker has sent (the
	// legacy rule; the distance is then 0). Null when neither has a fix.
	LastLocation *TruckLocation `json:"lastLocation"`
	// Driver is the assigned driver's master-data ObjectId. Absent when the
	// truck has no driver assigned, as in the legacy feed.
	Driver             string          `json:"driver,omitempty" example:"66debc0562e3db0b259024ee"`
	DriverLastLocation *DriverLocation `json:"driverLastLocation"`
	// DistanceDriverToTruck is the straight-line distance in metres between
	// lastLocation and driverLastLocation. Null unless both are present.
	DistanceDriverToTruck *float64 `json:"distanceDriverToTruck" example:"14528.75"`
}

// TruckLocation is the truck's position as the legacy feed described it.
// When the position came from the driver's phone, the tracker-only fields
// (status, gpsError, errorDesc) are absent and batteryLevel is the phone's
// percentage, exactly as the legacy feed did.
type TruckLocation struct {
	Latitude     float64   `json:"latitude" example:"-6.3676866"`
	Longitude    float64   `json:"longitude" example:"107.3623933"`
	Altitude     *float64  `json:"altitude,omitempty" example:"49"`
	Speed        float64   `json:"speed" example:"0"`
	Bearing      float64   `json:"bearing" example:"280"`
	BatteryLevel *float64  `json:"batteryLevel,omitempty" example:"25.575"` // tracker: external supply in volts; phone: percent
	Status       string    `json:"status,omitempty" example:"Stop"`         // Moving | Idle | Stop (tracker only)
	GPSError     *bool     `json:"gpsError,omitempty" example:"false"`
	ErrorDesc    []string  `json:"errorDesc" example:"NORMAL"`                   // NORMAL | BATT_WARNING | BATT_LOW, plus LOST_GPS
	UpdatedAt    time.Time `json:"updatedAt" example:"2026-11-24T02:17:24.264Z"` // when the position was received
	// LastGPS is the tracker's own latest fix time, even when the position
	// shown is the phone's. Null when the tracker has never reported.
	LastGPS *time.Time `json:"lastGPS" example:"2026-11-24T02:17:24.264Z"`
}

// DriverLocation is the driver's latest K-Trip phone fix.
type DriverLocation struct {
	Latitude     float64   `json:"latitude" example:"-6.4258087"`
	Longitude    float64   `json:"longitude" example:"107.4801472"`
	Altitude     *float64  `json:"altitude,omitempty" example:"91.2"`
	Speed        float64   `json:"speed" example:"0"`
	Bearing      float64   `json:"bearing" example:"0"`
	BatteryLevel *float64  `json:"batteryLevel,omitempty" example:"27"` // phone battery, percent
	GPSOn        *bool     `json:"gpsOn,omitempty" example:"true"`
	Status       string    `json:"status,omitempty" example:"Idle"`
	UpdatedAt    time.Time `json:"updatedAt" example:"2026-11-21T22:28:07.241Z"`
}

// Service builds special reports.
type Service struct {
	pool *db.Pool
	tel  *telemetry.Client
}

// NewService builds the service.
func NewService(pool *db.Pool, tel *telemetry.Client) *Service {
	return &Service{pool: pool, tel: tel}
}

// truck is one registry row, before positions are attached.
type truck struct {
	masterDataID *string
	plate        string
	imei         string
	driverID     string
}

// DriverTruckDistance returns every non-archived vehicle of the tenant with
// its truck and driver positions. Telemetry failures degrade to null
// positions rather than failing the report, like the live map.
func (s *Service) DriverTruckDistance(ctx context.Context, tenantID int64) ([]Row, error) {
	var trucks []truck
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		// Every vehicle, including those with no tracker and no driver: the
		// legacy feed listed them with null positions, and a consumer
		// reconciling its own truck list needs them present.
		rows, err := tx.Query(ctx, `
			SELECT v.master_data_id, v.license_plate,
			       COALESCE(t.imei, ''), COALESCE(e.master_data_id, '')
			FROM vehicles v
			LEFT JOIN trackers t  ON t.id = v.tracker_id
			LEFT JOIN employees e ON e.id = v.current_driver_id
			WHERE v.status <> 'archive'
			ORDER BY v.master_data_id NULLS LAST, v.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t truck
			if err := rows.Scan(&t.masterDataID, &t.plate, &t.imei, &t.driverID); err != nil {
				return err
			}
			trucks = append(trucks, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	var imeis, drivers []string
	for _, t := range trucks {
		if t.imei != "" {
			imeis = append(imeis, t.imei)
		}
		if t.driverID != "" {
			drivers = append(drivers, t.driverID)
		}
	}

	byIMEI := map[string]*telemetry.Position{}
	if len(imeis) > 0 {
		// All fields: battery and altitude live in metadata.
		ps, err := s.tel.LiveMany(ctx, imeis, nil)
		if err != nil {
			log.Printf("specialreport: tracker positions unavailable: %v", err)
		}
		for i := range ps {
			byIMEI[ps[i].IMEI] = &ps[i]
		}
	}
	byDriver := map[string]*telemetry.MobilePosition{}
	if len(drivers) > 0 {
		ps, err := s.tel.MobileLiveMany(ctx, drivers)
		if err != nil {
			log.Printf("specialreport: phone positions unavailable: %v", err)
		}
		for i := range ps {
			byDriver[ps[i].Driver] = &ps[i]
		}
	}

	out := make([]Row, 0, len(trucks))
	for _, t := range trucks {
		var gps *telemetry.Position
		if t.imei != "" {
			gps = byIMEI[t.imei]
		}
		var phone *telemetry.MobilePosition
		if t.driverID != "" {
			phone = byDriver[t.driverID]
		}
		out = append(out, buildRow(t, gps, phone))
	}
	return out, nil
}

// buildRow applies the legacy feed's rules to one truck.
func buildRow(t truck, gps *telemetry.Position, phone *telemetry.MobilePosition) Row {
	r := Row{ID: t.masterDataID, PoliceNumber: t.plate, Driver: t.driverID}

	trackerLoc := truckFromTracker(gps)
	driverLoc := driverFromPhone(phone)
	r.DriverLastLocation = driverLoc
	r.LastLocation = trackerLoc

	// The phone stands in for the truck when it is newer than the tracker's
	// last fix (or the tracker has none). lastGPS keeps the tracker's time so
	// a consumer can still see the tracker went quiet.
	if driverLoc != nil && (trackerLoc == nil || driverLoc.UpdatedAt.After(trackerTime(gps))) {
		r.LastLocation = truckFromPhone(driverLoc, gps)
	}

	if r.LastLocation != nil && driverLoc != nil {
		d := haversineM(r.LastLocation.Latitude, r.LastLocation.Longitude, driverLoc.Latitude, driverLoc.Longitude)
		r.DistanceDriverToTruck = &d
	}
	return r
}

// trackerTime is the tracker's latest fix time, zero when there is none.
func trackerTime(p *telemetry.Position) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.Time
}

func truckFromTracker(p *telemetry.Position) *TruckLocation {
	if p == nil || p.Lat == nil || p.Lon == nil {
		return nil
	}
	meta := parseMeta(p.Metadata)
	fix := p.Time
	loc := &TruckLocation{
		Latitude: *p.Lat, Longitude: *p.Lon,
		Altitude:  metaNum(meta, "altitude"),
		Speed:     deref(p.Speed),
		Bearing:   deref(p.Bearing),
		Status:    status(p.Speed, p.Ignition),
		GPSError:  ptr(false),
		ErrorDesc: []string{},
		UpdatedAt: p.Time,
		LastGPS:   &fix,
	}
	if p.ReceivedAt != nil {
		loc.UpdatedAt = *p.ReceivedAt
	}
	if mv := metaNum(meta, "External Voltage"); mv != nil {
		v := math.Round(*mv) / 1000 // mV -> V, 3 dp
		loc.BatteryLevel = &v
		loc.ErrorDesc = append(loc.ErrorDesc, batteryFlag(v))
	}
	if loc.Latitude == 0 && loc.Longitude == 0 {
		loc.ErrorDesc = append(loc.ErrorDesc, "LOST_GPS")
	}
	return loc
}

func driverFromPhone(p *telemetry.MobilePosition) *DriverLocation {
	if p == nil || p.Lat == nil || p.Lon == nil {
		return nil
	}
	meta := parseMeta(p.Metadata)
	loc := &DriverLocation{
		Latitude: *p.Lat, Longitude: *p.Lon,
		Altitude:     metaNum(meta, "altitude"),
		Speed:        deref(p.Speed),
		Bearing:      deref(p.Bearing),
		BatteryLevel: metaNum(meta, "batteryLevel"),
		GPSOn:        metaBool(meta, "gpsOn"),
		UpdatedAt:    p.Time,
	}
	if s, ok := meta["status"].(string); ok {
		loc.Status = s
	}
	return loc
}

// truckFromPhone presents the driver's phone fix as the truck's location.
func truckFromPhone(d *DriverLocation, gps *telemetry.Position) *TruckLocation {
	loc := &TruckLocation{
		Latitude: d.Latitude, Longitude: d.Longitude,
		Altitude: d.Altitude, Speed: d.Speed, Bearing: d.Bearing,
		BatteryLevel: d.BatteryLevel,
		ErrorDesc:    []string{},
		UpdatedAt:    d.UpdatedAt,
	}
	if gps != nil && !gps.Time.IsZero() {
		t := gps.Time
		loc.LastGPS = &t
	}
	return loc
}

// status is the legacy drive state: any speed is Moving, engine on at rest is
// Idle, everything else Stop.
func status(speed *float64, ignition *bool) string {
	switch {
	case speed != nil && *speed > 0:
		return "Moving"
	case ignition != nil && *ignition:
		return "Idle"
	default:
		return "Stop"
	}
}

func batteryFlag(volts float64) string {
	switch {
	case volts < battLowV:
		return "BATT_LOW"
	case volts < battWarningV:
		return "BATT_WARNING"
	default:
		return "NORMAL"
	}
}

// haversineM is the great-circle distance in metres. R is the IUGG mean Earth
// radius, 6371008.8 m — the value the legacy feed used (with 6371000 m its
// 14.5 km distances came out 2 cm short).
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371008.8
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func parseMeta(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m) // malformed metadata just means no extras
	}
	return m
}

func metaNum(m map[string]any, key string) *float64 {
	switch v := m[key].(type) {
	case float64:
		return &v
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return &f
		}
	}
	return nil
}

func metaBool(m map[string]any, key string) *bool {
	if v, ok := m[key].(bool); ok {
		return &v
	}
	return nil
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func ptr[T any](v T) *T { return &v }

// Handler exposes the special reports.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the special reports. Mount in the integration group: the
// consumers are outside systems holding a per-tenant API key, and the group
// also accepts a signed-in user so the call can be tried from Swagger.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/special-reports/driver-truck-distance", h.driverTruckDistance)
}

// driverTruckDistance godoc
//
//	@Summary		Truck vs driver last location (legacy feed format)
//	@Description	Every non-archived truck of the caller's account with its tracker's last location, its assigned driver's last K-Trip phone location, and the straight-line distance between them in metres. The response shape matches the old GPS platform's feed field for field, so an existing consumer can switch without changing its parser.
//	@Description
//	@Description	Rules carried over from that feed: `_id` and `driver` are master-data ObjectIds; `lastLocation` falls back to the driver's phone fix when it is newer than the tracker's last fix (distance is then 0, and `lastGPS` still shows the tracker's own last fix time); tracker `batteryLevel` is the external supply in volts, flagged in `errorDesc` as BATT_LOW under 8 V and BATT_WARNING under 24.5 V; LOST_GPS is added for a 0,0 fix. Positions are null when telemetry has none (or is unreachable).
//	@Description
//	@Description	Authenticate with the account's API key (`Authorization: Bearer fms_...` or `X-Api-Key: fms_...`) or a signed-in session.
//	@Tags			special-report
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	specialreport.Response
//	@Failure		401	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/v1/special-reports/driver-truck-distance [get]
func (h *Handler) driverTruckDistance(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.DriverTruckDistance(r.Context(), auth.TenantID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "report failed")
		return
	}
	httpx.JSON(w, http.StatusOK, Response{Message: "success", Data: rows})
}
