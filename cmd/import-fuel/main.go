// Command import-fuel loads official refuelling points (SPBU) for Java and
// Sumatra from OpenStreetMap into the fuel_station table.
//
// Run it as the DATABASE OWNER, not as fms_app — 0024 deliberately revokes
// write access from the application role because fuel_station is shared across
// every tenant. This is the same role migrations run as:
//
//	DATABASE_URL="postgres://postgres:...@host:5432/fms_app" go run ./cmd/import-fuel
//
// Idempotent: stations upsert on (osm_type, osm_id), so re-running refreshes
// existing rows in place instead of duplicating the country. Safe to run on a
// schedule to pick up new mappings.
//
// Flags:
//
//	-region jawa|sumatra|all   which bounding boxes to fetch (default all)
//	-dry-run                   fetch and report, write nothing
//	-endpoint URL              alternate Overpass mirror
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// region is one Overpass bounding box, always intersected with Indonesia's
// national boundary (see query()).
//
// THE BBOX ALONE IS NOT ENOUGH, and assuming otherwise produced a genuinely
// wrong dataset on the first run: any box wide enough to hold Sumatra also
// holds Peninsular Malaysia (1.2N–6.7N) and Singapore, because they sit
// directly across the Strait of Malacca rather than north of it. That import
// pulled in 3,166 Malaysian and 289 Singaporean stations — Petronas, Caltex,
// BHPetrol, SPC — which is 47% of the rows and indistinguishable from real
// coverage once drawn on a map. No rectangle can separate the two land masses,
// so the country filter is load-bearing, not a refinement.
//
// The two boxes overlap slightly at the Sunda Strait; the upsert key makes
// that harmless, and `island` reflects whichever region wrote last for the
// handful of stations in the overlap.
type region struct {
	Name string
	// south, west, north, east — Overpass bbox order.
	S, W, N, E float64
}

var regions = []region{
	// Java incl. Madura and Bali's western approach.
	{"jawa", -9.0, 105.0, -5.7, 114.8},
	// Sumatra incl. Bangka, Nias and Riau. Extends over Malaysia and Singapore
	// by necessity; the ISO3166-1 area filter removes them.
	{"sumatra", -6.2, 95.0, 6.0, 107.0},
}

const defaultEndpoint = "https://overpass-api.de/api/interpreter"

// Overpass is a donated community service with no SLA. Being a good citizen is
// not optional: a descriptive User-Agent, a generous server-side timeout, and
// backoff on 429/504 rather than hammering.
const userAgent = "karlo-fms-fuel-import/1.0 (fleet management; contact: ops@karlo.id)"

type overpassResponse struct {
	Elements []element `json:"elements"`
}

type element struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Lat  float64           `json:"lat"`
	Lon  float64           `json:"lon"`
	// Ways and relations have no lat/lon of their own; `out center` adds this.
	Center *struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"center"`
	Tags map[string]string `json:"tags"`
}

// point resolves an element's coordinates, preferring its own over the
// computed centroid. Returns ok=false for anything with neither, which
// otherwise lands at (0,0) in the Gulf of Guinea.
func (e element) point() (lat, lon float64, ok bool) {
	if e.Lat != 0 || e.Lon != 0 {
		return e.Lat, e.Lon, true
	}
	if e.Center != nil {
		return e.Center.Lat, e.Center.Lon, true
	}
	return 0, 0, false
}

// brandPatterns folds free-text OSM tags into the closed set stored in
// fuel_station.brand. Order matters: the first match wins, so more specific
// names must precede substrings of themselves.
//
// Matching is over brand + operator + name concatenated, because Indonesian
// SPBU are tagged inconsistently — some carry brand=Pertamina, many carry only
// name="SPBU 34.12708" with no brand at all, and a few put the operator in
// name. Checking all three recovers far more than trusting `brand` alone.
var brandPatterns = []struct {
	Canonical string
	Match     []string
}{
	// Pertashop is Pertamina's own small-format outlet, so it belongs with the
	// parent brand rather than in "other".
	{"pertamina", []string{"pertamina", "pertashop", "spbu", "pertamina dex"}},
	{"shell", []string{"shell"}},
	{"bp", []string{"bp-akr", "bp akr", "bp "}},
	{"vivo", []string{"vivo"}},
	{"akr", []string{"akr"}},
	{"total", []string{"total", "totalenergies"}},
}

// skipPatterns drop entries that are tagged amenity=fuel but are not places a
// truck can actually refuel.
//
// "Pertamini" is the one that matters: an informal roadside stand with a
// hand-operated or small electric pump, selling by the litre from jerry cans.
// Despite the name it has no connection to Pertamina, and OSM has thousands of
// them. Including them would inflate apparent coverage precisely in the rural
// areas where real SPBU are sparsest — the worst place to mislead someone
// planning a route. Note the name is NOT caught by the "pertamina" pattern
// above (…mini vs …mina), so it needs its own rule.
// "Pom mini" is the same thing under a generic name, and "bensin eceran" is
// literally petrol sold by the bottle. Note "pom bensin" is NOT here — that is
// the ordinary Indonesian term for a filling station and is perfectly real.
var skipPatterns = []string{"pertamini", "pom mini", "eceran"}

func shouldSkip(tags map[string]string) bool {
	hay := strings.ToLower(strings.Join([]string{
		tags["brand"], tags["operator"], tags["name"],
	}, " "))
	for _, p := range skipPatterns {
		if strings.Contains(hay, p) {
			return true
		}
	}
	return false
}

// normalizeBrand returns the canonical brand and whether it was recognised.
func normalizeBrand(tags map[string]string) (canonical, raw string) {
	raw = firstNonEmpty(tags["brand"], tags["operator"], tags["name"])
	hay := strings.ToLower(strings.Join([]string{
		tags["brand"], tags["operator"], tags["name"],
	}, " "))
	for _, p := range brandPatterns {
		for _, m := range p.Match {
			if strings.Contains(hay, m) {
				return p.Canonical, raw
			}
		}
	}
	// Kept rather than dropped. An unbranded fuel station is still a place a
	// truck can refuel, and discarding it would silently shrink coverage in
	// exactly the rural areas where mapping is thinnest.
	return "other", raw
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func query(r region) string {
	// Two filters, both required:
	//
	//   (area.id) — Indonesia's admin_level=2 boundary. Without it the Sumatra
	//               box returns Malaysia and Singapore too (see `region`).
	//   (bbox)    — keeps the query regional, so Overpass is not asked to scan
	//               the whole archipelago and time out.
	//
	// `out center tags` gives ways/relations a centroid; without it every
	// station mapped as a building footprint arrives with no coordinates.
	return fmt.Sprintf(`[out:json][timeout:300];
area["ISO3166-1"="ID"][admin_level=2]->.id;
(
  node["amenity"="fuel"](area.id)(%f,%f,%f,%f);
  way["amenity"="fuel"](area.id)(%f,%f,%f,%f);
);
out center tags;`, r.S, r.W, r.N, r.E, r.S, r.W, r.N, r.E)
}

func fetch(ctx context.Context, endpoint string, r region) ([]element, error) {
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		if attempt > 1 {
			// Overpass returns 429 when busy and 504 when a query outlasts its
			// slot. Both clear on their own; backing off is the correct and
			// only remedy.
			wait := time.Duration(attempt*attempt) * 15 * time.Second
			log.Printf("  retry %d in %s (%v)", attempt, wait, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		body := strings.NewReader(url.Values{"data": {query(r)}}.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", userAgent)

		resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		payload, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(payload), 200))
			continue
		}

		var out overpassResponse
		if err := json.Unmarshal(payload, &out); err != nil {
			lastErr = fmt.Errorf("decoding response: %w", err)
			continue
		}
		return out.Elements, nil
	}
	return nil, fmt.Errorf("after 4 attempts: %w", lastErr)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	var (
		regionFlag = flag.String("region", "all", "jawa | sumatra | all")
		dryRun     = flag.Bool("dry-run", false, "fetch and report without writing")
		endpoint   = flag.String("endpoint", defaultEndpoint, "Overpass API endpoint")
	)
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" && !*dryRun {
		log.Fatal("DATABASE_URL is required (run as the database owner, not fms_app)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var selected []region
	for _, r := range regions {
		if *regionFlag == "all" || *regionFlag == r.Name {
			selected = append(selected, r)
		}
	}
	if len(selected) == 0 {
		log.Fatalf("unknown region %q; valid: jawa, sumatra, all", *regionFlag)
	}

	var pool *pgxpool.Pool
	if !*dryRun {
		var err error
		pool, err = pgxpool.New(ctx, dsn)
		if err != nil {
			log.Fatalf("connecting: %v", err)
		}
		defer pool.Close()

		// Fail loudly and immediately if pointed at the app role, rather than
		// after a full Overpass fetch.
		var role string
		if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
			log.Fatalf("connecting: %v", err)
		}
		if role == "fms_app" {
			log.Fatal("connected as fms_app, which has no write access to fuel_station — use the owner credentials from scripts/migrate.sh")
		}
		log.Printf("connected as %s", role)
	}

	brandTotals := map[string]int{}
	grandTotal, written := 0, 0

	for _, r := range selected {
		log.Printf("fetching %s…", r.Name)
		start := time.Now()
		elements, err := fetch(ctx, *endpoint, r)
		if err != nil {
			log.Fatalf("fetching %s: %v", r.Name, err)
		}
		log.Printf("  %d elements in %s", len(elements), time.Since(start).Round(time.Second))

		skipped, informal := 0, 0
		for _, e := range elements {
			lat, lon, ok := e.point()
			if !ok {
				skipped++
				continue
			}
			if shouldSkip(e.Tags) {
				informal++
				continue
			}
			brand, raw := normalizeBrand(e.Tags)
			brandTotals[brand]++
			grandTotal++

			if *dryRun {
				continue
			}
			// COALESCE on update: a later import that has lost a name tag must
			// not blank a name we already hold.
			_, err := pool.Exec(ctx, `
INSERT INTO fuel_station (osm_type, osm_id, brand, brand_raw, name, lat, lon, island)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (osm_type, osm_id) DO UPDATE SET
  brand      = EXCLUDED.brand,
  brand_raw  = COALESCE(EXCLUDED.brand_raw, fuel_station.brand_raw),
  name       = COALESCE(EXCLUDED.name, fuel_station.name),
  lat        = EXCLUDED.lat,
  lon        = EXCLUDED.lon,
  island     = EXCLUDED.island,
  updated_at = now()`,
				e.Type, e.ID, brand, nullIfEmpty(raw), nullIfEmpty(e.Tags["name"]),
				lat, lon, r.Name)
			if err != nil {
				log.Fatalf("upserting %s/%d: %v", e.Type, e.ID, err)
			}
			written++
		}
		if skipped > 0 {
			log.Printf("  skipped %d element(s) with no usable coordinates", skipped)
		}
		if informal > 0 {
			log.Printf("  skipped %d informal seller(s) (Pertamini and similar)", informal)
		}
	}

	log.Printf("--- %d stations (%d written) ---", grandTotal, written)
	brands := make([]string, 0, len(brandTotals))
	for b := range brandTotals {
		brands = append(brands, b)
	}
	sort.Slice(brands, func(i, j int) bool { return brandTotals[brands[i]] > brandTotals[brands[j]] })
	for _, b := range brands {
		log.Printf("  %-10s %d", b, brandTotals[b])
	}
	if *dryRun {
		log.Print("dry run — nothing written")
	}
}

func nullIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t := strings.TrimSpace(s)
	return &t
}
