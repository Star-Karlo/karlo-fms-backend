// Package vehiclesync keeps fms_app.vehicles as a PROJECTION of the fleet
// register held in the shared master-data service.
//
// WHY A PROJECTION RATHER THAN DELETING THE TABLE.
//
// Exactly the argument tenantsync makes for `tenants`, with different numbers.
// Sixteen queries across alerts, service schedules, driver-score events,
// documents and notification rules join `vehicles` to put a plate beside a row:
//
//	LEFT JOIN vehicles v ON v.id = a.vehicle_id
//
// They join on the BIGINT, and they have to. alerts.vehicle_id,
// driving_events.vehicle_id, vehicle_telemetry_state.vehicle_id and thirty
// row-level-security policies are all bigint-keyed, over years of history.
// Re-keying that to a Mongo ObjectId is a rewrite of the data plane.
//
// So the SQL stays and the DATA moves. Master data is authoritative — the web
// app creates and edits vehicles there, and so does TMS — and this keeps the
// local copy in step. The table is a cache with a refresher; nothing else
// writes to it.
//
// WHAT IT FIXES. Right now the two copies are identical, because the migration
// just made them so, and nothing looks wrong. The moment somebody adds a truck
// in master data or re-plates one, they diverge: the new truck's alerts render
// with no plate at all, and the re-plated one shows the old number on every
// report. Neither errors. Both are simply wrong, quietly, and stay wrong.
//
// WHAT IT DELIBERATELY DOES NOT DO.
//
//   - It never deletes. A vehicle removed upstream is ARCHIVED here, because
//     alerts, telemetry and driving events still reference its id and a delete
//     would either cascade them away or fail on the foreign key. Archiving is
//     what FMS already does with a retired truck.
//   - It never invents a tenant. A company with no fms_tenant_id is not an FMS
//     customer; its vehicles are skipped rather than landed under tenant 0.
//   - It writes nothing back upstream. Master data does not learn the bigint,
//     which is why the ObjectId is recorded HERE — see 0042.
package vehiclesync

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

// Syncer projects master-data vehicles into fms_app.vehicles.
type Syncer struct {
	pool *db.Pool
	dir  *tenantdir.Directory
	md   *mdclient.Client
	wake chan struct{} // see Wake
}

// New returns nil unless both halves are configured — the directory to know
// which companies are FMS customers, and the client to read their fleets.
// A deployment that has not cut over gets no syncer and keeps its own table.
func New(pool *db.Pool, dir *tenantdir.Directory, md *mdclient.Client) *Syncer {
	if dir == nil || md == nil {
		return nil
	}
	return &Syncer{pool: pool, dir: dir, md: md, wake: make(chan struct{}, 1)}
}

// Wake asks for a refresh now rather than at the next tick — a master-data
// change notice (mdevents) or an operator's request. Never blocks; calls
// that land while one is pending coalesce into a single pass.
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
	if err := s.Once(ctx); err != nil {
		log.Printf("vehiclesync: first refresh failed, joins will use the existing rows: %v", err)
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
		if err := s.Once(ctx); err != nil {
			log.Printf("vehiclesync: %v", err)
		}
	}
}

// Result is what one pass changed, for the log and for tests.
type Result struct {
	Companies int
	Seen      int
	Linked    int // matched to an existing row by plate, and stamped
	Inserted  int
	Updated   int
	Archived  int
	Restored  int // master data says live again; was archived here
	Released  int // pairing pointed at a document master data no longer has
	Paired    int // current driver changed to match master data
	Skipped   int
}

// Once reconciles every FMS company's fleet.
//
// One transaction PER COMPANY, not one for everything. A customer whose fleet
// trips a constraint should cost that customer's refresh and nothing else —
// with a single transaction, one bad row anywhere would roll back the whole
// platform's projection and the next tick would do it again.
func (s *Syncer) Once(ctx context.Context) error {
	companies, err := s.dir.List(ctx)
	if err != nil {
		return err
	}
	if len(companies) == 0 {
		// The same refusal tenantsync makes. An auth service answering 200 with
		// nothing is indistinguishable from every customer having been removed,
		// and acting on it would archive every vehicle on the platform.
		log.Printf("vehiclesync: the company list came back empty; leaving the projection alone")
		return nil
	}

	var total Result
	for _, c := range companies {
		if c.CompanyID == "" || c.ID == 0 {
			continue
		}
		r, err := s.company(ctx, c)
		if err != nil {
			// Logged and carried on, deliberately: one customer's master-data
			// read failing must not stop the other four being refreshed.
			log.Printf("vehiclesync: %s (tenant %d): %v", c.Name, c.ID, err)
			continue
		}
		total.Companies++
		total.Seen += r.Seen
		total.Linked += r.Linked
		total.Inserted += r.Inserted
		total.Updated += r.Updated
		total.Archived += r.Archived
		total.Restored += r.Restored
		total.Released += r.Released
		total.Paired += r.Paired
		total.Skipped += r.Skipped
	}

	if total.Linked+total.Inserted+total.Updated+total.Archived+total.Restored+total.Released+total.Paired > 0 {
		log.Printf("vehiclesync: %d vehicle(s) across %d companies — %d linked, %d added, %d updated, %d archived, %d restored, %d re-paired, %d driver pairings",
			total.Seen, total.Companies, total.Linked, total.Inserted, total.Updated, total.Archived, total.Restored, total.Released, total.Paired)
	}
	return nil
}

func (s *Syncer) company(ctx context.Context, c tenantdir.Tenant) (Result, error) {
	var r Result
	vehicles, err := s.md.ListVehicles(ctx, c.CompanyID)
	if err != nil {
		return r, err
	}
	r.Seen = len(vehicles)
	if len(vehicles) == 0 {
		// Same reasoning as the empty company list: a company that genuinely
		// has no vehicles and a read that returned none look identical here,
		// and the destructive reading would archive their whole fleet.
		return r, nil
	}

	// db.Tx, NOT db.SystemTx — scoped to this company's tenant.
	//
	// tenantsync can use SystemTx because `tenants` has no RLS policy; `vehicles`
	// has one, and it applies to INSERT as a WITH CHECK. Under SystemTx
	// app.current_tenant is deliberately blank, so every insert here failed with
	// "new row violates row-level security policy for table vehicles" — all five
	// companies, every tick.
	//
	// Running each company's pass AS that tenant is not a workaround for the
	// policy, it is the correct reading of it: this projection genuinely is
	// writing one customer's rows, and a bug that tried to write another's
	// should be refused exactly as any request would be.
	// Every ObjectId master data currently holds for this company, built BEFORE
	// the loop so the stale-pairing sweep below can run first.
	incoming := make([]string, 0, len(vehicles))
	for _, v := range vehicles {
		if !v.Deleted && strings.TrimSpace(v.LicensePlate) != "" {
			incoming = append(incoming, v.ID)
		}
	}

	err = db.Tx(ctx, s.pool, c.ID, func(tx pgx.Tx) error {
		// Serialise against telemetrysync, which writes the same rows in an
		// order this job cannot match. Taken first, before any row lock.
		if err := db.LockFleetProjection(ctx, tx, c.ID); err != nil {
			return err
		}

		// RELEASE PAIRINGS THAT POINT AT A DOCUMENT THAT NO LONGER EXISTS.
		//
		// Without this the projection DUPLICATES THE ENTIRE FLEET the first time
		// master data is rebuilt. Rebuilding gives every vehicle a fresh
		// ObjectId; the local row still carries the old one, so the adopt step
		// below — which only considers rows with master_data_id IS NULL — skips
		// it, the insert fires instead, and the sweep at the end archives the
		// original. The result reads as a healthy fleet: same plates, all
		// active, all paired. Underneath, every alert, telemetry row and
		// driving event still references the ARCHIVED twin, so history detaches
		// from the vehicle it belongs to and nothing reports a problem.
		//
		// Observed exactly that locally: 1,988 vehicles became 3,975 — 1,987
		// archived originals and 1,988 new actives — after master data was
		// dropped and reloaded between runs. In production that is a plausible
		// sequence, not a contrived one: re-run 05 after fixing a data problem
		// and this would double the table.
		//
		// Clearing the id lets the plate adopt the row again, which is what
		// should have happened. A vehicle genuinely removed upstream keeps its
		// pairing here — it is simply absent from `incoming` and gets archived
		// by the sweep, which is the intended path.
		relCt, err := tx.Exec(ctx, `
			UPDATE vehicles SET master_data_id = NULL
			 WHERE tenant_id = $1
			   AND master_data_id IS NOT NULL
			   AND NOT (master_data_id = ANY($2::text[]))
			   AND EXISTS (SELECT 1 FROM vehicles w
			                WHERE w.tenant_id = $1 AND w.master_data_id = ANY($2::text[])
			                  AND upper(regexp_replace(w.license_plate, '[^A-Za-z0-9]', '', 'g'))
			                    = upper(regexp_replace(vehicles.license_plate, '[^A-Za-z0-9]', '', 'g'))
			              ) IS NOT TRUE`,
			c.ID, incoming)
		if err != nil {
			return err
		}
		r.Released = int(relCt.RowsAffected())

		live := make([]string, 0, len(vehicles))

		for _, v := range vehicles {
			plate := strings.TrimSpace(v.LicensePlate)
			if v.Deleted {
				continue // handled by the archive sweep below
			}
			if plate == "" {
				r.Skipped++
				continue
			}
			live = append(live, v.ID)

			// 1. Adopt by normalised plate, ONCE. Every vehicle the migration
			//    moved already exists here; this is what pairs the two without
			//    a second import. Only rows not yet claimed by another
			//    ObjectId are eligible.
			ct, err := tx.Exec(ctx, `
				UPDATE vehicles SET master_data_id = $1, updated_at = now()
				 WHERE tenant_id = $2
				   AND master_data_id IS NULL
				   AND upper(regexp_replace(license_plate, '[^A-Za-z0-9]', '', 'g'))
				     = upper(regexp_replace($3,            '[^A-Za-z0-9]', '', 'g'))`,
				v.ID, c.ID, plate)
			if err != nil {
				return err
			}
			if ct.RowsAffected() > 0 {
				r.Linked += int(ct.RowsAffected())
			}

			// 2. Insert when still unknown. id is GENERATED ALWAYS and this
			//    table DOES still allocate its own — unlike tenants, whose ids
			//    come from upstream. Master data has no bigint to offer, so FMS
			//    mints one and remembers which ObjectId it belongs to.
			ct, err = tx.Exec(ctx, `
				INSERT INTO vehicles (tenant_id, license_plate, hull_no, unit_year, status, master_data_id)
				SELECT $1, $2, NULLIF($3,''), NULLIF($4,0)::int, 'active', $5
				 WHERE NOT EXISTS (SELECT 1 FROM vehicles w WHERE w.master_data_id = $5)`,
				c.ID, plate, v.ChassisNumber, v.Year, v.ID)
			if err != nil {
				return err
			}
			r.Inserted += int(ct.RowsAffected())

			// 3. Keep the projected fields in step. Only the ones master data
			//    owns: odometer, fuel figures, tracker link and notes are FMS's
			//    own and are never touched by this.
			//
			//    The WHERE clause makes this a no-op when nothing changed, so
			//    the count means "actually differed" rather than "row visited"
			//    — a sync that reports 1,988 updates every five minutes tells
			//    nobody anything.
			ct, err = tx.Exec(ctx, `
				UPDATE vehicles SET license_plate = $2,
				                    hull_no       = COALESCE(NULLIF($3,''), hull_no),
				                    unit_year     = COALESCE(NULLIF($4,0)::int, unit_year),
				                    updated_at    = now()
				 WHERE master_data_id = $1
				   AND (license_plate IS DISTINCT FROM $2
				        OR (NULLIF($3,'') IS NOT NULL AND hull_no   IS DISTINCT FROM $3)
				        OR (NULLIF($4,0)  IS NOT NULL AND unit_year IS DISTINCT FROM $4::int))`,
				v.ID, plate, v.ChassisNumber, v.Year)
			if err != nil {
				return err
			}
			r.Updated += int(ct.RowsAffected())

			// 3b. Un-archive when master data says the truck is live again.
			//
			// Without this the projection is one-directional on status: the
			// sweep below archives a vehicle master data has retired, but
			// nothing ever reverses it, so restoring a truck upstream leaves it
			// invisible in FMS forever.
			//
			// ONLY from 'archive', never touching 'active' or 'repair'. Those
			// two are FMS's own vocabulary — master data has no notion of a
			// truck being in the workshop — and overwriting 'repair' with
			// 'active' every five minutes would quietly undo a decision a
			// depot made.
			ct, err = tx.Exec(ctx, `
				UPDATE vehicles SET status = 'active', updated_at = now()
				 WHERE master_data_id = $1 AND status = 'archive'`, v.ID)
			if err != nil {
				return err
			}
			r.Restored += int(ct.RowsAffected())

			// 3c. Current driver. Master data owns the pairing (TMS assigns
			//     it for trips; FMS's assign-driver writes through to it),
			//     so it is projected like the plate: the driver's ObjectId
			//     resolves to the local employees row driversync created.
			//     A driver not yet projected leaves the field as it is, so
			//     the next pass — after driversync — pairs it rather than
			//     clearing a pairing that exists upstream.
			if len(v.DriverIDs) == 0 {
				ct, err = tx.Exec(ctx, `
					UPDATE vehicles SET current_driver_id = NULL, updated_at = now()
					 WHERE master_data_id = $1 AND current_driver_id IS NOT NULL`, v.ID)
			} else {
				ct, err = tx.Exec(ctx, `
					UPDATE vehicles SET current_driver_id = e.id, updated_at = now()
					  FROM employees e
					 WHERE vehicles.master_data_id = $1
					   AND e.tenant_id = vehicles.tenant_id AND e.master_data_id = $2
					   AND vehicles.current_driver_id IS DISTINCT FROM e.id`, v.ID, v.DriverIDs[0])
			}
			if err != nil {
				return err
			}
			r.Paired += int(ct.RowsAffected())
		}

		// 4. Anything this company has here, already paired with master data,
		//    that master data no longer lists as live. Archived, never deleted.
		//
		//    Restricted to rows carrying a master_data_id on purpose: a vehicle
		//    that has never been paired might simply not have been reached by
		//    step 1 yet, and archiving those would take out the whole fleet on
		//    the first run against a company whose plates do not match.
		ct, err := tx.Exec(ctx, `
			UPDATE vehicles SET status = 'archive', updated_at = now()
			 WHERE tenant_id = $1
			   AND master_data_id IS NOT NULL
			   AND NOT (master_data_id = ANY($2::text[]))
			   AND status <> 'archive'`,
			c.ID, live)
		if err != nil {
			return err
		}
		r.Archived = int(ct.RowsAffected())
		return nil
	})
	return r, err
}
