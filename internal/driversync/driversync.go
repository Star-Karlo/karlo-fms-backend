// Package driversync projects the master-data driver register into
// fms_app.employees, the way vehiclesync does for the fleet.
//
// WHY IT EXISTS.
//
// After the migration, drivers live in karlo_masterdata. But
// fms_app.vehicles.current_driver_id is a BIGINT referencing employees(id), and
// nothing was creating the local row — so a driver created after the cutover
// had no employees row, no bigint, and could not be attached to a vehicle at
// all. The vehicle form greyed those drivers out and told the operator they
// were "added after the migration, not yet assignable", which was honest and
// permanent: nothing was ever going to change it.
//
// For a company onboarded AFTER the migration that meant EVERY driver was
// unassignable, so the Current Driver field could never be set for anybody.
// DEMO ACCOUNT hit exactly that — three drivers, all grey, no others to choose.
//
// The migration stamped attributes.fmsEmployeeId onto the 1,500 drivers it
// moved, and that stamp was what "assignable" meant. Nothing wrote it
// afterwards. This closes the loop: master data is authoritative, the local
// table is a projection, and every driver acquires a bigint.
//
// RELATIONSHIP TO WRITE-THROUGH. A driver created through FMS is born paired —
// the handler writes master data first and inserts locally with the id already
// set. This loop covers everything else: drivers created in TMS, drivers that
// predate write-through, and edits made upstream.
//
// SERVICE TOKEN OVER gRPC, not a user's bearer over HTTP. This runs on a timer
// with nobody behind it, so there is no caller to borrow — the same reasoning
// as vehiclesync, and the reason the read had to be a gRPC method rather than
// the existing HTTP endpoint.
package driversync

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

// Syncer projects master-data drivers into fms_app.employees.
type Syncer struct {
	pool *db.Pool
	dir  *tenantdir.Directory
	md   *mdclient.Client
	wake chan struct{} // see Wake
}

// New returns nil unless both halves are configured, matching vehiclesync: the
// directory to know which companies are FMS customers, the client to read them.
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
		log.Printf("driversync: first refresh failed, assignment will use the existing rows: %v", err)
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
			log.Printf("driversync: %v", err)
		}
	}
}

// Result is what one pass changed.
type Result struct {
	Companies int
	Seen      int
	Linked    int // matched an existing local row and stamped the pairing
	Inserted  int
	Updated   int
	Retired   int // master data deleted them; marked inactive here
	Restored  int
	Skipped   int
}

// Once reconciles every FMS company's drivers.
//
// One transaction per company, for the reason vehiclesync gives: a customer
// whose data trips a constraint should cost that customer's refresh, not the
// whole platform's.
func (s *Syncer) Once(ctx context.Context) error {
	companies, err := s.dir.List(ctx)
	if err != nil {
		return err
	}
	if len(companies) == 0 {
		// The same refusal the other two make. A directory answering 200 with
		// nothing is indistinguishable from every customer having been deleted,
		// and acting on it would retire every driver on the platform.
		log.Printf("driversync: the company list came back empty; leaving the projection alone")
		return nil
	}

	var total Result
	for _, c := range companies {
		if c.CompanyID == "" || c.ID == 0 {
			continue
		}
		r, err := s.company(ctx, c)
		if err != nil {
			log.Printf("driversync: %s (tenant %d): %v", c.Name, c.ID, err)
			continue
		}
		total.Companies++
		total.Seen += r.Seen
		total.Linked += r.Linked
		total.Inserted += r.Inserted
		total.Updated += r.Updated
		total.Retired += r.Retired
		total.Restored += r.Restored
		total.Skipped += r.Skipped
	}

	// Silent when nothing changed, so the log means something. A sync that
	// reports every driver every five minutes tells nobody anything.
	if total.Linked+total.Inserted+total.Updated+total.Retired+total.Restored > 0 {
		log.Printf("driversync: %d driver(s) across %d companies — %d linked, %d added, %d updated, %d retired, %d restored",
			total.Seen, total.Companies, total.Linked, total.Inserted, total.Updated, total.Retired, total.Restored)
	}
	return nil
}

func (s *Syncer) company(ctx context.Context, c tenantdir.Tenant) (Result, error) {
	var r Result

	drivers, err := s.md.ListDrivers(ctx, c.CompanyID)
	if err != nil {
		return r, err
	}
	r.Seen = len(drivers)
	if len(drivers) == 0 {
		// Nothing to do, and NOT a signal to retire the company's drivers. An
		// empty read is far more likely to be a transient upstream problem than
		// a company genuinely dismissing its entire workforce.
		return r, nil
	}

	err = db.Tx(ctx, s.pool, c.ID, func(tx pgx.Tx) error {
		for _, d := range drivers {
			name := strings.TrimSpace(d.FullName)
			if d.Deleted {
				continue // handled by the retire pass below
			}
			if name == "" {
				r.Skipped++
				continue
			}

			// 1. ADOPT AN EXISTING ROW.
			//
			// Two ways in, tried in order of how much they prove.
			//
			// First the FMS bigint master data already carries: that is a
			// pairing somebody recorded deliberately — by the migration, or by
			// write-through stamping it — and it names one row exactly.
			//
			// Only then the name, and only when it is UNAMBIGUOUS. Two drivers
			// can share a full name; adopting on a name that matches twice
			// would attach a master-data driver to whichever row came first and
			// silently move that person's vehicle assignments. So the match is
			// required to be unique, and a duplicate name simply falls through
			// to the insert below, which is the safe direction to be wrong in.
			var linked bool
			if d.FMSEmployeeID > 0 {
				ct, err := tx.Exec(ctx, `
					UPDATE employees SET master_data_id = $1, updated_at = now()
					 WHERE tenant_id = $2 AND id = $3 AND master_data_id IS NULL`,
					d.ID, c.ID, d.FMSEmployeeID)
				if err != nil {
					return err
				}
				if ct.RowsAffected() > 0 {
					r.Linked++
					linked = true
				}
			}
			if !linked {
				ct, err := tx.Exec(ctx, `
					UPDATE employees SET master_data_id = $1, updated_at = now()
					 WHERE id = (
					   -- min(id) rather than id: with HAVING and no GROUP BY the
					   -- whole match is one group, so the selected column has to
					   -- be aggregated or Postgres rejects the statement. The
					   -- HAVING is what enforces uniqueness — no row comes back
					   -- when the name matched twice, so the UPDATE matches
					   -- nothing and the driver falls through to the insert.
					   SELECT min(id) FROM employees
					    WHERE tenant_id = $2
					      AND master_data_id IS NULL
					      AND lower(btrim(full_name)) = lower(btrim($3))
					    HAVING count(*) = 1
					 )`, d.ID, c.ID, name)
				if err != nil {
					return err
				}
				if ct.RowsAffected() > 0 {
					r.Linked++
					linked = true
				}
			}

			// 2. INSERT when still unknown. employees.id is GENERATED ALWAYS —
			//    master data has no bigint to offer, so FMS mints one and
			//    remembers which ObjectId it belongs to. This is the step that
			//    makes a driver assignable.
			if !linked {
				ct, err := tx.Exec(ctx, `
					INSERT INTO employees (tenant_id, full_name, phone, employee_no,
					                       license_no, license_class, license_expiry,
					                       role, status, master_data_id)
					SELECT $1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), NULLIF($6,''),
					       $7::date, 'driver', COALESCE(NULLIF($8,''),'active'), $9
					 WHERE NOT EXISTS (SELECT 1 FROM employees w WHERE w.master_data_id = $9)`,
					c.ID, name, d.Phone, d.EmployeeNo, d.LicenseNo, d.LicenseClass,
					d.LicenseExpiry, d.Status, d.ID)
				if err != nil {
					return err
				}
				r.Inserted += int(ct.RowsAffected())
			}

			// 3. Keep the projected fields in step. Only what master data owns:
			//    `role` is FMS's own and is never touched. The WHERE makes this
			//    a no-op when nothing differs, so the count means "actually
			//    changed" rather than "row visited".
			ct, err := tx.Exec(ctx, `
				UPDATE employees SET full_name      = $2,
				                     phone          = COALESCE(NULLIF($3,''), phone),
				                     employee_no    = COALESCE(NULLIF($4,''), employee_no),
				                     license_no     = COALESCE(NULLIF($5,''), license_no),
				                     license_class  = COALESCE(NULLIF($6,''), license_class),
				                     license_expiry = COALESCE($7::date, license_expiry),
				                     updated_at     = now()
				 WHERE master_data_id = $1
				   AND (full_name IS DISTINCT FROM $2
				        OR (NULLIF($3,'') IS NOT NULL AND phone         IS DISTINCT FROM $3)
				        OR (NULLIF($4,'') IS NOT NULL AND employee_no   IS DISTINCT FROM $4)
				        OR (NULLIF($5,'') IS NOT NULL AND license_no    IS DISTINCT FROM $5)
				        OR (NULLIF($6,'') IS NOT NULL AND license_class IS DISTINCT FROM $6)
				        OR ($7::date IS NOT NULL AND license_expiry IS DISTINCT FROM $7::date))`,
				d.ID, name, d.Phone, d.EmployeeNo, d.LicenseNo, d.LicenseClass, d.LicenseExpiry)
			if err != nil {
				return err
			}
			r.Updated += int(ct.RowsAffected())

			// 3b. Bring someone back when master data says they are active
			//     again — otherwise retirement is one-directional and a
			//     reinstated driver stays invisible here forever.
			if d.Status == "active" {
				ct, err = tx.Exec(ctx, `
					UPDATE employees SET status = 'active', updated_at = now()
					 WHERE master_data_id = $1 AND status <> 'active'`, d.ID)
				if err != nil {
					return err
				}
				r.Restored += int(ct.RowsAffected())
			}
		}

		// 4. RETIRE, never delete.
		//
		// Restricted to rows that already carry a master_data_id, for the same
		// reason vehiclesync restricts its archive sweep: an unpaired row might
		// simply not have been adopted yet, and retiring those would deactivate
		// a company's whole workforce on the first run against a company whose
		// names do not match.
		//
		// Marked inactive rather than deleted because employees(id) is
		// referenced by vehicles.current_driver_id, alerts and driver-score
		// events. Deleting would break those or cascade away real history.
		live := make([]string, 0, len(drivers))
		for _, d := range drivers {
			if !d.Deleted {
				live = append(live, d.ID)
			}
		}
		ct, err := tx.Exec(ctx, `
			UPDATE employees SET status = 'inactive', updated_at = now()
			 WHERE tenant_id = $1
			   AND master_data_id IS NOT NULL
			   AND NOT (master_data_id = ANY($2::text[]))
			   AND status <> 'inactive'`, c.ID, live)
		if err != nil {
			return err
		}
		r.Retired += int(ct.RowsAffected())
		return nil
	})
	return r, err
}
