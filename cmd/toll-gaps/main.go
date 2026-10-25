// Lists the missing toll tariffs, as a work list to look up.
//
//	go run ./cmd/toll-gaps -gates toll-gates.csv -tariffs tariff-table.csv -out gaps.csv
//
// WHY A WHOLE-TABLE ANSWER. A dispatch names the first gate it cannot price —
// "no tariff covers Cibitung 4" — which fixes that one route and says nothing
// about the next. This finds every gap at once, so the lookups happen in one
// sitting rather than one dispatch at a time.
//
// WHAT A GAP IS, and it is not "a gate with no tariff". Pricing walks the
// gates a route passes and needs a cell leading ONWARD from each. Cibitung 4
// HAS a tariff — Telaga Asih -> Cibitung 4 — and still broke a 459 km route,
// because nothing priced a journey starting there. So the real gaps are gates
// that only ever appear as an exit, cells excluded for failing a check, and
// cells priced for some vehicle classes only.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

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
		if strings.HasSuffix(s, suf) {
			s = s[:len(s)-len(suf)]
		}
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
	var out []map[string]string
	for _, rec := range rows[1:] {
		m := map[string]string{}
		for i, h := range rows[0] {
			if i < len(rec) {
				m[strings.TrimSpace(h)] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

type gapRow struct{ gate, to, golongan, reason string }

func main() {
	gatesPath := flag.String("gates", "", "toll-gates.csv")
	tariffsPath := flag.String("tariffs", "", "tariff-table.csv")
	out := flag.String("out", "", "also write the work list as CSV")
	flag.Parse()
	if *gatesPath == "" || *tariffsPath == "" {
		log.Fatal("both -gates and -tariffs are required")
	}

	gateRows, err := readCSV(*gatesPath)
	if err != nil {
		log.Fatalf("gates: %v", err)
	}
	// Proper names by matching key, so a gap is reported as something
	// searchable rather than as a normalised key.
	display := map[string]string{}
	for _, r := range gateRows {
		if n := strings.TrimSpace(r["name"]); n != "" {
			display[norm(n)] = n
		}
	}
	name := func(k string) string {
		if n, ok := display[k]; ok {
			return n
		}
		return k
	}

	tariffRows, err := readCSV(*tariffsPath)
	if err != nil {
		log.Fatalf("tariffs: %v", err)
	}

	onward := map[string]bool{} // gate has a cell leading onward
	seen := map[string]bool{}   // gate appears at all
	cellClasses := map[string]map[int]bool{}
	cellUsable := map[string]bool{}

	for _, r := range tariffRows {
		from, to := norm(r["from_gate"]), norm(r["to_gate"])
		if from == "" {
			continue
		}
		seen[from] = true
		if to != "" {
			seen[to] = true
		}
		onward[from] = true // a flat fee also counts: it charges from here

		cell := from + " > " + to
		if strings.TrimSpace(r["verdict"]) == "usable" {
			cellUsable[cell] = true
		}
		if cellClasses[cell] == nil {
			cellClasses[cell] = map[int]bool{}
		}
		for g := 1; g <= 5; g++ {
			v := strings.TrimSpace(r[fmt.Sprintf("golongan_%d", g)])
			if v == "" {
				continue
			}
			// The same Rp 1.000 floor the checker uses: below it, the figure
			// is a placeholder and the class is effectively unpriced.
			if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 1000 {
				cellClasses[cell][g] = true
			}
		}
	}

	var gaps []gapRow

	for g := range seen {
		if !onward[g] {
			gaps = append(gaps, gapRow{
				gate:   name(g),
				reason: "only ever an exit — need a tariff FROM this gate",
			})
		}
	}
	for cell, cls := range cellClasses {
		parts := strings.SplitN(cell, " > ", 2)
		from := name(parts[0])
		to := ""
		if len(parts) > 1 && parts[1] != "" {
			to = name(parts[1])
		}
		if !cellUsable[cell] {
			gaps = append(gaps, gapRow{
				gate: from, to: to,
				reason: "excluded — failed a consistency check, needs a second source",
			})
			continue
		}
		var missing []string
		for g := 1; g <= 5; g++ {
			if !cls[g] {
				missing = append(missing, strconv.Itoa(g))
			}
		}
		if len(missing) > 0 {
			gaps = append(gaps, gapRow{
				gate: from, to: to,
				golongan: strings.Join(missing, ","),
				reason:   "priced for some classes only",
			})
		}
	}

	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].reason != gaps[j].reason {
			return gaps[i].reason < gaps[j].reason
		}
		return gaps[i].gate < gaps[j].gate
	})

	counts := map[string]int{}
	for _, g := range gaps {
		counts[g.reason]++
	}
	fmt.Printf("%d gap(s)\n\n", len(gaps))
	var reasons []string
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		fmt.Printf("  %4d  %s\n", counts[r], r)
	}
	fmt.Println()
	for _, g := range gaps {
		line := g.gate
		if g.to != "" {
			line += " -> " + g.to
		}
		if g.golongan != "" {
			line += "  [golongan " + g.golongan + "]"
		}
		fmt.Println(line)
	}

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatalf("out: %v", err)
		}
		defer f.Close()
		w := csv.NewWriter(f)
		_ = w.Write([]string{"gate", "to_gate", "missing_golongan", "reason"})
		for _, g := range gaps {
			_ = w.Write([]string{g.gate, g.to, g.golongan, g.reason})
		}
		w.Flush()
		fmt.Printf("\n-> %s\n", *out)
	}
}
