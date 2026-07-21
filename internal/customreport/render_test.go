package customreport

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
)

// A document that exercises the things that break renderers: a section that
// failed, a section with no rows, numbers mixed with text, characters that are
// legal in a report value and illegal in XML or a sheet name, and two sections
// wanting the same sheet name.
func sampleDoc() Doc {
	return Doc{
		Title:      "Weekly Fleet Report",
		Notes:      "Ops summary <for> R&D",
		RangeLabel: "last 7 days (15 Jul 2026 to 21 Jul 2026)",
		Generated:  time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Sections: []DocSection{
			{
				Title:   "Alerts",
				Headers: []string{"Code", "Count", "Plate"},
				Rows: [][]string{
					{"BATTERY_DROP", "31", "B-2559-XX"},
					{"OVERSPEED", "7", "H-9925-OA & sons"},
				},
				Numeric: []bool{false, true, false},
			},
			{Title: "Alerts", Note: "Could not be generated: vehicle_id is required"},
			{Title: "No/Data:Sheet*", Headers: []string{"A"}, Rows: nil, Numeric: []bool{false}},
		},
	}
}

func TestRenderAllFormatsProduceOpenableFiles(t *testing.T) {
	for _, f := range []Format{FormatXLSX, FormatPDF, FormatDOCX, FormatCSV} {
		t.Run(string(f), func(t *testing.T) {
			out, err := Render(sampleDoc(), f)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(out.Body) == 0 {
				t.Fatal("empty document")
			}
			if !strings.HasSuffix(out.Filename, "."+string(f)) {
				t.Errorf("filename %q does not end in .%s", out.Filename, f)
			}
			// Magic bytes: the cheapest way to catch "we wrote something, but
			// not that format".
			switch f {
			case FormatXLSX, FormatDOCX:
				if !bytes.HasPrefix(out.Body, []byte("PK")) {
					t.Error("not a zip container")
				}
			case FormatPDF:
				if !bytes.HasPrefix(out.Body, []byte("%PDF")) {
					t.Error("not a PDF")
				}
			}
		})
	}
}

// The docx is hand-written OOXML, so nothing but a parser proves it is
// well-formed. A stray & or < in a vehicle name makes Word refuse the file
// outright rather than render it oddly.
func TestDOCXIsWellFormedXML(t *testing.T) {
	out, err := Render(sampleDoc(), FormatDOCX)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Body), int64(len(out.Body)))
	if err != nil {
		t.Fatalf("not a readable zip: %v", err)
	}

	want := map[string]bool{"[Content_Types].xml": false, "_rels/.rels": false, "word/document.xml": false}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		// Every part must parse; a malformed one is an unopenable document.
		if err := xml.Unmarshal(body, new(any)); err != nil {
			t.Errorf("%s is not well-formed XML: %v", f.Name, err)
		}
		if f.Name == "word/document.xml" {
			s := string(body)
			if !strings.Contains(s, "R&amp;D") {
				t.Error("ampersand was not escaped; Word will reject the file")
			}
			if strings.Contains(s, "<for>") {
				t.Error("angle brackets from a report value leaked in unescaped")
			}
			if !strings.Contains(s, "Could not be generated") {
				t.Error("a failed section must still explain itself in the document")
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing required part %s", name)
		}
	}
}

// Sheet names have rules a report title does not: 31 chars, no []:*?/\, and
// unique. Breaking any of them fails the whole workbook, not just one sheet.
func TestSheetNamesAreLegalAndUnique(t *testing.T) {
	used := map[string]int{}
	names := []string{
		sheetName("Alerts", 0, used),
		sheetName("Alerts", 1, used),
		sheetName("No/Data:Sheet*", 2, used),
		sheetName("", 3, used),
		sheetName(strings.Repeat("very long section title ", 5), 4, used),
	}
	seen := map[string]bool{}
	for _, n := range names {
		if len(n) > 31 {
			t.Errorf("%q is %d chars, max 31", n, len(n))
		}
		if strings.ContainsAny(n, `[]:*?/\`) {
			t.Errorf("%q contains an illegal character", n)
		}
		if n == "" {
			t.Error("empty sheet name")
		}
		if seen[n] {
			t.Errorf("duplicate sheet name %q", n)
		}
		seen[n] = true
	}
}

// A value beginning =, + or @ is executed as a formula when Excel opens a CSV.
// Report rows carry free text that originated as user input.
func TestCSVDefusesFormulaInjection(t *testing.T) {
	doc := sampleDoc()
	doc.Sections = []DocSection{{
		Title:   "Injected",
		Headers: []string{"Name"},
		Rows:    [][]string{{`=cmd|'/c calc'!A1`}, {"+1234"}, {"@SUM(A1)"}, {"normal"}},
	}}
	out, err := Render(doc, FormatCSV)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out.Body)
	for _, bad := range []string{"\n=cmd", "\n+1234", "\n@SUM"} {
		if strings.Contains(s, bad) {
			t.Errorf("unescaped formula start %q survived", bad)
		}
	}
	if !strings.Contains(s, "normal") {
		t.Error("ordinary values must pass through untouched")
	}
	if !bytes.HasPrefix(out.Body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("missing UTF-8 BOM; Excel on Windows will mis-decode the file")
	}
}

// A filename is echoed into Content-Disposition, and the title is user input.
func TestFilenameCannotCarryHeaderInjection(t *testing.T) {
	for _, in := range []string{
		`report"; filename="evil.exe`,
		"line\r\nX-Injected: 1",
		"../../etc/passwd",
		"",
		strings.Repeat("x", 300),
	} {
		got := safeFilename(in)
		if strings.ContainsAny(got, "\"\r\n/\\;") {
			t.Errorf("safeFilename(%q) = %q, still dangerous", in, got)
		}
		if got == "" {
			t.Errorf("safeFilename(%q) returned empty", in)
		}
		if len(got) > 60 {
			t.Errorf("safeFilename(%q) too long: %d", in, len(got))
		}
	}
}

// PDF core fonts are Latin-1. Anything outside must become a visible
// placeholder rather than corrupt the content stream or vanish silently.
func TestPDFTextIsLatin1Safe(t *testing.T) {
	got := tr("Jalan Merdeka — 中文 \x01ctl\ttab")
	for _, r := range got {
		if r > 0xFF {
			t.Errorf("rune %q survived conversion", r)
		}
		if r < 0x20 && r != ' ' {
			t.Errorf("control character %q survived", r)
		}
	}
	if !strings.Contains(got, "-") {
		t.Error("em dash should degrade to a hyphen, not disappear")
	}
	if !strings.Contains(got, "?") {
		t.Error("unrepresentable characters should leave a visible placeholder")
	}
}
