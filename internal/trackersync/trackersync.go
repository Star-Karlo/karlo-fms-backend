// Package trackersync projects master data's devices into this service.
//
// Devices were the dataset the cutover missed. fms_app.trackers (GPS units,
// keyed by IMEI) and fms_app.dashcams (VSS device ids) kept their own life —
// the Trackers page, the vehicle form's tracker binding, the dashcam link —
// while master data grew a trackers collection of its own from a TMS
// backfill, and nothing kept the two in step: a device re-fitted in TMS
// stayed on the old truck here, and positions kept flowing to the wrong
// plate. Master data owns devices now, as it owns vehicles and drivers;
// this projects them, and the write paths here go upstream first.
//
// Shape mirrors vehiclesync: one transaction per company as that tenant,
// pair by master_data_id, adopt by device id once, insert when unknown,
// keep the projected fields in step, and — the part that matters — set the
// fitting: master data's current_vehicle_id becomes trackers.vehicle_id and
// vehicles.tracker_id (which is what the live map keys on), through the
// same statements the vehicle form uses so the assignment-history trigger
// records the change.
package trackersync

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/tenantdir"
)

// Syncer projects devices for every FMS company.
type Syncer struct {
	pool *db.Pool
	dir  *tenantdir.Directory
	md   *mdclient.Client
	wake chan struct{}
	// fittings: whether master data's current_vehicle_id may move a device
	// here (config.TrackerFittings). Off, devices are paired and their
	// fields kept in step but vehicles.tracker_id / dashcam links are left
	// as FMS has them.
	fittings bool
}

// New returns nil unless both halves are configured, like vehiclesync.
func New(pool *db.Pool, dir *tenantdir.Directory, md *mdclient.Client, fittings bool) *Syncer {
	if dir == nil || md == nil {
		return nil
	}
	return &Syncer{pool: pool, dir: dir, md: md, wake: make(chan struct{}, 1), fittings: fittings}
}

// Wake asks for a refresh now (a karlo:masterdata notice, or an operator).
func (s *Syncer) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run refreshes now, then on a ticker or whenever woken.
func (s *Syncer) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	if err := s.pass(ctx); err != nil {
		log.Printf("trackersync: first refresh failed, devices keep the existing rows: %v", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
		if err := s.pass(ctx); err != nil {
			log.Printf("trackersync: %v", err)
		}
	}
}

const passTimeout = 3 * time.Minute

func (s *Syncer) pass(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	start := time.Now()
	err := s.Once(ctx)
	if d := time.Since(start); d > 30*time.Second {
		log.Printf("trackersync: pass took %s", d.Round(time.Second))
	}
	return err
}

// Result is what one pass changed.
type Result struct {
	Companies int
	Seen      int
	Linked    int // adopted an existing local device by its id
	Inserted  int
	Updated   int
	Fitted    int // vehicle binding changed to match master data
	Retired   int // master data says retired/deleted; local row inactive
	Skipped   int
}

// Once reconciles every company's devices.
func (s *Syncer) Once(ctx context.Context) error {
	companies, err := s.dir.List(ctx)
	if err != nil {
		return err
	}
	if len(companies) == 0 {
		log.Printf("trackersync: the company list came back empty; leaving the projection alone")
		return nil
	}
	var total Result
	for _, c := range companies {
		if c.CompanyID == "" || c.ID == 0 {
			continue
		}
		r, err := s.company(ctx, c)
		if err != nil {
			log.Printf("trackersync: %s (tenant %d): %v", c.Name, c.ID, err)
			continue
		}
		total.Companies++
		total.Seen += r.Seen
		total.Linked += r.Linked
		total.Inserted += r.Inserted
		total.Updated += r.Updated
		total.Fitted += r.Fitted
		total.Retired += r.Retired
		total.Skipped += r.Skipped
	}
	if total.Linked+total.Inserted+total.Updated+total.Fitted+total.Retired > 0 {
		log.Printf("trackersync: %d device(s) across %d companies — %d linked, %d added, %d updated, %d fittings, %d retired",
			total.Seen, total.Companies, total.Linked, total.Inserted, total.Updated, total.Fitted, total.Retired)
	}
	return nil
}

func (s *Syncer) company(ctx context.Context, c tenantdir.Tenant) (Result, error) {
	var r Result
	devices, err := s.md.ListTrackers(ctx, c.CompanyID, "")
	if err != nil {
		return r, err
	}
	r.Seen = len(devices)
	if len(devices) == 0 {
		return r, nil // a company with no devices and a failed read look alike; do nothing
	}

	err = db.Tx(ctx, s.pool, c.ID, func(tx pgx.Tx) error {
		if err := db.LockFleetProjection(ctx, tx, c.ID); err != nil {
			return err
		}
		for _, d := range devices {
			id := strings.TrimSpace(d.DeviceID)
			if id == "" {
				r.Skipped++
				continue
			}
			var err error
			switch d.Kind {
			case "dashcam":
				err = s.dashcam(ctx, tx, c.ID, d, id, &r)
			default:
				err = s.gps(ctx, tx, c.ID, d, id, &r)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	return r, err
}

// localStatus maps master data's vocabulary onto this table's.
func localStatus(d mdclient.Tracker) string {
	if d.Deleted || d.Status == "retired" || d.Status == "inactive" {
		return "inactive"
	}
	return "active"
}

func (s *Syncer) gps(ctx context.Context, tx pgx.Tx, tenantID int64, d mdclient.Tracker, imei string, r *Result) error {
	// 1. Adopt by IMEI, once.
	ct, err := tx.Exec(ctx, `
		UPDATE trackers SET master_data_id = $1, updated_at = now()
		 WHERE tenant_id = $2 AND master_data_id IS NULL AND imei = $3`, d.ID, tenantID, imei)
	if err != nil {
		return err
	}
	r.Linked += int(ct.RowsAffected())

	// 2. Insert when unknown. A retired device is not worth a row.
	if !d.Deleted && d.Status != "retired" {
		ct, err = tx.Exec(ctx, `
			INSERT INTO trackers (tenant_id, imei, phone_no, status, master_data_id)
			SELECT $1, $2, NULLIF($3,''), $4, $5
			 WHERE NOT EXISTS (SELECT 1 FROM trackers w WHERE w.master_data_id = $5)`,
			tenantID, imei, d.PhoneNo, localStatus(d), d.ID)
		if err != nil {
			return err
		}
		r.Inserted += int(ct.RowsAffected())
	}

	// 3. Projected fields: imei (a device re-keyed upstream), phone, status.
	ct, err = tx.Exec(ctx, `
		UPDATE trackers SET imei = $2,
		                    phone_no = COALESCE(NULLIF($3,''), phone_no),
		                    status = $4, updated_at = now()
		 WHERE master_data_id = $1
		   AND (imei IS DISTINCT FROM $2
		        OR (NULLIF($3,'') IS NOT NULL AND phone_no IS DISTINCT FROM $3)
		        OR status IS DISTINCT FROM $4)`,
		d.ID, imei, d.PhoneNo, localStatus(d))
	if err != nil {
		return err
	}
	n := int(ct.RowsAffected())
	if localStatus(d) == "inactive" {
		r.Retired += n
	} else {
		r.Updated += n
	}

	// 4. The fitting. Resolve master data's vehicle to the local row; a
	//    vehicle not yet projected leaves the binding alone for the next
	//    pass (vehiclesync runs on the same notice).
	if !s.fittings {
		return nil
	}
	var localID int64
	var curVehicle *int64
	if err := tx.QueryRow(ctx, `SELECT id, vehicle_id FROM trackers WHERE master_data_id = $1`, d.ID).Scan(&localID, &curVehicle); err != nil {
		if err == pgx.ErrNoRows {
			return nil // retired and never known here
		}
		return err
	}
	var want *int64
	if d.CurrentVehicleID != "" && localStatus(d) == "active" {
		var vid int64
		err := tx.QueryRow(ctx, `SELECT id FROM vehicles WHERE master_data_id = $1`, d.CurrentVehicleID).Scan(&vid)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		want = &vid
	}
	if (want == nil) == (curVehicle == nil) && (want == nil || *want == *curVehicle) {
		return nil // already as master data has it
	}
	// Same statements as the vehicle form's bind/unbind, so
	// trackers.vehicle_id changes fire the assignment-history trigger and a
	// vehicle never ends up with two open device assignments.
	if curVehicle != nil {
		if _, err := tx.Exec(ctx, `UPDATE vehicles SET tracker_id = NULL WHERE id = $1 AND tracker_id = $2`, *curVehicle, localID); err != nil {
			return err
		}
	}
	if want != nil {
		if _, err := tx.Exec(ctx, `UPDATE trackers SET vehicle_id = NULL WHERE vehicle_id = $1 AND id <> $2`, *want, localID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE vehicles SET tracker_id = $2 WHERE id = $1`, *want, localID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE trackers SET vehicle_id = $2, updated_at = now() WHERE id = $1`, localID, want); err != nil {
		return err
	}
	r.Fitted++
	return nil
}

func (s *Syncer) dashcam(ctx context.Context, tx pgx.Tx, tenantID int64, d mdclient.Tracker, deviceID string, r *Result) error {
	// Adopt by VSS device id, once.
	ct, err := tx.Exec(ctx, `
		UPDATE dashcams SET master_data_id = $1
		 WHERE tenant_id = $2 AND master_data_id IS NULL AND vss_device_id = $3`, d.ID, tenantID, deviceID)
	if err != nil {
		return err
	}
	r.Linked += int(ct.RowsAffected())

	if d.Deleted || d.Status == "retired" {
		// A dashcam row here is the link to a vehicle; a retired device has
		// none. The VSS device itself is not FMS's to keep.
		ct, err = tx.Exec(ctx, `DELETE FROM dashcams WHERE master_data_id = $1`, d.ID)
		if err != nil {
			return err
		}
		r.Retired += int(ct.RowsAffected())
		return nil
	}
	if !s.fittings {
		return nil // paired; the link stays as FMS has it until the flag is on
	}

	var want *int64
	if d.CurrentVehicleID != "" {
		var vid int64
		err := tx.QueryRow(ctx, `SELECT id FROM vehicles WHERE master_data_id = $1`, d.CurrentVehicleID).Scan(&vid)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		want = &vid
	}
	// The dashcams table only exists to say which vehicle a device is on:
	// fitted → one row (device name/type/channels are filled by the VSS sync
	// that already runs), not fitted → no row.
	if want == nil {
		ct, err = tx.Exec(ctx, `DELETE FROM dashcams WHERE master_data_id = $1`, d.ID)
		if err != nil {
			return err
		}
		r.Fitted += int(ct.RowsAffected())
		return nil
	}
	// A truck has at most one dashcam (dashcams_vehicle_uk): the device
	// master data has on it replaces whatever was linked here.
	if _, err := tx.Exec(ctx, `DELETE FROM dashcams WHERE vehicle_id = $1 AND vss_device_id <> $2`, *want, deviceID); err != nil {
		return err
	}
	ct, err = tx.Exec(ctx, `
		INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, master_data_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, vss_device_id) DO UPDATE
		   SET vehicle_id = EXCLUDED.vehicle_id, master_data_id = EXCLUDED.master_data_id
		 WHERE dashcams.vehicle_id IS DISTINCT FROM EXCLUDED.vehicle_id
		    OR dashcams.master_data_id IS DISTINCT FROM EXCLUDED.master_data_id`,
		tenantID, *want, deviceID, d.ID)
	if err != nil {
		return err
	}
	r.Fitted += int(ct.RowsAffected())
	return nil
}
