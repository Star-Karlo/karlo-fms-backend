package dashcam

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// AlarmPoller turns dashcam alarms from the telemetry service into FMS alerts.
//
// FMS holds no WebSocket. The telemetry service owns the live VSS socket and
// persists every alarm; this polls that store on a timer and raises a normal
// FMS alert for each — so camera events flow into the same Notifications feed,
// severity colours and rule machinery as every other alert, with no new UI.
//
// It maps each alarm's VSS device to the vehicle it is linked to (the dashcams
// table) and scopes the alert to that vehicle's tenant. An alarm from a device
// linked to no vehicle is skipped — there is no truck to attribute it to.
type AlarmPoller struct {
	pool  *db.Pool
	tel   *telemetry.Client
	every time.Duration

	// The last telemetry alarm id turned into an alert. -1 until the first poll
	// establishes a baseline, so a fresh start does not replay history as a
	// flood of "new" alerts.
	cursor int64
}

// NewAlarmPoller builds the poller. 20s is well inside the browser's own
// Notifications poll, so an alarm reaches a screen about as fast as any alert.
func NewAlarmPoller(pool *db.Pool, tel *telemetry.Client) *AlarmPoller {
	return &AlarmPoller{pool: pool, tel: tel, every: 20 * time.Second, cursor: -1}
}

// Run polls until ctx is cancelled.
func (p *AlarmPoller) Run(ctx context.Context) {
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.tick(ctx); err != nil {
				// Logged, not fatal: the telemetry service being briefly
				// unreachable must not stop the poller.
				log.Printf("dashcam alarm poller: %v", err)
			}
		}
	}
}

func (p *AlarmPoller) tick(ctx context.Context) error {
	since := p.cursor
	if since < 0 {
		since = 0
	}
	alarms, err := p.tel.DashcamAlarms(ctx, since, 500)
	if err != nil {
		return err
	}
	if len(alarms) == 0 {
		return nil
	}
	maxID := alarms[len(alarms)-1].ID

	// First poll establishes the baseline without alerting: everything already
	// in the store predates this process and is not "new".
	if p.cursor < 0 {
		p.cursor = maxID
		return nil
	}

	// Which vehicle (and tenant) each device belongs to. Built once per tick
	// across the devices in this batch, rather than per alarm.
	wanted := map[string]bool{}
	for _, a := range alarms {
		wanted[a.DeviceID] = true
	}
	links := p.deviceLinks(ctx, wanted)

	for _, a := range alarms {
		link, ok := links[a.DeviceID]
		if !ok {
			continue // device not linked to any vehicle — nothing to attribute
		}
		if isStatusEvent(a.Code) {
			continue // trip start, ACC on, GPS recovered… housekeeping, not alerts
		}
		code, name, severity := alarmMeta(a.Code)
		// The REAL alarm time — not now(). The evidence clip (and any SD footage)
		// lives at this instant, so a save's clip window must be built from it;
		// using the poll time (which can lag minutes behind a late-uploaded
		// alarm) pointed the window at a moment with no footage.
		occurred := parseTime(a.OccurredAt)
		camera := "camera"
		if err := db.Tx(ctx, p.pool, link.tenantID, func(tx pgx.Tx) error {
			_, e := store.New(tx).CreateAlert(ctx, store.CreateAlertParams{
				TenantID:     link.tenantID,
				VehicleID:    &link.vehicleID,
				AlertCode:    &code,
				AlertName:    &name,
				Severity:     &severity,
				DeviceSource: &camera,
				OccurredAt:   pgtype.Timestamptz{Time: occurred, Valid: true},
				Lat:          a.Lat,
				Lon:          a.Lon,
			})
			return e
		}); err != nil {
			log.Printf("dashcam alarm poller: create alert for %s: %v", a.DeviceID, err)
			continue
		}

		// If it is a driving-behaviour event (harsh brake/accel/turn), also
		// record it as a driving_event the score collector can read. Best-effort:
		// a failure here must not stop alerts flowing.
		if b, ok := isDrivingEvent(a.Code); ok {
			if err := recordDrivingEvent(ctx, p.pool, link.tenantID, link, a, b); err != nil {
				log.Printf("dashcam alarm poller: record driving event for %s: %v", a.DeviceID, err)
			}
		}
	}
	p.cursor = maxID
	return nil
}

type devLink struct {
	tenantID  int64
	vehicleID int64
	driverID  *int64 // current driver on the vehicle at ingest, or nil
}

// deviceLinks resolves the wanted device ids to their vehicle+tenant. dashcams
// is tenant-scoped by RLS, so it reads per tenant (tenants carries no policy) —
// the same shape the notification engine uses.
func (p *AlarmPoller) deviceLinks(ctx context.Context, wanted map[string]bool) map[string]devLink {
	out := map[string]devLink{}
	var tenantIDs []int64
	rows, err := p.pool.Query(ctx, `SELECT id FROM tenants`)
	if err != nil {
		log.Printf("dashcam alarm poller: list tenants: %v", err)
		return out
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			tenantIDs = append(tenantIDs, id)
		}
	}
	rows.Close()

	for _, tid := range tenantIDs {
		_ = db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
			r, err := tx.Query(ctx,
				`SELECT d.vss_device_id, d.vehicle_id, v.current_driver_id
				 FROM dashcams d JOIN vehicles v ON v.id = d.vehicle_id
				 WHERE d.vehicle_id IS NOT NULL`)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var dev string
				var veh int64
				var drv *int64
				if err := r.Scan(&dev, &veh, &drv); err != nil {
					return err
				}
				if wanted[dev] {
					out[dev] = devLink{tenantID: tid, vehicleID: veh, driverID: drv}
				}
			}
			return r.Err()
		})
	}
	return out
}

func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now()
}
