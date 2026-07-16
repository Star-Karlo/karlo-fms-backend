// Command seed-alerts generates synthetic safety/telematics alerts for a sample
// of the fleet so Control Tower, Notification, and the Alerts report are
// populated and demonstrable. Replace with real ADAS/DMS ingestion later.
//
//	go run ./cmd/seed-alerts -tenant 1 -vehicles 200 -days 2
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/alert"
)

var cities = []struct {
	name     string
	lat, lon float64
}{
	{"Semarang, Jawa Tengah", -6.9667, 110.4167},
	{"Surabaya, Jawa Timur", -7.2575, 112.7521},
	{"Solo, Jawa Tengah", -7.5561, 110.8317},
	{"Kudus, Jawa Tengah", -6.8048, 110.8405},
	{"Gresik, Jawa Timur", -7.1556, 112.6534},
}

func main() {
	tenant := flag.Int64("tenant", 1, "tenant id")
	dsn := flag.String("dsn", env("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/fms_app"), "database URL (owner)")
	vehicles := flag.Int("vehicles", 200, "how many vehicles to attach alerts to")
	days := flag.Int("days", 2, "spread alerts over the past N days")
	avgPer := flag.Int("per", 8, "average alerts per vehicle")
	reset := flag.Bool("reset", true, "delete existing alerts for the tenant first")
	flag.Parse()

	rng := rand.New(rand.NewSource(42))
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if *reset {
		if _, err := pool.Exec(ctx, `DELETE FROM alerts WHERE tenant_id=$1`, *tenant); err != nil {
			log.Fatalf("reset: %v", err)
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT id, tracker_id, current_driver_id
		FROM vehicles
		WHERE tenant_id=$1 AND tracker_id IS NOT NULL AND status<>'archive'
		ORDER BY id LIMIT $2`, *tenant, *vehicles)
	if err != nil {
		log.Fatalf("query vehicles: %v", err)
	}
	type veh struct {
		id        int64
		trackerID *int64
		driverID  *int64
	}
	var vs []veh
	for rows.Next() {
		var v veh
		if err := rows.Scan(&v.id, &v.trackerID, &v.driverID); err != nil {
			log.Fatalf("scan: %v", err)
		}
		vs = append(vs, v)
	}
	rows.Close()
	if len(vs) == 0 {
		log.Fatal("no vehicles with trackers found — import the fleet first")
	}

	now := time.Now()
	batch := &pgx.Batch{}
	total := 0
	for _, v := range vs {
		n := 1 + rng.Intn(*avgPer*2) // 1 .. 2*avg
		for i := 0; i < n; i++ {
			at := alert.Taxonomy[rng.Intn(len(alert.Taxonomy))]
			c := cities[rng.Intn(len(cities))]
			lat := c.lat + (rng.Float64()-0.5)*0.15
			lon := c.lon + (rng.Float64()-0.5)*0.15

			// Bias most events into the last 24h, a few into the last hour.
			var occurred time.Time
			switch r := rng.Float64(); {
			case r < 0.15:
				occurred = now.Add(-time.Duration(rng.Intn(60)) * time.Minute)
			case r < 0.7:
				occurred = now.Add(-time.Duration(rng.Intn(24)) * time.Hour).Add(-time.Duration(rng.Intn(60)) * time.Minute)
			default:
				occurred = now.Add(-time.Duration(rng.Intn(*days*24)) * time.Hour)
			}

			status := "unchecked"
			switch r := rng.Float64(); {
			case r < 0.25:
				status = "checked"
			case r < 0.30:
				status = "false_alert"
			}

			var actual, limit interface{}
			if at.Code == "OVERSPEED" {
				limit = 80
				actual = 80 + rng.Intn(40)
			}

			batch.Queue(`
				INSERT INTO alerts (tenant_id, vehicle_id, tracker_id, driver_id, source, device_source,
				                    alert_code, alert_name, severity, occurred_at, actual_value, limit_value,
				                    lat, lon, address, check_status)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
				*tenant, v.id, v.trackerID, v.driverID, at.Source, at.Source,
				at.Code, at.Name, at.Severity, occurred, actual, limit,
				lat, lon, c.name, status)
			total++
		}
	}

	br := pool.SendBatch(ctx, batch)
	for i := 0; i < total; i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			log.Fatalf("insert alert %d: %v", i, err)
		}
	}
	br.Close()

	log.Printf("done: %d synthetic alerts across %d vehicles (tenant %d)", total, len(vs), *tenant)
	fmt.Println("note: these are SYNTHETIC alerts for demo — replace with real ADAS/DMS ingestion.")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
