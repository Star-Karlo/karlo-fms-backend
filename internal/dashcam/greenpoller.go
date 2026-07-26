package dashcam

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// GreenPoller turns Teltonika Green Driving events from the telemetry service
// into driving_events — the tracker-sourced twin of AlarmPoller.
//
// It is what makes driver behaviour scoring fleet-wide: a truck with only a GPS
// tracker (no camera) still contributes harsh braking/acceleration/cornering
// events. Same shape as the alarm poller — poll by ascending id, keep a cursor,
// map the device to a vehicle+tenant — but the device key is the tracker IMEI,
// resolved through trackers → vehicles.tracker_id.
type GreenPoller struct {
	pool   *db.Pool
	tel    *telemetry.Client
	every  time.Duration
	cursor int64 // last telemetry green-event id materialised; -1 until baseline
}

// NewGreenPoller builds the poller. 20s matches the alarm poller so behaviour
// events from either source reach a driver's score on the same cadence.
func NewGreenPoller(pool *db.Pool, tel *telemetry.Client) *GreenPoller {
	return &GreenPoller{pool: pool, tel: tel, every: 20 * time.Second, cursor: -1}
}

// Run polls until ctx is cancelled.
func (p *GreenPoller) Run(ctx context.Context) {
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.tick(ctx); err != nil {
				log.Printf("green driving poller: %v", err)
			}
		}
	}
}

func (p *GreenPoller) tick(ctx context.Context) error {
	since := p.cursor
	if since < 0 {
		since = 0
	}
	events, err := p.tel.GreenDrivingEvents(ctx, since, 500)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	maxID := events[len(events)-1].ID

	// First poll sets the baseline without materialising history.
	if p.cursor < 0 {
		p.cursor = maxID
		return nil
	}

	wanted := map[string]bool{}
	for _, e := range events {
		wanted[e.IMEI] = true
	}
	links := p.imeiLinks(ctx, wanted)

	for _, e := range events {
		link, ok := links[e.IMEI]
		if !ok {
			continue // tracker not on a vehicle we own — nothing to attribute
		}
		b, ok := greenBehaviour(e.EventType)
		if !ok {
			continue // not one of the harsh accel/brake/corner types
		}
		if err := recordGreenEvent(ctx, p.pool, link.tenantID, link, e, b); err != nil {
			log.Printf("green driving poller: record for %s: %v", e.IMEI, err)
		}
	}
	p.cursor = maxID
	return nil
}

// imeiLinks resolves tracker IMEIs to their vehicle+tenant+driver. Reads per
// tenant under RLS, like deviceLinks, but joins trackers → vehicles on the IMEI.
func (p *GreenPoller) imeiLinks(ctx context.Context, wanted map[string]bool) map[string]devLink {
	out := map[string]devLink{}
	rows, err := p.pool.Query(ctx, `SELECT id FROM tenants`)
	if err != nil {
		log.Printf("green driving poller: list tenants: %v", err)
		return out
	}
	var tenantIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			tenantIDs = append(tenantIDs, id)
		}
	}
	rows.Close()

	for _, tid := range tenantIDs {
		_ = db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
			// Camera trucks are excluded: the camera is authoritative for the
			// per-axis X/Y/Z g of a harsh event, so its events score those trucks
			// and the tracker's Green Driving would only double-count. Tracker
			// Green Driving covers the camera-less majority of the fleet.
			r, err := tx.Query(ctx, `
SELECT t.imei, v.id, v.current_driver_id
FROM trackers t JOIN vehicles v ON v.tracker_id = t.id
WHERE NOT EXISTS (SELECT 1 FROM dashcams d WHERE d.vehicle_id = v.id)`)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var imei string
				var veh int64
				var drv *int64
				if err := r.Scan(&imei, &veh, &drv); err != nil {
					return err
				}
				if wanted[imei] {
					out[imei] = devLink{tenantID: tid, vehicleID: veh, driverID: drv}
				}
			}
			return r.Err()
		})
	}
	return out
}
