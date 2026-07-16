// Command import loads a legacy MongoDB "trucks" CSV export into the FMS
// application registry (vehicles + STNK/KIR documents).
//
// It is idempotent: rows are upserted by their source _id (external_ref), so
// re-running updates instead of duplicating. Soft-deleted source rows
// (policeNumber/truckNumber containing "deletedTruck") are skipped.
//
// It connects as the DB owner (bypasses RLS) and writes into the given tenant.
//
//	go run ./cmd/import -csv ../prod.trucks.csv -tenant 1
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	csvPath := flag.String("csv", "../prod.trucks.csv", "path to the trucks CSV export")
	tenant := flag.Int64("tenant", 1, "target tenant id")
	dsn := flag.String("dsn", env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/fms_app"), "database URL (owner role, bypasses RLS)")
	includeDeleted := flag.Bool("include-deleted", false, "also import soft-deleted rows")
	flag.Parse()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("db ping: %v", err)
	}

	// Ensure the target tenant exists.
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (name, slug) VALUES ('Karlo','karlo') ON CONFLICT (slug) DO NOTHING`); err != nil {
		log.Fatalf("ensure tenant: %v", err)
	}

	f, err := os.Open(*csvPath)
	if err != nil {
		log.Fatalf("open csv: %v", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		log.Fatalf("read header: %v", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}

	var seen, skipped, imported, docs, trackers int
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read row: %v", err)
		}
		seen++
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}

		police := get("policeNumber")
		truckNo := get("truckNumber")
		if !*includeDeleted && (strings.Contains(police, "deletedTruck") || strings.Contains(truckNo, "deletedTruck")) {
			skipped++
			continue
		}
		extRef := get("_id")
		if extRef == "" || police == "" {
			skipped++
			continue
		}

		attrs := map[string]any{
			"long": get("long"), "wide": get("wide"), "high": get("high"),
			"weight_max": get("weightMax"), "volume_max": get("volumeMax"),
			"axle": get("axle"), "truck_head_name": get("truckHeadName"),
			"truck_type_name": get("truckTypeName"), "truck_type_id": get("truckType"),
			"truck_head_id": get("truckHead"), "truck_body_id": get("truckBody"),
			"source_driver_id": get("driver"), // Mongo ObjectId — resolve when the drivers export arrives
		}
		attrsJSON, _ := json.Marshal(attrs)

		var vehicleID int64
		err = pool.QueryRow(ctx, `
			INSERT INTO vehicles (tenant_id, external_ref, license_plate, hull_no, manufacturer,
			                      vehicle_type, box_type, color, status, attributes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9)
			ON CONFLICT (tenant_id, external_ref) WHERE external_ref IS NOT NULL
			DO UPDATE SET license_plate=EXCLUDED.license_plate, hull_no=EXCLUDED.hull_no,
			              manufacturer=EXCLUDED.manufacturer, vehicle_type=EXCLUDED.vehicle_type,
			              box_type=EXCLUDED.box_type, color=EXCLUDED.color, attributes=EXCLUDED.attributes
			RETURNING id`,
			*tenant, extRef, police, nullify(truckNo), nullify(get("brandName")),
			nullify(get("truckTypeName")), nullify(get("truckBodyName")), nullify(get("color")), attrsJSON,
		).Scan(&vehicleID)
		if err != nil {
			log.Printf("row %s (%s): %v", extRef, police, err)
			skipped++
			continue
		}
		imported++

		// Link a tracker by IMEI (the join key to the telemetry pipeline).
		if imei := get("imeiTracker"); validIMEI(imei) {
			// If this vehicle previously had a different IMEI, unlink the old one
			// so the per-vehicle active-tracker uniqueness holds.
			_, _ = pool.Exec(ctx, `UPDATE trackers SET vehicle_id=NULL WHERE tenant_id=$1 AND vehicle_id=$2 AND imei<>$3`, *tenant, vehicleID, imei)
			var trackerID int64
			err := pool.QueryRow(ctx, `
				INSERT INTO trackers (tenant_id, imei, vehicle_id, external_ref, status)
				VALUES ($1,$2,$3,$2,'active')
				ON CONFLICT (tenant_id, imei) DO UPDATE SET vehicle_id=EXCLUDED.vehicle_id
				RETURNING id`, *tenant, imei, vehicleID).Scan(&trackerID)
			if err != nil {
				log.Printf("tracker %s for %s: %v", imei, police, err)
			} else {
				_, _ = pool.Exec(ctx, `UPDATE vehicles SET tracker_id=$1 WHERE id=$2`, trackerID, vehicleID)
				trackers++
			}
		}

		// Replace this vehicle's imported STNK/KIR documents (idempotent).
		if _, err := pool.Exec(ctx, `DELETE FROM documents WHERE tenant_id=$1 AND owner_type='vehicle' AND owner_id=$2 AND kind IN ('STNK','KIR')`, *tenant, vehicleID); err != nil {
			log.Printf("doc cleanup %d: %v", vehicleID, err)
		}
		if n := get("stnkNumber"); n != "" {
			if insertDoc(ctx, pool, *tenant, vehicleID, "STNK", n, get("stnkActivePeriodeTo")) {
				docs++
			}
		}
		if n := get("kirNumber"); n != "" {
			if insertDoc(ctx, pool, *tenant, vehicleID, "KIR", n, get("kirActivePeriodeTo")) {
				docs++
			}
		}
	}

	log.Printf("done: rows=%d imported=%d skipped=%d trackers_linked=%d documents=%d (tenant %d)", seen, imported, skipped, trackers, docs, *tenant)
}

// validIMEI accepts a 15-digit numeric IMEI, rejecting placeholders like "0".
func validIMEI(s string) bool {
	if len(s) != 15 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func insertDoc(ctx context.Context, pool *pgxpool.Pool, tenant, vehicleID int64, kind, number, expiryRaw string) bool {
	var expiry *time.Time
	if t, err := time.Parse(time.RFC3339, expiryRaw); err == nil {
		expiry = &t
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO documents (tenant_id, owner_type, owner_id, kind, title, number, expiry)
		VALUES ($1,'vehicle',$2,$3,$3,$4,$5)`,
		tenant, vehicleID, kind, number, expiry)
	if err != nil {
		log.Printf("doc %s for %d: %v", kind, vehicleID, err)
		return false
	}
	return true
}

func nullify(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
