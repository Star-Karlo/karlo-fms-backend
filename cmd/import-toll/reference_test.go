package main

import (
	"os"
	"strings"
	"testing"
)

// Rows lifted from tarif_tol_jasamarga_2026-10-27.csv, each one a case the
// checks exist for.
const sample = "\ufeffWilayah,Gerbang Asal,Gerbang Tujuan,Sistem,Gol I,Gol II,Gol III,Gol IV,Gol V\n" +
	`Java,"Cikupa (Tangerang Barat)","Serang Barat",Tertutup,38000,59000,59000,76500,76500
Java,"Bekasi Barat 1","(flat — sistem terbuka)",Terbuka,7000,10500,10500,14000,14000
Java,"Buah batu","Kopo",Tertutup,4000,7000,7000,9500,9500
Java,"Buah Batu","Kopo",Tertutup,4000,7000,7000,9500,9500
Java,"Bangil","IC Lawang",Tertutup,3000,5000,5000,6000,6000
Java,"Bangil","Lawang",Tertutup,40000,61000,61000,80500,80500
Java,"Caruban","Ungaran",Tertutup,200000,300500,26300500,401500,401500
Java,"Cikampek Utama/Cikampek","Pekalongan",Tertutup,261000,409000,409000,536000,5360000
Java,"Subang","Cikedung",Tertutup,29000,29000,29000,60500,60500
Java,"Colomadu","Colomadu",Tertutup,16000,24000,24000,32000,32000
Java,"Padalarang","SS Padalarang",Tertutup,500,1000,1000,1000,1000
Java,"Kopo","Pasir Koja",Tertutup,2500,4500,4500,9500,9500
`

func load(t *testing.T, csv string) map[string]refCell {
	t.Helper()
	cells, _, err := readReference(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	checkReference(cells)
	out := map[string]refCell{}
	for _, c := range cells {
		out[c.EntryNorm+">"+c.ExitNorm] = c
	}
	return out
}

func TestReferenceChecks(t *testing.T) {
	cells := load(t, sample)

	usable := []string{
		"cikupa>serang barat",
		"bekasi barat 1>",          // flat
		"buah batu>kopo",           // listed twice, identical
		"bangil>lawang",            // NOT overwritten by the IC Lawang row
		"padalarang>ss padalarang", // Rp 500 is real here
		"kopo>pasir koja",          // the widest legitimate spread, 3.8x
	}
	for _, k := range usable {
		c, ok := cells[k]
		if !ok {
			t.Errorf("%s: missing", k)
			continue
		}
		if c.verdict() != "usable" {
			t.Errorf("%s: want usable, got review: %v", k, c.Issues)
		}
	}
	if cells["bangil>lawang"].Amount[0] != 40000 {
		t.Errorf("bangil>lawang took the interchange's figure: %v", cells["bangil>lawang"].Amount)
	}
	if !cells["bekasi barat 1>"].Flat {
		t.Error("an open-system row should load as flat")
	}

	review := []string{
		"caruban>ungaran",                    // Gol III with two extra digits
		"cikampek utama cikampek>pekalongan", // Gol V with one
		"subang>cikedung",                    // Gol II copied from Gol I
		"colomadu>colomadu",                  // same gate in and out
	}
	for _, k := range review {
		if c, ok := cells[k]; !ok || c.verdict() != "review" {
			t.Errorf("%s: want review, got %+v", k, c)
		}
	}
}

func TestReferenceLostTicketFare(t *testing.T) {
	var b strings.Builder
	b.WriteString("Gerbang Asal,Gerbang Tujuan,Sistem,Gol I,Gol II,Gol III,Gol IV,Gol V\n")
	entries := []string{"Gempol 1", "Gempol 3", "Kejapanan 1", "Lawang", "Malang"}
	exits := []string{"Grati", "Pasuruan"}
	for _, e := range entries {
		for _, x := range exits {
			b.WriteString(e + "," + x + ",Tertutup,229000,348000,348000,462000,462000\n")
		}
	}
	// The real fare beside them stays usable.
	b.WriteString("Bangil,Pasuruan,Tertutup,18500,28000,28000,37500,37500\n")

	cells := load(t, b.String())
	if c := cells["gempol 1>grati"]; c.verdict() != "review" {
		t.Errorf("the repeated maximum fare should be a review, got %v", c.Issues)
	}
	if c := cells["bangil>pasuruan"]; c.verdict() != "usable" {
		t.Errorf("a real fare was caught: %v", c.Issues)
	}
}

func TestReferenceSQLJoinsByName(t *testing.T) {
	cells, _, err := readReference(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	checkReference(cells)
	path := t.TempDir() + "/out.sql"
	if err := writeReferenceSQL(path, cells, nil, "docs/tarif.csv"); err != nil {
		t.Fatal(err)
	}
	sql := readFile(t, path)
	for _, want := range []string{
		"ON CONFLICT (entry_gate, COALESCE(exit_gate, 0), golongan)",
		"WHERE toll_source_rank(excluded.source) >= toll_source_rank(toll_tariffs.source);",
		"WHERE eg.name_norm = 'cikupa' AND xg.name_norm = 'serang barat'",
		"(VALUES (1, 38000), (2, 59000), (3, 59000), (4, 76500), (5, 76500))",
		"SELECT 'flat', eg.id, NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL lacks %q", want)
		}
	}
	// The blanket delete is gone with migration 0065: one row per cell means
	// it would remove winners whose alternative has been archived, and drop
	// any cell this file stopped covering.
	if strings.Contains(sql, "DELETE FROM toll_tariffs") {
		t.Error("a reference load must refresh in place, not delete every row of its source")
	}
	if strings.Contains(sql, "VALUES ('closed'") {
		t.Error("a pair must insert through a join, so an unplaced gate loads nothing instead of failing")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
