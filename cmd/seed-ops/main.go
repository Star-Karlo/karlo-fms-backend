// Command seed-ops seeds synthetic drivers, driver-score events (current month),
// and service schedules so Driver Scorecard and Maintenance are demonstrable.
// Replace with real driver data / maintenance records later.
//
//	go run ./cmd/seed-ops -tenant 1
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var firstNames = []string{"Budi", "Andi", "Slamet", "Agus", "Joko", "Bambang", "Eko", "Dedi", "Rudi", "Hendra", "Wahyu", "Tono", "Iwan", "Sugeng", "Yanto", "Priyo", "Rizal", "Fajar", "Gunawan", "Hadi"}
var lastNames = []string{"Santoso", "Wijaya", "Kurniawan", "Saputra", "Pratama", "Nugroho", "Hidayat", "Setiawan", "Firmansyah", "Utomo"}

var behaviors = []struct {
	code  string
	maxOc int
}{
	{"HARSH_ACCEL", 8}, {"HARSH_BRAKING", 8}, {"SHARP_TURN", 6}, {"OVERSPEED", 12},
	{"EXCEEDED_IDLE", 6}, {"EXCEEDED_PARKING", 5}, {"DMS_SMOKING", 3}, {"DMS_YAWN", 6},
	{"DMS_EYE_CLOSED", 4}, {"DMS_PHONE", 7}, {"DMS_DISTRACTION", 5}, {"DMS_OUT_OF_FRAME", 4},
}

func main() {
	tenant := flag.Int64("tenant", 1, "tenant id")
	dsn := flag.String("dsn", env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/fms_app"), "database URL (owner)")
	drivers := flag.Int("drivers", 40, "how many synthetic drivers to seed")
	schedules := flag.Int("schedules", 300, "how many service schedules to seed")
	flag.Parse()

	rng := rand.New(rand.NewSource(7))
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// Reset synthetic ops data for the tenant.
	for _, q := range []string{
		`DELETE FROM driver_score_events WHERE tenant_id=$1`,
		`DELETE FROM service_schedules WHERE tenant_id=$1`,
		`DELETE FROM employees WHERE tenant_id=$1 AND external_ref LIKE 'seed:%'`,
	} {
		if _, err := pool.Exec(ctx, q, *tenant); err != nil {
			log.Fatalf("reset: %v", err)
		}
	}

	// Drivers.
	period := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	var driverIDs []int64
	for i := 0; i < *drivers; i++ {
		name := firstNames[rng.Intn(len(firstNames))] + " " + lastNames[rng.Intn(len(lastNames))]
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO employees (tenant_id, full_name, role, status, external_ref, employee_no)
			VALUES ($1,$2,'driver','active',$3,$4) RETURNING id`,
			*tenant, name, fmt.Sprintf("seed:%d", i), fmt.Sprintf("D%03d", i+1)).Scan(&id)
		if err != nil {
			log.Fatalf("driver: %v", err)
		}
		driverIDs = append(driverIDs, id)
	}

	// Score events (current month) per driver.
	events := 0
	for _, did := range driverIDs {
		// trip distance
		trip := 300 + rng.Intn(2500)
		if _, err := pool.Exec(ctx, `
			INSERT INTO driver_score_events (tenant_id, driver_id, event_type, occurrences, score, period)
			VALUES ($1,$2,'trip_km',0,$3,$4)`, *tenant, did, trip, period); err != nil {
			log.Fatalf("trip: %v", err)
		}
		for _, b := range behaviors {
			oc := rng.Intn(b.maxOc + 1)
			if oc == 0 {
				continue
			}
			penalty := float64(oc) * (0.5 + rng.Float64()) // small penalty per occurrence
			if _, err := pool.Exec(ctx, `
				INSERT INTO driver_score_events (tenant_id, driver_id, event_type, occurrences, score, period)
				VALUES ($1,$2,$3,$4,$5,$6)`, *tenant, did, b.code, oc, penalty, period); err != nil {
				log.Fatalf("event: %v", err)
			}
			events++
		}
	}

	// Service schedules for a sample of vehicles.
	rows, err := pool.Query(ctx, `SELECT id, odometer FROM vehicles WHERE tenant_id=$1 AND status<>'archive' ORDER BY id LIMIT $2`, *tenant, *schedules)
	if err != nil {
		log.Fatalf("vehicles: %v", err)
	}
	type vh struct {
		id int64
	}
	var vs []vh
	for rows.Next() {
		var v vh
		var odo *float64
		_ = rows.Scan(&v.id, &odo)
		vs = append(vs, v)
	}
	rows.Close()

	components := []string{"Engine Oil", "Brake Pads", "Tire Rotation", "Air Filter", "Transmission Oil", "Coolant"}
	statuses := []string{"normal", "normal", "normal", "due_soon", "overdue"}
	sched := 0
	for _, v := range vs {
		comp := components[rng.Intn(len(components))]
		status := statuses[rng.Intn(len(statuses))]
		nextDue := period.AddDate(0, 0, rng.Intn(60)-15) // some overdue (past), some future
		if _, err := pool.Exec(ctx, `
			INSERT INTO service_schedules (tenant_id, vehicle_id, component, scheduler, km_interval, km_reminder, day_interval, day_reminder, next_due_at, status)
			VALUES ($1,$2,$3,'floating',$4,$5,$6,$7,$8,$9)`,
			*tenant, v.id, comp, 10000, 500, 90, 7, nextDue, status); err != nil {
			log.Fatalf("schedule: %v", err)
		}
		sched++
	}

	log.Printf("done: %d drivers, %d score events, %d service schedules (tenant %d)", len(driverIDs), events, sched, *tenant)
	fmt.Println("note: SYNTHETIC ops data for demo — replace with real driver + maintenance records.")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
