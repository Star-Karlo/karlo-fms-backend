// Loads the harvested toll gates and tariffs into toll_gates and toll_tariffs.
//
//	go run ./cmd/import-toll \
//	  -gates   /path/to/toll-gates.csv \
//	  -tariffs /path/to/tariff-table.csv \
//	  -source  mapid
//
// Jasa Marga's own figures load differently, from the reference file Nathan
// keeps in TMS/NEW/MICROSERVICES/docs — see reference.go and -reference.
//
// INPUTS, both produced by the routing-service scripts:
//
//	toll-gates.csv    from extract-toll-gates.py — 1.747 OpenStreetMap toll
//	                  booths, 1.502 named, with coordinates.
//	tariff-table.csv  from check-tariffs.py — cells with a per-golongan price
//	                  AND A VERDICT. The verdict is carried through: a cell
//	                  marked 'review' failed a consistency check and must not
//	                  reach a quote, and the loader is not the place to
//	                  second-guess that.
//
// THE JOIN IS BY NORMALISED NAME, and it is the weak point. Tariffs name gates
// the way the pricing source does ("GT Serang Timur"); the gate table names
// them the way OpenStreetMap does ("Gerbang Tol Serang Timur"). Both are
// stripped of prefixes and parentheticals to a matching key. A tariff whose
// gate cannot be located is REPORTED AND SKIPPED rather than loaded without a
// position: a cell with no coordinates can never be matched to a route, so
// loading it would inflate the table's apparent coverage.
//
// Idempotent: re-running updates positions and re-upserts tariffs for the same
// source, so a corrected CSV can simply be loaded again.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
)

// Normalisation, matching scripts/match-toll-gates.py. Kept in step with it
// deliberately: if the two disagree, tariffs silently fail to find their gates.
var (
	prefixes  = []string{"gerbang tol ", "gerbang ", "gt. ", "gt ", "toll gate ", "toll ", "tol ", "ic "}
	suffixes  = []string{" toll plaza", " toll gate", " (gto)", " (gate in)", " (gate out)", " gto"}
	direction = regexp.MustCompile(`(?i)\s*\((arah|menuju|dari)[^)]*\)\s*`)
	parens    = regexp.MustCompile(`\(.*?\)`)
	spaces    = regexp.MustCompile(`\s+`)
)

func norm(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = direction.ReplaceAllString(s, " ")
	s = parens.ReplaceAllString(s, " ")
	s = strings.NewReplacer("-", " ", ".", " ", "/", " ").Replace(s)
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	for _, suf := range suffixes {
		s = strings.TrimSuffix(s, suf)
	}
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

func readCSV(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%s has no data rows", path)
	}
	head := rows[0]
	var out []map[string]string
	for _, rec := range rows[1:] {
		m := map[string]string{}
		for i, h := range head {
			if i < len(rec) {
				m[strings.TrimSpace(h)] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

const clusterM = 1500.0

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371008.8
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dLat, dLon := p2-p1, (lon2-lon1)*math.Pi/180
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(p1)*math.Cos(p2)*math.Pow(math.Sin(dLon/2), 2)
	return R * 2 * math.Asin(math.Sqrt(h))
}

type gate struct {
	name     string
	nameNorm string
	lat, lon float64
	n        int
	ruas     string
}

// clusterGates folds a plaza's booth lanes into one gate.
//
// Cikampek Utama is seven OSM nodes metres apart; loading each would multiply
// every gate and let a route "pass" the same plaza several times, which the
// cover would then try to charge for separately.
func clusterGates(rows []map[string]string) map[string]*gate {
	byKey := map[string][]*gate{}
	for _, r := range rows {
		name := strings.TrimSpace(r["name"])
		if name == "" {
			continue // unnamed gates cannot be matched to a tariff
		}
		lat, err1 := strconv.ParseFloat(r["lat"], 64)
		lon, err2 := strconv.ParseFloat(r["lon"], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		k := norm(name)
		g := &gate{name: name, nameNorm: k, lat: lat, lon: lon, n: 1, ruas: r["ruas"]}
		placed := false
		for _, c := range byKey[k] {
			if haversineM(c.lat, c.lon, lat, lon) <= clusterM {
				// Running centroid, so the order rows arrive in cannot change
				// which cluster a borderline node joins.
				c.lat = (c.lat*float64(c.n) + lat) / float64(c.n+1)
				c.lon = (c.lon*float64(c.n) + lon) / float64(c.n+1)
				c.n++
				placed = true
				break
			}
		}
		if !placed {
			byKey[k] = append(byKey[k], g)
		}
	}
	// One gate per normalised name — the table's unique key. Where a name has
	// several distinct plazas, the largest cluster wins and the rest are
	// reported: those are the Cikampek Utama 1/2/3 cases that need position or
	// a decree to separate, and guessing would attach a tariff to the wrong
	// plaza.
	out := map[string]*gate{}
	for k, cs := range byKey {
		best := cs[0]
		for _, c := range cs[1:] {
			if c.n > best.n {
				best = c
			}
		}
		if len(cs) > 1 {
			log.Printf("  note: %q has %d distinct plazas; using the largest (%d nodes)",
				k, len(cs), best.n)
		}
		out[k] = best
	}
	return out
}

func main() {
	gatesPath := flag.String("gates", "", "toll-gates.csv from extract-toll-gates.py")
	tariffsPath := flag.String("tariffs", "", "tariff-table.csv from check-tariffs.py")
	source := flag.String("source", "mapid", "provenance label stored on every tariff row")
	sourceRef := flag.String("source-ref", "", "a citation: a decree number, or an API path")
	dryRun := flag.Bool("dry-run", false, "report what would be loaded, write nothing")
	sqlOut := flag.String("sql-out", "",
		"write an idempotent .sql file instead of connecting — for a database only reachable from inside the VPC")
	referencePath := flag.String("reference", "",
		"Jasa Marga's tariff table (tarif_tol_jasamarga_<date>.csv) — replaces source 'jasamarga'; see reference.go")
	flag.Parse()

	if *referencePath != "" {
		runReference(*referencePath, *gatesPath, *sqlOut, *sourceRef)
		return
	}

	if *gatesPath == "" || *tariffsPath == "" {
		log.Fatal("both -gates and -tariffs are required")
	}

	gateRows, err := readCSV(*gatesPath)
	if err != nil {
		log.Fatalf("gates: %v", err)
	}
	gates := clusterGates(gateRows)
	log.Printf("%d gate row(s) -> %d clustered gate(s)", len(gateRows), len(gates))

	tariffRows, err := readCSV(*tariffsPath)
	if err != nil {
		log.Fatalf("tariffs: %v", err)
	}
	log.Printf("%d tariff row(s)", len(tariffRows))

	// Only gates a tariff actually references are loaded. The other ~1.300 OSM
	// booths are real but unpriceable, and loading them would mean a route
	// "passes" gates no tariff covers, which the cover refuses on — turning
	// every quote into a refusal.
	needed := map[string]bool{}
	for _, r := range tariffRows {
		if f := norm(r["from_gate"]); f != "" {
			needed[f] = true
		}
		if t := norm(r["to_gate"]); t != "" {
			needed[t] = true
		}
	}
	log.Printf("%d gate(s) referenced by a tariff", len(needed))

	var missing []string
	for k := range needed {
		if gates[k] == nil {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		log.Printf("WARNING: %d referenced gate(s) have no position and will be skipped:", len(missing))
		for i, m := range missing {
			if i == 12 {
				log.Printf("  ... and %d more", len(missing)-12)
				break
			}
			log.Printf("  %s", m)
		}
	}

	if *dryRun {
		loadable := 0
		for _, r := range tariffRows {
			f, t := norm(r["from_gate"]), norm(r["to_gate"])
			if gates[f] == nil || (t != "" && gates[t] == nil) {
				continue
			}
			loadable++
		}
		log.Printf("dry run: %d of %d tariff row(s) loadable", loadable, len(tariffRows))
		return
	}

	// SQL TO A FILE, for a database that cannot be reached from here.
	//
	// The RDS instance lives inside the VPC and is not publicly addressable,
	// so a laptop cannot connect — while the CSVs only exist on that laptop.
	// Emitting SQL resolves it without opening a security group: the file goes
	// through whichever channel already reaches the database, which for this
	// repo is the migrate task the deploy workflow runs.
	//
	// Written to be idempotent and re-runnable, the same as the direct path.
	if *sqlOut != "" {
		if err := writeSQL(*sqlOut, gates, needed, tariffRows, *source, *sourceRef); err != nil {
			log.Fatalf("sql: %v", err)
		}
		return
	}

	// CONNECTED ONLY NOW. A dry run checks the CSVs and the name matching,
	// which needs no database — and requiring one meant it could not be run
	// from a laptop, which is exactly where the files are.
	cfg := config.Load()
	ctx := context.Background()
	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	ids := map[string]int64{}
	for k := range needed {
		g := gates[k]
		if g == nil {
			continue
		}
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO toll_gates (name, name_norm, lat, lon, source, ruas)
			VALUES ($1, $2, $3, $4, $5, nullif($6, ''))
			ON CONFLICT (name_norm) DO UPDATE
			  SET name = excluded.name, lat = excluded.lat, lon = excluded.lon,
			      source = excluded.source, ruas = excluded.ruas,
			      updated_at = now()
			RETURNING id`,
			g.name, g.nameNorm, g.lat, g.lon, "osm", g.ruas).Scan(&id)
		if err != nil {
			log.Fatalf("gate %q: %v", k, err)
		}
		ids[k] = id
	}
	log.Printf("%d gate(s) loaded", len(ids))

	var loaded, skipped int
	for _, r := range tariffRows {
		fromNorm, toNorm := norm(r["from_gate"]), norm(r["to_gate"])
		entryID, ok := ids[fromNorm]
		if !ok {
			skipped++
			continue
		}
		var exitID *int64
		kind := "flat"
		if toNorm != "" {
			id, ok := ids[toNorm]
			if !ok {
				skipped++
				continue
			}
			exitID, kind = &id, "closed"
		}

		verdict := strings.TrimSpace(r["verdict"])
		if verdict != "usable" {
			verdict = "review"
		}

		for g := 1; g <= 5; g++ {
			raw := strings.TrimSpace(r[fmt.Sprintf("golongan_%d", g)])
			if raw == "" {
				continue
			}
			amount, err := strconv.ParseFloat(raw, 64)
			// A zero is not a tariff: it is a route that never joined the
			// network. The column's CHECK rejects it, so it is skipped here
			// rather than failing the whole load.
			if err != nil || amount <= 0 {
				continue
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO toll_tariffs
				  (kind, entry_gate, exit_gate, golongan, amount_rp,
				   source, source_ref, verdict, issues)
				VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8, nullif($9, ''))
				ON CONFLICT (entry_gate, COALESCE(exit_gate, 0), golongan, source)
				DO UPDATE SET amount_rp = excluded.amount_rp,
				              verdict = excluded.verdict,
				              issues = excluded.issues,
				              source_ref = excluded.source_ref,
				              fetched_at = now()`,
				kind, entryID, exitID, g, int64(amount),
				*source, *sourceRef, verdict, r["issues"]); err != nil {
				log.Fatalf("tariff %s -> %s golongan %d: %v", fromNorm, toNorm, g, err)
			}
			loaded++
		}
	}

	log.Printf("%d tariff figure(s) loaded, %d row(s) skipped for a missing gate", loaded, skipped)

	var usable, review int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE verdict = 'usable'),
		        count(*) FILTER (WHERE verdict = 'review') FROM toll_tariffs`).
		Scan(&usable, &review)
	log.Printf("toll_tariffs now holds %d usable and %d review figure(s)", usable, review)
	log.Printf("review rows are EXCLUDED from pricing, not merely flagged")
}

// sqlLit quotes a string for SQL.
//
// Doubling the single quotes rather than relying on a driver, because this
// output is not executed here — it is a file someone else applies, so a value
// that breaks out of its quotes would run as SQL in a context with none of
// this program's safeguards. Gate names come from OpenStreetMap and a provider
// API, so they are not trusted input.
func sqlLit(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func sqlNullable(v string) string {
	if strings.TrimSpace(v) == "" {
		return "NULL"
	}
	return sqlLit(v)
}

// writeSQL emits the gate and tariff upserts as a single transaction.
func writeSQL(path string, gates map[string]*gate, needed map[string]bool,
	tariffRows []map[string]string, source, sourceRef string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := func(format string, a ...any) {
		fmt.Fprintf(f, format+"\n", a...)
	}

	w("-- Toll gates and tariffs, generated by cmd/import-toll.")
	w("--")
	w("-- Idempotent: gates upsert on their normalised name and tariffs on")
	w("-- (entry, exit, golongan, source), so re-applying a corrected file is safe.")
	w("--")
	w("-- One transaction. A partial load would leave tariffs whose gates are")
	w("-- missing, and the pricer would then refuse every route through them")
	w("-- without anything saying why.")
	w("BEGIN;")
	w("")

	loadedGates := 0
	for k := range needed {
		g := gates[k]
		if g == nil {
			continue
		}
		w("INSERT INTO toll_gates (name, name_norm, lat, lon, source, ruas)")
		w("VALUES (%s, %s, %.6f, %.6f, 'osm', %s)",
			sqlLit(g.name), sqlLit(g.nameNorm), g.lat, g.lon, sqlNullable(g.ruas))
		w("ON CONFLICT (name_norm) DO UPDATE SET name = excluded.name,")
		w("  lat = excluded.lat, lon = excluded.lon, source = excluded.source,")
		w("  ruas = excluded.ruas, updated_at = now();")
		loadedGates++
	}
	w("")

	figures, skipped := 0, 0
	for _, r := range tariffRows {
		fromNorm, toNorm := norm(r["from_gate"]), norm(r["to_gate"])
		if gates[fromNorm] == nil || (toNorm != "" && gates[toNorm] == nil) {
			skipped++
			continue
		}
		kind := "flat"
		if toNorm != "" {
			kind = "closed"
		}
		verdict := strings.TrimSpace(r["verdict"])
		if verdict != "usable" {
			verdict = "review"
		}
		for g := 1; g <= 5; g++ {
			raw := strings.TrimSpace(r[fmt.Sprintf("golongan_%d", g)])
			if raw == "" {
				continue
			}
			amount, err := strconv.ParseFloat(raw, 64)
			if err != nil || amount <= 0 {
				continue
			}
			// Gate ids are looked up by name in the statement itself, so this
			// file does not depend on ids that differ between environments.
			exitExpr := "NULL"
			if toNorm != "" {
				exitExpr = "(SELECT id FROM toll_gates WHERE name_norm = " + sqlLit(toNorm) + ")"
			}
			w("INSERT INTO toll_tariffs (kind, entry_gate, exit_gate, golongan, amount_rp, source, source_ref, verdict, issues)")
			w("VALUES (%s, (SELECT id FROM toll_gates WHERE name_norm = %s), %s, %d, %d, %s, %s, %s, %s)",
				sqlLit(kind), sqlLit(fromNorm), exitExpr, g, int64(amount),
				sqlLit(source), sqlNullable(sourceRef), sqlLit(verdict), sqlNullable(r["issues"]))
			w("ON CONFLICT (entry_gate, COALESCE(exit_gate, 0), golongan, source)")
			w("DO UPDATE SET amount_rp = excluded.amount_rp, verdict = excluded.verdict,")
			w("  issues = excluded.issues, source_ref = excluded.source_ref, fetched_at = now();")
			figures++
		}
	}

	w("")
	w("COMMIT;")

	log.Printf("wrote %s", path)
	log.Printf("  %d gate(s), %d tariff figure(s), %d row(s) skipped for a missing gate",
		loadedGates, figures, skipped)
	log.Printf("  apply it with the same channel that applies migrations")
	return nil
}
