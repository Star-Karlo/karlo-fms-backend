package main

// The reference load: Jasa Marga's tariff table as Nathan keeps it.
//
//	go run ./cmd/import-toll \
//	  -reference /path/to/tarif_tol_jasamarga_2026-10-27.csv \
//	  -sql-out   db/migrations/00NN_toll_reference.sql \
//	  [-gates    /path/to/gate-positions.csv]
//
// THE FILE IS THE TRUTH FOR SOURCE 'jasamarga'. The master copy lives at
// TMS/NEW/MICROSERVICES/docs/tarif_tol_jasamarga_<date>.csv; every figure FMS
// prices under 'jasamarga' comes from it. So the generated SQL deletes that
// source's rows first and loads the file whole, rather than upserting over an
// older harvest and leaving its stale cells behind. Other sources (mapid) are
// untouched: they fill the roads the reference does not list, and lose to it
// wherever it does (SourcePriority).
//
// Its columns: Wilayah, Gerbang Asal, Gerbang Tujuan, Sistem, Gol I..Gol V.
// An open-system gate has "(flat — sistem terbuka)" as its exit.
//
// A GATE WITH NO POSITION LOADS NOTHING, silently and correctly: each insert
// joins toll_gates by normalised name, so a pair whose gate is not placed yet
// produces no row instead of failing the transaction. When positions arrive,
// pass them with -gates and generate again; the same file then loads more.
//
// ROWS ARE CHECKED, NOT TRUSTED. The 2026-10-27 file prices Caruban -> Ungaran
// Golongan III at Rp 26.300.500 and lists the Gempol network's lost-ticket fare
// as the price of dozens of real pairs. A cell that fails a check is loaded
// with verdict 'review', which the pricer excludes — so the figure is kept and
// visible, and never billed.

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

var golonganNames = [5]string{"I", "II", "III", "IV", "V"}

// refCell is one priced pair (or one flat gate) from the reference.
type refCell struct {
	EntryName, ExitName string // as written in the file
	EntryNorm, ExitNorm string
	Flat                bool
	Amount              [5]int64
	Issues              []string
}

func (c refCell) verdict() string {
	if len(c.Issues) == 0 {
		return "usable"
	}
	return "review"
}

func (c refCell) label() string {
	if c.Flat {
		return c.EntryName + " (flat)"
	}
	return c.EntryName + " -> " + c.ExitName
}

// readReference parses the file into one cell per normalised pair. notes says
// what was skipped and why, for the run's log.
func readReference(r io.Reader) (cells []refCell, notes []string, err error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("the reference has no data rows")
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		// A spreadsheet export starts with a byte-order mark, which would
		// otherwise make the first column unfindable by name.
		col[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	want := []string{"Gerbang Asal", "Gerbang Tujuan", "Gol I", "Gol II", "Gol III", "Gol IV", "Gol V"}
	for _, h := range want {
		if _, ok := col[h]; !ok {
			return nil, nil, fmt.Errorf("the reference has no %q column", h)
		}
	}

	index := map[string]int{}
	for n, rec := range rows[1:] {
		line := n + 2
		get := func(h string) string {
			if i := col[h]; i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		c := refCell{EntryName: get("Gerbang Asal"), ExitName: get("Gerbang Tujuan")}
		c.Flat = strings.HasPrefix(c.ExitName, "(flat")
		if c.Flat {
			c.ExitName = ""
		}

		// AN INTERCHANGE IS NOT A GATE. "IC Lawang" normalises to "lawang",
		// the gate of the same town, and Bangil -> IC Lawang (Rp 3.000) would
		// then overwrite Bangil -> Lawang (Rp 40.000). Nobody pays at an
		// interchange, so these rows have nothing to attach to.
		if isInterchange(c.EntryName) || isInterchange(c.ExitName) {
			notes = append(notes, fmt.Sprintf("line %d: %s is an interchange, not a gate", line, c.label()))
			continue
		}

		bad := false
		for g, h := range want[2:] {
			v, err := strconv.ParseInt(get(h), 10, 64)
			if err != nil {
				notes = append(notes, fmt.Sprintf("line %d: %s Gol %s %q is not a number", line, c.label(), golonganNames[g], get(h)))
				bad = true
				break
			}
			c.Amount[g] = v
		}
		if bad {
			continue
		}

		c.EntryNorm = norm(c.EntryName)
		if !c.Flat {
			c.ExitNorm = norm(c.ExitName)
		}
		if c.EntryNorm == "" || (!c.Flat && c.ExitNorm == "") {
			notes = append(notes, fmt.Sprintf("line %d: a gate name is empty", line))
			continue
		}

		// The file repeats pairs under spellings that normalise alike
		// ("Buah batu" / "Buah Batu"). Identical figures are one cell; different
		// ones mean the file contradicts itself, which is a review.
		key := c.EntryNorm + "\x00" + c.ExitNorm
		if i, ok := index[key]; ok {
			if cells[i].Amount != c.Amount {
				cells[i].Issues = append(cells[i].Issues, fmt.Sprintf(
					"listed twice with different figures (%s and %s)",
					rupiahs(cells[i].Amount), rupiahs(c.Amount)))
			}
			continue
		}
		index[key] = len(cells)
		cells = append(cells, c)
	}
	return cells, notes, nil
}

func isInterchange(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "ic ")
}

// minPlausibleRp is the least a real charge can be. The reference itself
// prices SS Padalarang <-> Padalarang Golongan I at Rp 500, so the floor sits
// there rather than at check-tariffs.py's Rp 1.000; it is for catching a
// placeholder like Rp 1, not short connectors.
const minPlausibleRp = 500

// maxClassSpread bounds the dearest class against Golongan I. The widest real
// spread in the 2026-10-27 file is 3.8x (Kopo -> Pasir Koja); every typed extra
// digit is 14x or more.
const maxClassSpread = 5

// checkReference marks the cells that must not be billed on.
func checkReference(cells []refCell) {
	for i := range cells {
		c := &cells[i]
		a := c.Amount
		for g := 0; g < 5; g++ {
			if a[g] < minPlausibleRp {
				c.Issues = append(c.Issues, fmt.Sprintf("Gol %s Rp %d is below Rp %d", golonganNames[g], a[g], minPlausibleRp))
			}
		}
		// Classes never get cheaper as the vehicle gets bigger. II = III and
		// IV = V are normal (published tables group them); a drop is not.
		for g := 1; g < 5; g++ {
			if a[g] < a[g-1] {
				c.Issues = append(c.Issues, fmt.Sprintf("Gol %s (%d) is below Gol %s (%d)",
					golonganNames[g], a[g], golonganNames[g-1], a[g-1]))
			}
		}
		if a[0] > 0 {
			for g := 1; g < 5; g++ {
				if a[g] > maxClassSpread*a[0] {
					c.Issues = append(c.Issues, fmt.Sprintf("Gol %s (%d) is %dx Gol I — a typed extra digit?",
						golonganNames[g], a[g], a[g]/a[0]))
				}
			}
		}
		// Every other pair above Rp 5.000 prices Golongan II above I. Subang ->
		// Cikedung at 29.000 for I, II and III, beside 48.000 the other way, is
		// a copied cell.
		if a[0] >= 5000 && a[1] == a[0] {
			c.Issues = append(c.Issues, fmt.Sprintf("Gol II equals Gol I (%d)", a[0]))
		}
		if !c.Flat && c.EntryNorm == c.ExitNorm {
			c.Issues = append(c.Issues, "enters and exits at the same gate — the fare for turning back, not a journey")
		}
	}

	// THE LOST-TICKET FARE. A closed system charges its longest journey to a
	// vehicle that cannot show where it entered, and the file lists that
	// figure as if it were the price of ordinary pairs: Gempol 1 -> Bangil at
	// Rp 229.000 when Bangil is the next gate along. One set of five figures,
	// large, repeated across many pairs from many gates, is that fare.
	type spread struct {
		cells   []int
		entries map[string]bool
	}
	seen := map[[5]int64]*spread{}
	for i, c := range cells {
		if c.Flat || c.Amount[0] < 100000 {
			continue
		}
		s := seen[c.Amount]
		if s == nil {
			s = &spread{entries: map[string]bool{}}
			seen[c.Amount] = s
		}
		s.cells = append(s.cells, i)
		s.entries[c.EntryNorm] = true
	}
	for amount, s := range seen {
		if len(s.cells) < 10 || len(s.entries) < 5 {
			continue
		}
		for _, i := range s.cells {
			cells[i].Issues = append(cells[i].Issues, fmt.Sprintf(
				"Rp %d is a lost-ticket maximum fare, listed for %d pairs from %d gates",
				amount[0], len(s.cells), len(s.entries)))
		}
	}
}

func rupiahs(a [5]int64) string {
	s := make([]string, 5)
	for i, v := range a {
		s[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(s, "/")
}

// readGatePositions reads name,lat,lon rows — the sheet the team fills for the
// gates the reference names and nobody has placed yet. Blank rows are gates
// still waiting; they are skipped, not failed.
func readGatePositions(path string) (map[string]*gate, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	out := map[string]*gate{}
	for _, r := range rows {
		name := strings.TrimSpace(r["name"])
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(r["lat"]), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(r["lon"]), 64)
		if name == "" || err1 != nil || err2 != nil {
			continue
		}
		// Indonesia spans roughly 6N-11S, 95E-141E. A swapped pair or a
		// dropped minus sign lands outside it and would place a gate in the
		// sea, where no route passes and its tariffs look loaded but are dead.
		if lat < -11.5 || lat > 6.5 || lon < 94.5 || lon > 141.5 {
			return nil, fmt.Errorf("gate %q at %v,%v is outside Indonesia — lat and lon swapped?", name, lat, lon)
		}
		out[norm(name)] = &gate{name: name, nameNorm: norm(name), lat: lat, lon: lon}
	}
	return out, nil
}

// runReference writes the reference load as one SQL transaction.
func runReference(refPath, gatesPath, sqlOut, sourceRef string) {
	if sqlOut == "" {
		// The database is inside the VPC; the file is on a laptop. The deploy's
		// migrate task is the channel that reaches it.
		log.Fatal("-reference writes SQL: -sql-out is required")
	}
	f, err := os.Open(refPath)
	if err != nil {
		log.Fatalf("reference: %v", err)
	}
	cells, notes, err := readReference(f)
	f.Close()
	if err != nil {
		log.Fatalf("reference: %v", err)
	}
	for _, n := range notes {
		log.Printf("  skipped %s", n)
	}
	checkReference(cells)

	var placed map[string]*gate
	if gatesPath != "" {
		if placed, err = readGatePositions(gatesPath); err != nil {
			log.Fatalf("gates: %v", err)
		}
	}

	if sourceRef == "" {
		sourceRef = refPath
	}
	if err := writeReferenceSQL(sqlOut, cells, placed, sourceRef); err != nil {
		log.Fatalf("sql: %v", err)
	}

	review := 0
	for _, c := range cells {
		if len(c.Issues) > 0 {
			review++
			log.Printf("  review: %s %s — %s", c.label(), rupiahs(c.Amount), strings.Join(c.Issues, "; "))
		}
	}
	log.Printf("wrote %s: %d cell(s), %d usable, %d for review, %d row(s) skipped, %d gate position(s)",
		sqlOut, len(cells), len(cells)-review, review, len(notes), len(placed))
	log.Printf("  cells whose gates have no position load nothing until those gates are placed")
}

func writeReferenceSQL(path string, cells []refCell, placed map[string]*gate, sourceRef string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := func(format string, a ...any) { fmt.Fprintf(f, format+"\n", a...) }

	w("-- Jasa Marga toll tariffs from the reference file, generated by")
	w("-- cmd/import-toll -reference. Source: %s", strings.ReplaceAll(sourceRef, "\n", " "))
	w("--")
	w("-- Replaces every 'jasamarga' row with the file's figures, so the source holds")
	w("-- exactly what the reference says. Other sources are untouched. A pair whose")
	w("-- gate has no position joins to nothing and inserts no row. Cells that failed")
	w("-- a check load as verdict 'review' and are never priced.")
	w("BEGIN;")
	w("")
	for _, g := range placed {
		w("INSERT INTO toll_gates (name, name_norm, lat, lon, source) VALUES (%s, %s, %.6f, %.6f, 'manual')",
			sqlLit(g.name), sqlLit(g.nameNorm), g.lat, g.lon)
		w("ON CONFLICT (name_norm) DO UPDATE SET name = excluded.name, lat = excluded.lat,")
		w("  lon = excluded.lon, source = excluded.source, updated_at = now();")
	}
	w("")
	w("DELETE FROM toll_tariffs WHERE source = 'jasamarga';")
	w("")
	for _, c := range cells {
		vals := make([]string, 5)
		for g, v := range c.Amount {
			vals[g] = fmt.Sprintf("(%d, %d)", g+1, v)
		}
		issues := "NULL"
		if len(c.Issues) > 0 {
			issues = sqlLit(strings.Join(c.Issues, "; "))
		}
		if c.Flat {
			w("INSERT INTO toll_tariffs (kind, entry_gate, exit_gate, golongan, amount_rp, source, source_ref, verdict, issues)")
			w("SELECT 'flat', eg.id, NULL, v.g, v.amount, 'jasamarga', %s, %s, %s", sqlLit(sourceRef), sqlLit(c.verdict()), issues)
			w("FROM toll_gates eg, (VALUES %s) AS v(g, amount)", strings.Join(vals, ", "))
			w("WHERE eg.name_norm = %s;", sqlLit(c.EntryNorm))
			continue
		}
		w("INSERT INTO toll_tariffs (kind, entry_gate, exit_gate, golongan, amount_rp, source, source_ref, verdict, issues)")
		w("SELECT 'closed', eg.id, xg.id, v.g, v.amount, 'jasamarga', %s, %s, %s", sqlLit(sourceRef), sqlLit(c.verdict()), issues)
		w("FROM toll_gates eg, toll_gates xg, (VALUES %s) AS v(g, amount)", strings.Join(vals, ", "))
		w("WHERE eg.name_norm = %s AND xg.name_norm = %s;", sqlLit(c.EntryNorm), sqlLit(c.ExitNorm))
	}
	w("")
	w("COMMIT;")
	return nil
}
