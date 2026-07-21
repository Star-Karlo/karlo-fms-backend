package customreport

import (
	"strings"
	"testing"
	"time"
)

func narratedDoc() Doc {
	rows := make([][]string, 0, 60)
	for i := 0; i < 60; i++ {
		rows = append(rows, []string{"B-1234-XX", "10"})
	}
	rows[0][0] = "H-9999-ZZ"
	return Doc{
		Title:      "Weekly Ops",
		RangeLabel: "last 7 days",
		Generated:  time.Now(),
		Sections: []DocSection{
			{Title: "Alerts", Headers: []string{"Plate", "Count"}, Rows: rows, Numeric: []bool{false, true}},
			{Title: "Empty", Headers: []string{"A"}, Rows: nil, Numeric: []bool{false}},
		},
	}
}

// The digest is the ONLY thing the model sees, so what it contains is the
// boundary of what can honestly be written. It must carry real aggregates and
// must not carry the whole table.
func TestDigestSummarisesRatherThanShippingRows(t *testing.T) {
	d := BuildDigest(narratedDoc(), Spec{Sections: []Section{{Source: "vehicle-alerts-summary"}, {Source: "driver-management"}}})

	if len(d.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(d.Sections))
	}
	a := d.Sections[0]
	if a.Rows != 60 {
		t.Errorf("row count = %d, want 60", a.Rows)
	}
	if len(a.Samples) > digestSampleRows {
		t.Errorf("digest carries %d sample rows; it must summarise, not ship the table", len(a.Samples))
	}

	// Numeric column: real aggregates, or the model has nothing true to say.
	count := a.Columns[1]
	if count.Kind != "number" || count.Sum == nil || *count.Sum != 600 {
		t.Errorf("Count column = %+v, want a numeric sum of 600", count)
	}
	if count.Max == nil || *count.Max != 10 {
		t.Errorf("max = %v, want 10", count.Max)
	}

	// Text column: the values that dominate, so "mostly B-1234-XX" is sayable.
	plate := a.Columns[0]
	if len(plate.Top) == 0 || plate.Top[0].Value != "B-1234-XX" || plate.Top[0].Count != 59 {
		t.Errorf("top values = %+v, want B-1234-XX with 59", plate.Top)
	}

	// A source's own description travels with it: without it an empty roster
	// was described as "no changes in driver management", which it does not
	// measure.
	if d.Sections[1].About == "" {
		t.Error("section digest has no description; the model can only guess what the report means")
	}
}

// Prose belongs in documents that are read, not in spreadsheets that are
// filtered — and the spec can override either way.
func TestNarrativeDefaultsByFormat(t *testing.T) {
	var spec Spec
	for _, tc := range []struct {
		format Format
		want   bool
	}{{FormatPDF, true}, {FormatDOCX, true}, {FormatXLSX, false}, {FormatCSV, false}} {
		if got := wantsNarrative(spec, tc.format); got != tc.want {
			t.Errorf("%s: narrative = %v, want %v", tc.format, got, tc.want)
		}
	}
	on, off := true, false
	if !wantsNarrative(Spec{Narrative: &on}, FormatCSV) {
		t.Error("an explicit opt-in should win over the format default")
	}
	if wantsNarrative(Spec{Narrative: &off}, FormatPDF) {
		t.Error("an explicit opt-out should win over the format default")
	}
}

func TestApplyNarrativeTrimsTablesAndSaysSo(t *testing.T) {
	doc := narratedDoc()
	applyNarrative(&doc, &Narrative{
		Summary:  "Thirty-one alerts.",
		Sections: []string{"About alerts.", "Nothing here."},
	})

	if doc.Summary != "Thirty-one alerts." {
		t.Errorf("summary = %q", doc.Summary)
	}
	if doc.Sections[0].Commentary != "About alerts." {
		t.Errorf("commentary = %q", doc.Sections[0].Commentary)
	}
	// An executive summary followed by sixty rows is not an executive summary,
	// but a table that just stops is worse than one that explains itself.
	if len(doc.Sections[0].Rows) != narrativeTableRows {
		t.Errorf("rows = %d, want trimmed to %d", len(doc.Sections[0].Rows), narrativeTableRows)
	}
	if !strings.Contains(doc.Sections[0].Note, "60") || !strings.Contains(doc.Sections[0].Note, "Excel") {
		t.Errorf("note should say how many rows were held back and where to get them, got %q", doc.Sections[0].Note)
	}
}

// Commentary is matched to sections BY POSITION, so a short list must pad
// rather than shift paragraph 2 onto section 3.
func TestShortNarrativeDoesNotMisalign(t *testing.T) {
	doc := narratedDoc()
	applyNarrative(&doc, &Narrative{Summary: "s", Sections: []string{"only one"}})
	if doc.Sections[0].Commentary != "only one" {
		t.Errorf("first commentary = %q", doc.Sections[0].Commentary)
	}
	if doc.Sections[1].Commentary != "" {
		t.Errorf("second section got %q; a missing paragraph must stay missing", doc.Sections[1].Commentary)
	}
}

func TestNilNarrativeLeavesTheDocumentAlone(t *testing.T) {
	doc := narratedDoc()
	before := len(doc.Sections[0].Rows)
	applyNarrative(&doc, nil)
	if doc.Summary != "" || len(doc.Sections[0].Rows) != before {
		t.Error("a failed narration must leave the tables untouched")
	}
}
