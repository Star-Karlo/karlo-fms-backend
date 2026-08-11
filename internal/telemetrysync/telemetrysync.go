// Package telemetrysync is the background telemetry collector. Every tick it
// pulls the live snapshot for each tenant's tracked fleet and derives the
// metrics that need continuous observation rather than on-demand queries:
//
//   - engine hours   — accumulates ignition-on time into vehicle_telemetry_state
//     and mirrors it to vehicles.hourmeter (hours)
//   - odometer sync  — copies telemetry "Total Odometer" into vehicles.odometer
//     so km-based maintenance schedules advance by themselves
//   - battery tamper — alerts on a sudden external-voltage drop or disconnect
//   - geofence in/out — for vehicles attached to active GEOFENCE_ENTER/EXIT
//     rules, detects circle-geofence transitions and writes alerts
//
// State lives in vehicle_telemetry_state (one row per tenant+vehicle), so the
// accumulators survive restarts and transitions are edge-triggered.
package telemetrysync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// liveFields trims the live payload to what the collector derives from.
var liveFields = []string{"lat", "lon", "speed", "ignition", "metadata"}

// geofence transition alerts share the notifengine cooldown idea but shorter —
// transitions are edge-triggered already; this only suppresses boundary flap.
const geofenceCooldownMin = 5

// batteryDropRatio: alert when voltage falls below this fraction of the last
// healthy (>= 8V) reading — covers both sagging batteries and cut wires.
const batteryDropRatio = 0.7

// minValidMv: External Voltage readings below this (millivolts) are treated as
// spurious device glitches (a transmitting tracker is never truly at ~0V), not
// real measurements — ignored so a single 0V blip can't fire a false alert.
const minValidMv = 6000

// Collector runs the periodic sync.
type Collector struct {
	pool     *db.Pool
	tel      *telemetry.Client
	interval time.Duration
}

// New builds a collector. interval ~2m is plenty for hour/odometer granularity.
func New(pool *db.Pool, tel *telemetry.Client, interval time.Duration) *Collector {
	return &Collector{pool: pool, tel: tel, interval: interval}
}

// Run ticks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	log.Printf("telemetrysync: started (interval=%s)", c.interval)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		if err := c.tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telemetrysync: tick error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Collector) tick(ctx context.Context) error {
	var tenantIDs []int64
	if err := db.SystemTx(ctx, c.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM tenants`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			tenantIDs = append(tenantIDs, id)
		}
		return rows.Err()
	}); err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	for _, tid := range tenantIDs {
		if err := c.syncTenant(ctx, tid); err != nil {
			log.Printf("telemetrysync: tenant %d: %v", tid, err)
		}
	}
	return nil
}

type vehState struct {
	engineSec    int64
	lastIgnition *bool
	lastOdoKm    *float64
	lastMv       *float64
	lastGsm      *float64
	lowMvStreak  int // consecutive low External Voltage samples (debounce glitches)
	lastSampleAt *time.Time
	inside       map[int64]bool
}

type circleGf struct {
	id       int64
	name     string
	lat, lon float64
	radiusM  float64
}

func (c *Collector) syncTenant(ctx context.Context, tenantID int64) error {
	vehByIMEI := map[string]int64{}      // imei -> vehicle id
	states := map[int64]*vehState{}      // vehicle id -> accumulator state
	gfByID := map[int64]circleGf{}       // circle geofences by id
	assigned := map[int64][]int64{}      // vehicle id -> assigned geofence ids
	prevOdo := map[int64]*float64{}      // vehicle id -> vehicles.odometer

	if err := db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT v.id, t.imei, v.odometer::float8
			FROM vehicles v JOIN trackers t ON t.id = v.tracker_id
			WHERE t.imei IS NOT NULL AND t.imei <> ''`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var imei string
			var odo *float64
			if err := rows.Scan(&id, &imei, &odo); err != nil {
				rows.Close()
				return err
			}
			vehByIMEI[imei] = id
			prevOdo[id] = odo
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}

		rows, err = tx.Query(ctx, `
			SELECT vehicle_id, engine_hours_seconds, last_ignition, last_odometer_km::float8,
			       last_voltage_mv::float8, last_gsm_signal::float8, last_sample_at, inside_geofences
			FROM vehicle_telemetry_state`)
		if err != nil {
			return err
		}
		for rows.Next() {
			st := &vehState{inside: map[int64]bool{}}
			var vid int64
			var insideRaw []byte
			if err := rows.Scan(&vid, &st.engineSec, &st.lastIgnition, &st.lastOdoKm, &st.lastMv, &st.lastGsm, &st.lastSampleAt, &insideRaw); err != nil {
				rows.Close()
				return err
			}
			var ids []int64
			_ = json.Unmarshal(insideRaw, &ids)
			for _, id := range ids {
				st.inside[id] = true
			}
			states[vid] = st
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}

		// Circle geofences (polygon support can come later; the UI creates circles).
		rows, err = tx.Query(ctx, `
			SELECT id, name, ST_Y(center::geometry)::float8, ST_X(center::geometry)::float8, radius_m::float8
			FROM geofences WHERE center IS NOT NULL AND radius_m IS NOT NULL AND radius_m > 0`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var g circleGf
			if err := rows.Scan(&g.id, &g.name, &g.lat, &g.lon, &g.radiusM); err != nil {
				rows.Close()
				return err
			}
			gfByID[g.id] = g
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}

		return nil
	}); err != nil {
		return err
	}
	if len(vehByIMEI) == 0 {
		return nil
	}

	// Geofence assignments in a SEPARATE tx: if this fails (e.g. migration not
	// applied yet), geofence evaluation is skipped but engine hours / odometer /
	// battery collection still run.
	if err := db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT vehicle_id, geofence_id FROM geofence_vehicles`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var vid, gid int64
			if err := rows.Scan(&vid, &gid); err != nil {
				return err
			}
			assigned[vid] = append(assigned[vid], gid)
		}
		return rows.Err()
	}); err != nil {
		log.Printf("telemetrysync: tenant %d: geofence assignments unavailable (skipping geofence checks): %v", tenantID, err)
		assigned = map[int64][]int64{}
	}

	imeis := make([]string, 0, len(vehByIMEI))
	for im := range vehByIMEI {
		imeis = append(imeis, im)
	}
	positions, err := c.tel.LiveMany(ctx, imeis, liveFields)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}

	now := time.Now()
	maxStep := 3 * c.interval // cap per-tick engine-hour growth across restarts/gaps

	type stateWrite struct {
		vid    int64
		st     *vehState
		odoKm  *float64 // set → also update vehicles.odometer
		hourmt *float64 // set → also update vehicles.hourmeter
		lat    *float64
		lon    *float64
	}
	var writes []stateWrite
	type alertWrite struct {
		vid      int64
		code     string
		name     string
		severity string
		lat, lon *float64
		actual   *float64
		limit    *float64
		cooldown int32
	}
	var alerts []alertWrite

	for _, p := range positions {
		vid, ok := vehByIMEI[p.IMEI]
		if !ok {
			continue
		}
		st := states[vid]
		if st == nil {
			st = &vehState{inside: map[int64]bool{}}
		}
		sampleAt := p.Time
		if sampleAt.IsZero() || sampleAt.After(now) {
			sampleAt = now
		}

		// --- Engine hours: accumulate while ignition stays on between samples.
		if st.lastIgnition != nil && *st.lastIgnition && st.lastSampleAt != nil {
			dt := sampleAt.Sub(*st.lastSampleAt)
			if dt > 0 {
				if dt > maxStep {
					dt = maxStep
				}
				st.engineSec += int64(dt.Seconds())
			}
		}

		meta := parseMeta(p.Metadata)

		// --- Odometer: telemetry "Total Odometer" is meters.
		var odoWrite *float64
		if m := metaNum(meta, "Total Odometer"); m != nil && *m > 0 {
			km := *m / 1000.0
			if st.lastOdoKm == nil || km > *st.lastOdoKm+0.01 {
				st.lastOdoKm = &km
				// Only touch vehicles.odometer when it actually advances.
				if po := prevOdo[vid]; po == nil || km > *po+0.01 {
					odoWrite = &km
				}
			}
		}

		// --- Battery: sudden drop vs the last healthy reading = tamper/dying.
		// These trackers occasionally emit a single spurious 0V (ignition power
		// blip / missing field) then recover to 24-28V, so:
		//   * readings below minValidMv are treated as invalid glitches and
		//     ignored (don't update lastMv, don't alert),
		//   * a real drop must PERSIST for 3 consecutive samples (~6 min) before
		//     it alerts — filtering transient dips.
		if mv := metaNum(meta, "External Voltage"); mv != nil && *mv >= minValidMv {
			isDrop := st.lastMv != nil && *st.lastMv >= 8000 && *mv < *st.lastMv*batteryDropRatio
			if isDrop {
				st.lowMvStreak++
				if st.lowMvStreak >= 3 {
					nowV, wasV := *mv/1000, *st.lastMv/1000
					alerts = append(alerts, alertWrite{
						vid: vid, code: "BATTERY_DROP",
						name:     fmt.Sprintf("Battery drop — %.1fV (was %.1fV)", nowV, wasV),
						severity: "urgent", lat: p.Lat, lon: p.Lon,
						actual: &nowV, limit: &wasV, cooldown: 60,
					})
				}
			} else {
				st.lowMvStreak = 0
			}
			st.lastMv = mv
		}
		if g := metaNum(meta, "GSM Signal"); g != nil {
			st.lastGsm = g
		}

		// --- Geofence transitions (for the geofences this vehicle is assigned to).
		if gfIDs := assigned[vid]; len(gfIDs) > 0 && p.Lat != nil && p.Lon != nil {
			insideNow := map[int64]bool{}
			for _, gid := range gfIDs {
				g, ok := gfByID[gid]
				if !ok {
					continue
				}
				is := haversineKm(*p.Lat, *p.Lon, g.lat, g.lon)*1000 <= g.radiusM
				if is {
					insideNow[gid] = true
				}
				was := st.inside[gid]
				if is && !was {
					alerts = append(alerts, alertWrite{
						vid: vid, code: "GEOFENCE_ENTER", name: "Entered " + g.name,
						severity: "important", lat: p.Lat, lon: p.Lon, cooldown: geofenceCooldownMin,
					})
				} else if was && !is {
					alerts = append(alerts, alertWrite{
						vid: vid, code: "GEOFENCE_EXIT", name: "Exited " + g.name,
						severity: "important", lat: p.Lat, lon: p.Lon, cooldown: geofenceCooldownMin,
					})
				}
			}
			st.inside = insideNow
		}

		st.lastIgnition = p.Ignition
		st.lastSampleAt = &sampleAt

		hours := float64(st.engineSec) / 3600.0
		writes = append(writes, stateWrite{vid: vid, st: st, odoKm: odoWrite, hourmt: &hours, lat: p.Lat, lon: p.Lon})
	}

	// Persist everything for the tenant in one transaction.
	created := 0
	err = db.Tx(ctx, c.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		for _, w := range writes {
			ids := make([]int64, 0, len(w.st.inside))
			for id := range w.st.inside {
				ids = append(ids, id)
			}
			insideJSON, _ := json.Marshal(ids)
			if _, err := tx.Exec(ctx, `
				INSERT INTO vehicle_telemetry_state
				  (tenant_id, vehicle_id, engine_hours_seconds, last_ignition, last_odometer_km,
				   last_voltage_mv, last_gsm_signal, last_lat, last_lon, last_sample_at, inside_geofences, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, now())
				ON CONFLICT (tenant_id, vehicle_id) DO UPDATE SET
				  engine_hours_seconds = EXCLUDED.engine_hours_seconds,
				  last_ignition        = EXCLUDED.last_ignition,
				  last_odometer_km     = COALESCE(EXCLUDED.last_odometer_km, vehicle_telemetry_state.last_odometer_km),
				  last_voltage_mv      = COALESCE(EXCLUDED.last_voltage_mv, vehicle_telemetry_state.last_voltage_mv),
				  last_gsm_signal      = COALESCE(EXCLUDED.last_gsm_signal, vehicle_telemetry_state.last_gsm_signal),
				  last_lat             = COALESCE(EXCLUDED.last_lat, vehicle_telemetry_state.last_lat),
				  last_lon             = COALESCE(EXCLUDED.last_lon, vehicle_telemetry_state.last_lon),
				  last_sample_at       = EXCLUDED.last_sample_at,
				  inside_geofences     = EXCLUDED.inside_geofences,
				  updated_at           = now()`,
				tenantID, w.vid, w.st.engineSec, w.st.lastIgnition, w.st.lastOdoKm,
				w.st.lastMv, w.st.lastGsm, w.lat, w.lon, w.st.lastSampleAt, insideJSON,
			); err != nil {
				return err
			}
			if w.odoKm != nil || w.hourmt != nil {
				if _, err := tx.Exec(ctx, `
					UPDATE vehicles SET
					  odometer  = COALESCE($2::numeric, odometer),
					  hourmeter = COALESCE($3::numeric, hourmeter),
					  updated_at = now()
					WHERE id = $1 AND (
					  ($2::numeric IS NOT NULL AND ($2::numeric > COALESCE(odometer, 0))) OR
					  ($3::numeric IS NOT NULL AND abs(COALESCE(hourmeter,0) - $3::numeric) > 0.01)
					)`, w.vid, w.odoKm, w.hourmt); err != nil {
					return err
				}
			}
		}
		for _, a := range alerts {
			vid, code, name, sev := a.vid, a.code, a.name, a.severity
			n, err := q.CountRecentAlert(ctx, store.CountRecentAlertParams{
				TenantID: tenantID, VehicleID: &vid, AlertCode: &code, CooldownMin: a.cooldown,
			})
			if err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			if _, err := q.CreateAlert(ctx, store.CreateAlertParams{
				TenantID: tenantID, VehicleID: &vid, AlertCode: &code, AlertName: &name, Severity: &sev,
				ActualValue: conv.Num(a.actual), LimitValue: conv.Num(a.limit), Lat: a.lat, Lon: a.lon,
			}); err != nil {
				return err
			}
			created++
		}
		return nil
	})
	if err == nil && created > 0 {
		log.Printf("telemetrysync: tenant %d: created %d alert(s)", tenantID, created)
	}
	return err
}

// ---- helpers ----

func parseMeta(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func metaNum(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	switch v := m[key].(type) {
	case float64:
		return &v
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// ---- read API ----

// StateResponse is the per-vehicle accumulator snapshot.
type StateResponse struct {
	EngineHours  float64    `json:"engine_hours"`  // cumulative hours (ignition-on)
	OdometerKm   *float64   `json:"odometer_km"`   // latest telemetry odometer
	BatteryV     *float64   `json:"battery_v"`     // latest external voltage (V)
	GsmSignal    *float64   `json:"gsm_signal"`    // latest GSM signal (0-5)
	LastSampleAt *time.Time `json:"last_sample_at"`
	// Maintenance context for the odometer: km driven since the most recent
	// recorded service. Nil when the vehicle has no service records yet.
	LastServiceKm  *float64 `json:"last_service_km"`
	LastServiceAt  *string  `json:"last_service_at"` // YYYY-MM-DD
	KmSinceService *float64 `json:"km_since_service"`
}

// Handler exposes the collector's state.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the state endpoint (inside the authenticated group).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/vehicles/{vehicle_id}/telemetry-state", h.state)
}

// state godoc
//
//	@Summary	Accumulated telemetry state for a vehicle (engine hours, odometer, battery)
//	@Tags		live-view
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	path	int	true	"vehicle id"
//	@Success	200	{object}	telemetrysync.StateResponse
//	@Router		/v1/vehicles/{vehicle_id}/telemetry-state [get]
func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
		return
	}
	var res StateResponse
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var sec int64
		var mv *float64
		err := tx.QueryRow(r.Context(), `
			SELECT engine_hours_seconds, last_odometer_km::float8, last_voltage_mv::float8,
			       last_gsm_signal::float8, last_sample_at
			FROM vehicle_telemetry_state WHERE vehicle_id = $1`, id).
			Scan(&sec, &res.OdometerKm, &mv, &res.GsmSignal, &res.LastSampleAt)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil {
			res.EngineHours = math.Round(float64(sec)/3600*10) / 10
			if mv != nil {
				v := math.Round(*mv/100) / 10
				res.BatteryV = &v
			}
		}

		// Most recent service record → km since service (odometer context).
		var svcKm *float64
		var svcAt *time.Time
		if err := tx.QueryRow(r.Context(), `
			SELECT last_odometer::float8, last_done_at
			FROM service_schedules
			WHERE vehicle_id = $1 AND last_done_at IS NOT NULL
			ORDER BY last_done_at DESC NULLS LAST LIMIT 1`, id).
			Scan(&svcKm, &svcAt); err != nil && err != pgx.ErrNoRows {
			return err
		}
		if svcAt != nil {
			d := svcAt.Format("2006-01-02")
			res.LastServiceAt = &d
		}
		res.LastServiceKm = svcKm
		if svcKm != nil && res.OdometerKm != nil && *res.OdometerKm > *svcKm {
			diff := math.Round((*res.OdometerKm-*svcKm)*10) / 10
			res.KmSinceService = &diff
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}
