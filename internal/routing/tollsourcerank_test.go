package routing

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The collapse in migration 0065 and the importer's upsert both rank sources
// in SQL, while the pricer ranks them in Go. Two definitions of "which figure
// wins" is exactly the kind of drift that reprices a lane silently, so this
// reads the migration and holds them to the same numbers.
func TestSourceRankMatchesSQL(t *testing.T) {
	const path = "../../db/migrations/0065_toll_single_source.sql"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	re := regexp.MustCompile(`WHEN '([a-z]+)'\s+THEN (\d+)`)
	got := map[string]int{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("rank for %q is not a number: %v", m[1], err)
		}
		got[m[1]] = n
	}
	if len(got) == 0 {
		t.Fatalf("no toll_source_rank cases found in %s", path)
	}

	for src, want := range SourcePriority {
		if have, ok := got[src]; !ok {
			t.Errorf("source %q ranks %d in Go but is absent from toll_source_rank()", src, want)
		} else if have != want {
			t.Errorf("source %q ranks %d in Go and %d in SQL", src, want, have)
		}
	}
	for src, have := range got {
		if _, ok := SourcePriority[src]; !ok {
			t.Errorf("source %q ranks %d in SQL but is absent from SourcePriority", src, have)
		}
	}
}

// A manual correction must beat a harvested figure and lose to a decree. If
// that order ever inverts, a person's fix would be silently overwritten by the
// next import.
func TestManualOutranksHarvestsButNotDecree(t *testing.T) {
	if SourcePriority["manual"] <= SourcePriority["jasamarga"] {
		t.Error("a hand correction must outrank the operator's table")
	}
	if SourcePriority["manual"] <= SourcePriority["mapid"] {
		t.Error("a hand correction must outrank a routing-derived figure")
	}
	if SourcePriority["manual"] >= SourcePriority["decree"] {
		t.Error("a decree is the legal figure and must outrank a hand correction")
	}
}
