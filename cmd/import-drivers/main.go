// Command import-drivers loads a driver export into the FMS registry and links
// each driver to the truck they are assigned to.
//
// The CSV pairs a tracker IMEI with a driver from the legacy MongoDB system:
//
//	imeiTracker,driverId,diverName,DriverPhone
//	868373075424343,6808bc995183ac32375971d5,WALDI,08812702067
//
// Idempotent: drivers upsert on their Mongo ObjectId (employees.external_ref,
// uniquely indexed per tenant), so re-running updates rather than duplicating.
// That matters because rows are deliberately SKIPPED rather than guessed at —
// fix the underlying registry and run it again.
//
// WHAT IT REFUSES TO DO, and why. A tracker attached to two vehicles cannot be
// resolved to one truck, and picking either would assign a driver to a vehicle
// they may not be in. The cost of that is not a wrong row in a table: it is a
// planner ringing the wrong person during an incident. Seven such trackers
// exist in this fleet today, so the case is real rather than theoretical.
//
//	go run ./cmd/import-drivers -csv ../driver_inject.csv -tenant 1
//	go run ./cmd/import-drivers -csv ../driver_inject.csv -tenant 1 -dry-run
//
// Connects as the DB owner (bypasses RLS) and writes into the given tenant.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// objectID is what a Mongo _id looks like. Checked because external_ref is the
// upsert key: a malformed one would create a second driver record on every run
// rather than updating the first.
var objectID = regexp.MustCompile(`^[0-9a-f]{24}$`)

var nonDigit = regexp.MustCompile(`\D`)

// dialable reports whether a phone number has enough digits to call.
//
// Stored AS WRITTEN rather than normalised to +62: "08812702067" is the form an
// Indonesian planner recognises at a glance, and the UI already converts to
// international form for wa.me links. Normalising here would make every number
// on screen look foreign for no gain.
func dialable(phone string) bool {
	return len(nonDigit.ReplaceAllString(phone, "")) >= 10
}

type row struct {
	imei, driverID, name, phone string
}

type stats struct {
	imported    int
	updatedLink int
	noTracker   []string
	ambiguous   []string
	badID       []string
	badPhone    []string
	noVehicle   []string
}

func main() {
	csvPath := flag.String("csv", "../driver_inject.csv", "path to the driver CSV export")
	tenant := flag.Int64("tenant", 1, "target tenant id")
	dryRun := flag.Bool("dry-run", false, "report what would happen, write nothing")
	dsn := flag.String("dsn", env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/fms_app"),
		"database URL or keyword DSN (owner role, bypasses RLS)")
	flag.Parse()

	f, err := os.Open(*csvPath)
	if err != nil {
		log.Fatalf("open csv: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		log.Fatalf("read header: %v", err)
	}
	// Positional lookup by header name, so a column order change or an extra
	// column does not silently shift the data. Note "diverName" — the export's
	// own spelling, matched as written rather than corrected.
	idx := map[string]int{}
	for i, h := range head {
		idx[strings.TrimSpace(h)] = i
	}
	need := []string{"imeiTracker", "driverId", "diverName", "DriverPhone"}
	for _, n := range need {
		if _, ok := idx[n]; !ok {
			log.Fatalf("csv is missing the %q column; found: %v", n, head)
		}
	}

	var rows []row
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read csv: %v", err)
		}
		get := func(k string) string { return strings.TrimSpace(rec[idx[k]]) }
		rows = append(rows, row{
			imei:     get("imeiTracker"),
			driverID: get("driverId"),
			name:     get("diverName"),
			phone:    get("DriverPhone"),
		})
	}
	log.Printf("read %d rows from %s", len(rows), *csvPath)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	var role string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		log.Fatalf("connect: %v", err)
	}
	log.Printf("connected as %s, tenant %d", role, *tenant)
	if *dryRun {
		log.Printf("DRY RUN — nothing will be written")
	}

	var s stats
	for _, rw := range rows {
		if !objectID.MatchString(rw.driverID) {
			s.badID = append(s.badID, rw.imei)
			continue
		}
		if !dialable(rw.phone) {
			// Kept as a warning rather than a skip: a driver with a bad number
			// is still worth having on the vehicle, and the UI already shows
			// "no number" rather than offering a broken call button.
			s.badPhone = append(s.badPhone, rw.imei)
		}

		// Resolve the truck FIRST. A driver with nowhere to go is not worth
		// creating: it would leave an orphan employee record that no screen
		// ever shows, and re-running after fixing the registry would then
		// update rather than insert, hiding the fact it was ever broken.
		var vehicleIDs []int64
		err := pool.QueryRow(ctx, `
SELECT coalesce(array_agg(v.id), '{}')
FROM trackers t
JOIN vehicles v ON v.tracker_id = t.id
WHERE t.imei = $1 AND t.tenant_id = $2`, rw.imei, *tenant).Scan(&vehicleIDs)
		if err != nil {
			log.Fatalf("resolving imei %s: %v", rw.imei, err)
		}
		switch {
		case len(vehicleIDs) == 0:
			s.noTracker = append(s.noTracker, rw.imei)
			continue
		case len(vehicleIDs) > 1:
			// See the header: guessing here means calling the wrong person.
			s.ambiguous = append(s.ambiguous, rw.imei)
			continue
		}
		vehicleID := vehicleIDs[0]

		if *dryRun {
			s.imported++
			continue
		}

		// One transaction per row: a failure part-way leaves the rows already
		// done in place, and re-running finishes the job rather than starting
		// over.
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var driverID int64
			// The partial unique index carries a WHERE clause, so the conflict
			// target must repeat it or Postgres cannot match the index.
			if err := tx.QueryRow(ctx, `
INSERT INTO employees (tenant_id, full_name, phone, role, status, external_ref)
VALUES ($1, $2, $3, 'driver', 'active', $4)
ON CONFLICT (tenant_id, external_ref) WHERE external_ref IS NOT NULL
DO UPDATE SET full_name = EXCLUDED.full_name,
              phone      = EXCLUDED.phone,
              updated_at = now()
RETURNING id`, *tenant, rw.name, rw.phone, rw.driverID).Scan(&driverID); err != nil {
				return fmt.Errorf("upsert driver %s: %w", rw.driverID, err)
			}
			ct, err := tx.Exec(ctx,
				`UPDATE vehicles SET current_driver_id = $2, updated_at = now()
				 WHERE id = $1 AND tenant_id = $3
				   AND (current_driver_id IS DISTINCT FROM $2)`,
				vehicleID, driverID, *tenant)
			if err != nil {
				return fmt.Errorf("link vehicle %d: %w", vehicleID, err)
			}
			if ct.RowsAffected() > 0 {
				s.updatedLink++
			}
			return nil
		})
		if err != nil {
			log.Fatalf("row %s: %v", rw.imei, err)
		}
		s.imported++
	}

	log.Printf("--- done ---")
	log.Printf("  drivers imported/updated : %d", s.imported)
	log.Printf("  vehicle links changed    : %d", s.updatedLink)
	report("no tracker in the registry", s.noTracker)
	report("tracker on more than one vehicle (skipped deliberately)", s.ambiguous)
	report("malformed driverId", s.badID)
	report("phone too short to dial (imported anyway)", s.badPhone)
	report("tracker exists but no vehicle", s.noVehicle)
}

// report prints a skipped category with enough examples to act on, without
// dumping fifteen hundred lines when something goes broadly wrong.
func report(label string, items []string) {
	if len(items) == 0 {
		return
	}
	log.Printf("  %-55s : %d", label, len(items))
	max := len(items)
	if max > 10 {
		max = 10
	}
	for _, it := range items[:max] {
		log.Printf("      %s", it)
	}
	if len(items) > max {
		log.Printf("      ... and %d more", len(items)-max)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
