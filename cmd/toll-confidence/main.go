// Command toll-confidence answers one question: on a real journey, does
// Karlo's own toll pricing produce a complete answer, and how close is it to
// what MAPID charges?
//
// It runs the SHIPPING code — routing.GatesAlong, TollTable.CoverAll,
// CoverStructure.PriceFor — against gates and tariffs read out of the
// migration files, so the measurement is of the thing that will run, not of a
// reimplementation of it. Geometry comes from a JSON file per lane so the
// routing engine is called once, by hand, and not from here.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/karlo/fms-backend/internal/routing"
)

var (
	gateRe5 = regexp.MustCompile(`INSERT INTO toll_gates \(name, name_norm, lat, lon, source\) VALUES \('((?:[^']|'')*)', '((?:[^']|'')*)', (-?[\d.]+), (-?[\d.]+)`)
	gateRe6 = regexp.MustCompile(`INSERT INTO toll_gates \(name, name_norm, lat, lon, source, ruas\)\nVALUES \('((?:[^']|'')*)', '((?:[^']|'')*)', (-?[\d.]+), (-?[\d.]+)`)
	oldTar  = regexp.MustCompile(`INSERT INTO toll_tariffs[^;]*?VALUES \('(closed|flat)', \(SELECT id FROM toll_gates WHERE name_norm = '((?:[^']|'')*)'\), (?:\(SELECT id FROM toll_gates WHERE name_norm = '((?:[^']|'')*)'\)|NULL), (\d+), (\d+), '([^']*)', (?:'(?:[^']|'')*'|NULL), '(usable|review)'`)
	newTar  = regexp.MustCompile(`(?s)INSERT INTO toll_tariffs[^;]*?SELECT '(closed|flat)',.*?'(usable|review)', (?:NULL|'(?:[^']|'')*')\nFROM .*?\(VALUES ((?:\(\d+, \d+\), ?)*\(\d+, \d+\))\) AS v\(g, amount\)\nWHERE eg\.name_norm = '((?:[^']|'')*)'(?: AND xg\.name_norm = '((?:[^']|'')*)')?;`)
	golRe   = regexp.MustCompile(`\((\d+), (\d+)\)`)
)

type cellKey struct {
	kind, entry, exit string
}

func load(dir string) ([]routing.TollGate, []routing.TollCell, error) {
	files, err := filepath.Glob(filepath.Join(dir, "00[56]*.sql"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)

	gates := map[string]routing.TollGate{}
	cells := map[cellKey]map[int]int64{}
	var id int64

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, nil, err
		}
		s := string(raw)

		// 0063 replaces the whole source rather than adding to it; honouring
		// that here is the difference between measuring the database and
		// measuring every row ever written to it.
		if strings.Contains(s, "DELETE FROM toll_tariffs WHERE source = 'jasamarga'") {
			for k := range cells {
				if strings.HasPrefix(k.kind, "jasamarga|") {
					delete(cells, k)
				}
			}
		}

		for _, m := range append(gateRe5.FindAllStringSubmatch(s, -1), gateRe6.FindAllStringSubmatch(s, -1)...) {
			lat, _ := strconv.ParseFloat(m[3], 64)
			lon, _ := strconv.ParseFloat(m[4], 64)
			id++
			gates[m[2]] = routing.TollGate{ID: id, Name: strings.ReplaceAll(m[1], "''", "'"), NameNorm: m[2], Lat: lat, Lon: lon}
		}
		for _, m := range oldTar.FindAllStringSubmatch(s, -1) {
			if m[7] != "usable" {
				continue
			}
			g, _ := strconv.Atoi(m[4])
			amt, _ := strconv.ParseInt(m[5], 10, 64)
			k := cellKey{m[6] + "|" + m[1], m[2], m[3]}
			if cells[k] == nil {
				cells[k] = map[int]int64{}
			}
			cells[k][g] = amt
		}
		for _, m := range newTar.FindAllStringSubmatch(s, -1) {
			if m[2] != "usable" {
				continue
			}
			k := cellKey{"jasamarga|" + m[1], m[4], m[5]}
			if cells[k] == nil {
				cells[k] = map[int]int64{}
			}
			for _, p := range golRe.FindAllStringSubmatch(m[3], -1) {
				g, _ := strconv.Atoi(p[1])
				amt, _ := strconv.ParseInt(p[2], 10, 64)
				cells[k][g] = amt
			}
		}
	}

	var gl []routing.TollGate
	for _, g := range gates {
		gl = append(gl, g)
	}
	var cl []routing.TollCell
	for k, amounts := range cells {
		kind := strings.SplitN(k.kind, "|", 2)
		cl = append(cl, routing.TollCell{
			EntryNorm: k.entry, ExitNorm: k.exit, Flat: kind[1] == "flat",
			AmountRp: amounts, Verdict: "usable", Source: kind[0],
		})
	}
	return gl, cl, nil
}

type lane struct {
	Name     string           `json:"name"`
	Geometry [][2]float64     `json:"geometry"`
	MapidRp  map[string]int64 `json:"mapid_rp,omitempty"`
	TollRuns []tollRun        `json:"toll_runs,omitempty"`
}

func main() {
	dir := flag.String("migrations", "db/migrations", "where the toll migrations live")
	lanesPath := flag.String("lanes", "", "JSON array of {name, geometry, mapid_rp}")
	gol := flag.Int("golongan", 5, "the class to price")
	noFilter := flag.Bool("raw", false, "skip the transaction filter — price every gate the route passes")
	plaza := flag.Bool("plaza", false, "collapse the several booths of one plaza into one charge")
	joinBase := flag.Bool("join-base", false, "join tariffs to gates on the plaza base name, not the booth name")
	verbose := flag.Bool("v", false, "list every gate the route passes, with how far off the line it sits")
	flag.Parse()

	gates, cells, err := load(*dir)
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	if *joinBase {
		for i := range gates {
			gates[i].NameNorm = plazaBase(gates[i].Name)
		}
		for i := range cells {
			cells[i].EntryNorm = plazaBase(cells[i].EntryNorm)
			if cells[i].ExitNorm != "" {
				cells[i].ExitNorm = plazaBase(cells[i].ExitNorm)
			}
		}
	}
	table := routing.NewTollTable(cells)
	closed, flat := table.Cells()
	fmt.Printf("loaded %d gates, %d closed cells, %d flat cells\n\n", len(gates), closed, flat)

	raw, err := os.ReadFile(*lanesPath)
	if err != nil {
		log.Fatalf("lanes: %v", err)
	}
	var lanes []lane
	if err := json.Unmarshal(raw, &lanes); err != nil {
		log.Fatalf("lanes: %v", err)
	}

	var complete, total int
	fmt.Printf("%-26s %7s %7s %12s %12s %8s\n", "LANE", "GATES", "CHARGE", "OURS (gol "+strconv.Itoa(*gol)+")", "MAPID", "DIFF")
	for _, l := range lanes {
		total++
		passed := routing.GatesAlong(l.Geometry, gates)
		if *plaza {
			passed = collapsePlazas(passed)
		}
		if !*noFilter {
			passed = transactionGates(passed, l.TollRuns)
		}
		st := table.CoverAll(passed)
		amount, charges, ok := st.PriceFor(*gol)

		ours := "-"
		if ok {
			ours = fmt.Sprintf("%d", amount)
		}
		mapid := "-"
		diff := ""
		if m, has := l.MapidRp[strconv.Itoa(*gol)]; has {
			mapid = fmt.Sprintf("%d", m)
			if ok && m > 0 {
				diff = fmt.Sprintf("%+.1f%%", 100*float64(amount-m)/float64(m))
			}
		}
		if ok && !st.Incomplete() {
			complete++
		}
		fmt.Printf("%-26s %7d %7d %12s %12s %8s\n", trunc(l.Name, 26), len(passed), len(charges), ours, mapid, diff)
		if len(st.Unpriced) > 0 {
			fmt.Printf("    unpriced: %s\n", strings.Join(st.Unpriced, ", "))
		}
		if *verbose {
			for _, g := range passed {
				fmt.Printf("      %7.1f km  offset %5.1f m  %s\n", g.AlongM/1000, g.OffsetM, g.Gate.Name)
			}
		}
	}
	fmt.Printf("\ncomplete: %d of %d lanes\n", complete, total)
}

// tollRun is a contiguous stretch of the route on one toll ruas, from the
// routing service. Defined here rather than in internal/routing because the
// filter below is a HYPOTHESIS under measurement, not shipping behaviour.
type tollRun struct {
	Ruas  string  `json:"ruas,omitempty"`
	FromM float64 `json:"from_m"`
	ToM   float64 `json:"to_m"`
}

const tollRunAnchorM = 1500.0

// transactionGates keeps the gate nearest each run boundary and discards the
// rest.
//
// MEASURED AND NOT GOOD ENOUGH, 2026-10-28. It fixes the right problem — a
// drive-by booth is not a transaction, and this cuts Jakarta-Bandung from 23
// gates to 5 — but picking the NEAREST gate to a boundary picks a slip-road
// booth as often as the barrier, so the total swung from +85% to -49%. The
// gate count is right and the gate choice is wrong. Kept here, out of the
// shipping path, so the next attempt has something to beat.
func transactionGates(passed []routing.PassedGate, runs []tollRun) []routing.PassedGate {
	if len(runs) == 0 || len(passed) == 0 {
		return passed
	}
	keep := make(map[int]bool, 2*len(runs))
	for _, r := range runs {
		for _, edge := range [2]float64{r.FromM, r.ToM} {
			best, bestD := -1, tollRunAnchorM
			for i, g := range passed {
				d := g.AlongM - edge
				if d < 0 {
					d = -d
				}
				if d <= bestD {
					best, bestD = i, d
				}
			}
			if best >= 0 {
				keep[best] = true
			}
		}
	}
	out := make([]routing.PassedGate, 0, len(keep))
	for i, g := range passed {
		if keep[i] {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return passed
	}
	return out
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// plazaBase strips the lane number and the varied spellings of "gerbang tol"
// off a gate name, so the several booths of one plaza share a base.
//
// Cibitung 2, 3, 4 and 6 are four lanes of ONE barrier, and the gate table
// holds each separately because that is how MAPID's export listed them. A
// pricer that treats them as four gates charges four times at a plaza a truck
// taps once.
var plazaPrefix = regexp.MustCompile(`(?i)^(gerbang tol|toll gate|gt)\s+`)
var plazaSuffix = regexp.MustCompile(`\s+\d+$`)

func plazaBase(name string) string {
	s := plazaPrefix.ReplaceAllString(strings.TrimSpace(name), "")
	s = plazaSuffix.ReplaceAllString(s, "")
	return strings.ToLower(strings.TrimSpace(s))
}

const plazaSpanM = 3000.0

// collapsePlazas keeps one booth per plaza: the one sitting closest to the
// driven line, which is the lane the truck actually went through.
//
// Gates are grouped only when they share a base name AND sit within
// plazaSpanM of each other along the route, so two genuinely separate plazas
// that happen to share a name (a pair at either end of a short ruas) are not
// merged.
func collapsePlazas(passed []routing.PassedGate) []routing.PassedGate {
	var out []routing.PassedGate
	for _, g := range passed {
		base := plazaBase(g.Gate.Name)
		merged := false
		for i := range out {
			if plazaBase(out[i].Gate.Name) != base {
				continue
			}
			if g.AlongM-out[i].AlongM > plazaSpanM {
				continue
			}
			merged = true
			if g.OffsetM < out[i].OffsetM {
				out[i] = g
			}
			break
		}
		if !merged {
			out = append(out, g)
		}
	}
	return out
}
