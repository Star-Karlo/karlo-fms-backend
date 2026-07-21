// Package report is the report subsystem: a template catalog plus runnable
// report generators (Vehicle Trip History via the telemetry API, and Vehicle
// Alerts History from the shared alert model).
package report

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Meta describes a report template in the catalog.
type Meta struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"` // Vehicle | Driver
	SubTopic    string `json:"sub_topic"`
	Runnable    bool   `json:"runnable"` // false = catalog placeholder
}

// Catalog is the report template catalog (representative subset of the ~29).
var Catalog = []Meta{
	{"vehicle-trip-history", "Vehicle Trip History", "Enriched GPS pings for a vehicle over a period.", "Vehicle", "Trip", true},
	{"vehicle-trip-summary", "Vehicle Trip Summary", "Per-trip distance, duration and stops.", "Vehicle", "Trip", true},
	{"vehicle-usage-summary", "Vehicle Usage Summary", "Daily utilization per vehicle.", "Vehicle", "Trip", true},
	{"vehicle-stop-history", "Vehicle Stop History", "Where and how long vehicles stopped.", "Vehicle", "Trip", true},
	{"gps-connection-history", "GPS Offline/Connection History", "Device online/offline transitions.", "Vehicle", "Trip", true},
	{"geofence-inout", "Geolocation Entry/Exit", "Geofence enter/exit and dwell time.", "Vehicle", "Geolocation", true},
	{"vehicle-alerts-history", "Vehicle Alerts History", "All safety/telematics alerts over a period.", "Vehicle", "Alert", true},
	{"vehicle-alerts-summary", "Vehicle Alerts Summary", "Alert counts by type and vehicle.", "Vehicle", "Alert", true},
	{"speeding-violations", "Speeding Violations", "Overspeed events with actual vs limit.", "Vehicle", "Alert", true},
	{"harsh-driving", "Harsh Driving Events", "Sudden acceleration / braking from GPS speed deltas.", "Vehicle", "Alert", true},
	{"service-scheduling-detail", "Service Scheduling Detail", "Upcoming and overdue services.", "Vehicle", "Service", true},
	{"fuel-history", "Fuel History", "Fuel level and refuel/theft events.", "Vehicle", "Fuel", true},
	{"temperature-history", "Temperature History", "Cold-chain temperature over time.", "Vehicle", "Temperature", true},
	{"door-open-duration", "Door Open Duration", "Door-open events and durations.", "Vehicle", "Door", true},
	{"pto-active-duration", "PTO Active Duration", "Power-take-off active periods.", "Vehicle", "PTO", true},
	{"driver-management", "Driver Management", "Driver roster and assignments.", "Driver", "Driver", true},
	{"driver-operational-summary", "Driver Operational Summary", "Per-driver trips, distance and behavior.", "Driver", "Driver", true},

	// Fleet-wide historical reports, from the daily telemetry rollup. Everything
	// above that is trip-shaped covers ONE vehicle; these cover all of them,
	// which is what a weekly or monthly report actually needs.
	{"fleet-daily-summary", "Fleet Daily Summary", "Day by day: hours moving and idling, distance, and how much of the fleet worked. Fleet-wide.", "Fleet", "Performance", true},
	{"vehicle-utilisation", "Vehicle Utilisation", "Per vehicle over the period: hours moving and idling, distance, days used. Fleet-wide.", "Fleet", "Performance", true},
	{"idle-leaderboard", "Worst Idlers", "Vehicles that spent the largest share of engine-on time idling. Fleet-wide.", "Fleet", "Performance", true},
	{"unused-vehicles", "Unused Vehicles", "Vehicles that reported but never moved in the period. Fleet-wide.", "Fleet", "Performance", true},
}

// TripRow is one row of the Vehicle Trip History report.
type TripRow struct {
	Date     time.Time `json:"date"`
	Plate    string    `json:"license_plate"`
	Type     *string   `json:"vehicle_type"`
	IMEI     string    `json:"imei"`
	Driver   *string   `json:"driver"`
	Lat      *float64  `json:"lat"`
	Lon      *float64  `json:"lon"`
	Location string    `json:"location"`
	Speed    *float64  `json:"speed"`
	Ignition *bool     `json:"acc"`
}

// TripRequest parameters the trip-history report.
type TripRequest struct {
	VehicleID   int64  `json:"vehicle_id"`
	From        string `json:"from"`
	To          string `json:"to"`
	IntervalMin int    `json:"interval_min"`
}

// Service runs reports.
type Service struct {
	pool *db.Pool
	tel  *telemetry.Client
	// fleet supplies the daily rollup joined to the vehicle registry. Optional.
	fleet *liveview.Service
}

// NewService builds the report service.
func NewService(pool *db.Pool, tel *telemetry.Client) *Service {
	return &Service{pool: pool, tel: tel}
}

// Handler exposes report endpoints.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts report endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/reports/catalog", h.catalog)
	r.Post("/v1/reports/vehicle-trip-history", h.tripHistory)
	r.Post("/v1/reports/vehicle-alerts-history", h.alertsHistory)
	// Generic runner for every catalog report (columns + rows).
	r.Post("/v1/reports/{key}/run", h.run)
}

// catalog godoc
//
//	@Summary	Report template catalog
//	@Tags		reports
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	report.Meta
//	@Router		/v1/reports/catalog [get]
func (h *Handler) catalog(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, Catalog)
}

// tripHistory godoc
//
//	@Summary	Generate a Vehicle Trip History report
//	@Tags		reports
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		report.TripRequest	true	"parameters"
//	@Success	200		{object}	httpx.List[report.TripRow]
//	@Failure	400		{object}	map[string]string
//	@Router		/v1/reports/vehicle-trip-history [post]
func (h *Handler) tripHistory(w http.ResponseWriter, r *http.Request) {
	var req TripRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.VehicleID == 0 {
		httpx.Error(w, http.StatusBadRequest, "vehicle_id, from, to are required")
		return
	}
	from, err1 := time.Parse(time.RFC3339, req.From)
	to, err2 := time.Parse(time.RFC3339, req.To)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}

	// Resolve the vehicle's IMEI + labels.
	tid := auth.TenantID(r.Context())
	var veh store.GetVehicleRow
	if err := db.Tx(r.Context(), h.svc.pool, tid, func(tx pgx.Tx) error {
		v, err := store.New(tx).GetVehicle(r.Context(), req.VehicleID)
		veh = v
		return err
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "vehicle not found")
		return
	}
	if veh.TrackerImei == nil || *veh.TrackerImei == "" {
		httpx.Error(w, http.StatusBadRequest, "vehicle has no tracker/IMEI")
		return
	}

	maxPoints := 5000
	positions, err := h.svc.tel.History(r.Context(), *veh.TrackerImei, from, to, nil, maxPoints)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "telemetry history unavailable: "+err.Error())
		return
	}

	// Optional interval thinning (keep one point per interval).
	rows := make([]TripRow, 0, len(positions))
	var lastKept time.Time
	interval := time.Duration(req.IntervalMin) * time.Minute
	for _, p := range positions {
		if interval > 0 && !lastKept.IsZero() && p.Time.Sub(lastKept) < interval {
			continue
		}
		lastKept = p.Time
		rows = append(rows, TripRow{
			Date: p.Time, Plate: veh.LicensePlate, Type: veh.VehicleType, IMEI: p.IMEI,
			Driver: veh.DriverName, Lat: p.Lat, Lon: p.Lon,
			Location: joinLoc(p), Speed: p.Speed, Ignition: p.Ignition,
		})
	}
	httpx.JSON(w, http.StatusOK, httpx.List[TripRow]{Items: rows, Total: int64(len(rows))})
}

func joinLoc(p telemetry.Position) string {
	parts := []string{}
	for _, s := range []string{p.Jalan, p.Kelurahan, p.Kecamatan, p.Kota, p.Provinsi} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// AlertRow is one row of the Vehicle Alerts History report.
type AlertRow struct {
	Date        time.Time `json:"date"`
	Plate       *string   `json:"license_plate"`
	AlertCode   *string   `json:"alert_code"`
	AlertName   *string   `json:"alert_name"`
	Severity    *string   `json:"severity"`
	Actual      *float64  `json:"actual"`
	Limit       *float64  `json:"limit"`
	Address     *string   `json:"address"`
	CheckStatus string    `json:"check_status"`
}

// alertsHistory godoc
//
//	@Summary	Generate a Vehicle Alerts History report
//	@Tags		reports
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		from	query	string	false	"RFC3339"
//	@Param		to		query	string	false	"RFC3339"
//	@Success	200		{object}	httpx.List[report.AlertRow]
//	@Router		/v1/reports/vehicle-alerts-history [post]
func (h *Handler) alertsHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var from, to pgtype.Timestamptz
	if t, err := time.Parse(time.RFC3339, q.Get("from")); err == nil {
		from = pgtype.Timestamptz{Time: t, Valid: true}
	}
	if t, err := time.Parse(time.RFC3339, q.Get("to")); err == nil {
		to = pgtype.Timestamptz{Time: t, Valid: true}
	}
	tid := auth.TenantID(r.Context())
	var rows []AlertRow
	err := db.Tx(r.Context(), h.svc.pool, tid, func(tx pgx.Tx) error {
		list, err := store.New(tx).ListAlerts(r.Context(), store.ListAlertsParams{
			CheckStatus: conv.StrPtr(q.Get("check_status")), DeviceSource: conv.StrPtr(q.Get("device_source")),
			AlertCode: conv.StrPtr(q.Get("alert_code")), From: from, To: to, Lim: 10000, Off: 0,
		})
		if err != nil {
			return err
		}
		rows = make([]AlertRow, 0, len(list))
		for _, a := range list {
			rows = append(rows, AlertRow{
				Date: a.OccurredAt.Time, Plate: a.LicensePlate, AlertCode: a.AlertCode, AlertName: a.AlertName,
				Severity: a.Severity, Actual: conv.NumF(a.ActualValue), Limit: conv.NumF(a.LimitValue),
				Address: a.Address, CheckStatus: a.CheckStatus,
			})
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[AlertRow]{Items: rows, Total: int64(len(rows))})
}
