package customreport

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/karlo/fms-backend/internal/report"
)

// Narrative reports: prose over the numbers, not instead of them.
//
// A spreadsheet is read by filtering and sorting; a PDF or Word document is
// read start to finish by someone who wants to know what happened. Handing
// them 4,000 rows and calling it a report puts the analysis back on the reader.
//
// THE NARRATIVE IS WRITTEN AT RENDER TIME, from the digest of the data that
// this run actually produced — never stored in the spec. A saved report re-runs
// against new data, so prose written when it was created would describe last
// month's numbers beside this month's tables, and be believed. That is the
// single most important property here.
//
// The model sees a DIGEST, not the rows: counts, sums, ranges and the top few
// values per column. That keeps a 20,000-row report the same cost as a 20-row
// one, and it is also the honesty mechanism — it can only cite figures that
// were computed, because those are the only ones it is given.

// Narrator writes the prose. Implemented in the AI package; kept as an
// interface here so this package does not depend on Bedrock, and so a report
// still renders (without prose) when the AI is unavailable.
type Narrator interface {
	Narrate(ctx context.Context, d Digest) (*Narrative, error)
}

// Digest is the compact, factual view of a rendered report.
type Digest struct {
	Title    string          `json:"title"`
	Period   string          `json:"period"`
	Notes    string          `json:"notes,omitempty"`
	Sections []SectionDigest `json:"sections"`
}

type SectionDigest struct {
	Title  string `json:"title"`
	Source string `json:"source"`
	// About is the source report's own description.
	//
	// Without it the model has only a key to reason from, and guesses at
	// meaning: an empty driver-management section was described as "no changes
	// in driver management" when that report is a ROSTER, not a changelog —
	// empty means no drivers, not no change. A wrong reading stated fluently is
	// exactly what this feature must not produce.
	About   string         `json:"about,omitempty"`
	Rows    int            `json:"rows"`
	Note    string         `json:"note,omitempty"`
	Columns []ColumnDigest `json:"columns,omitempty"`
	Samples [][]string     `json:"sample_rows,omitempty"`
}

// ColumnDigest is one column reduced to what can be said about it truthfully.
type ColumnDigest struct {
	Label string `json:"label"`
	Kind  string `json:"kind"` // number | text

	// Numbers.
	Sum *float64 `json:"sum,omitempty"`
	Avg *float64 `json:"avg,omitempty"`
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`

	// Text: the values that actually dominate.
	Top []ValueCount `json:"top,omitempty"`
}

type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Narrative is what comes back.
type Narrative struct {
	// Summary is the executive summary — what happened, in a few sentences.
	Summary string `json:"summary"`
	// Sections is commentary per section, in the same order. Shorter than the
	// summary; it explains the table beneath it.
	Sections []string `json:"sections"`
}

// digestSampleRows is how many rows the model sees verbatim. Enough to know
// what a row looks like; few enough that it cannot mistake the sample for the
// data and start counting from it.
const digestSampleRows = 3

// digestTopValues bounds the per-column value list.
const digestTopValues = 5

// BuildDigest reduces a rendered document to what can be stated about it.
func BuildDigest(doc Doc, spec Spec) Digest {
	d := Digest{Title: doc.Title, Period: doc.RangeLabel, Notes: doc.Notes}

	for i, sec := range doc.Sections {
		source := ""
		if i < len(spec.Sections) {
			source = spec.Sections[i].Source
		}
		sd := SectionDigest{Title: sec.Title, Source: source, About: describe(source), Rows: len(sec.Rows), Note: sec.Note}

		for c, header := range sec.Headers {
			col := ColumnDigest{Label: header, Kind: "text"}
			numeric := c < len(sec.Numeric) && sec.Numeric[c]

			if numeric {
				col.Kind = "number"
				var sum float64
				var n int
				var min, max float64
				for _, row := range sec.Rows {
					if c >= len(row) {
						continue
					}
					v, err := strconv.ParseFloat(row[c], 64)
					if err != nil {
						continue
					}
					if n == 0 || v < min {
						min = v
					}
					if n == 0 || v > max {
						max = v
					}
					sum += v
					n++
				}
				if n > 0 {
					avg := sum / float64(n)
					col.Sum, col.Avg, col.Min, col.Max = &sum, &avg, &min, &max
				}
			} else {
				counts := map[string]int{}
				for _, row := range sec.Rows {
					if c < len(row) && strings.TrimSpace(row[c]) != "" {
						counts[row[c]]++
					}
				}
				for v, n := range counts {
					col.Top = append(col.Top, ValueCount{Value: v, Count: n})
				}
				sort.Slice(col.Top, func(a, b int) bool {
					if col.Top[a].Count != col.Top[b].Count {
						return col.Top[a].Count > col.Top[b].Count
					}
					return col.Top[a].Value < col.Top[b].Value
				})
				if len(col.Top) > digestTopValues {
					col.Top = col.Top[:digestTopValues]
				}
			}
			sd.Columns = append(sd.Columns, col)
		}

		for r := 0; r < len(sec.Rows) && r < digestSampleRows; r++ {
			sd.Samples = append(sd.Samples, sec.Rows[r])
		}
		d.Sections = append(d.Sections, sd)
	}
	return d
}

// describe looks up a source's own description from the report catalog.
func describe(key string) string {
	for _, m := range report.Catalog {
		if m.Key == key {
			return m.Description
		}
	}
	return ""
}

// wantsNarrative decides whether prose belongs in this document.
//
// Default by FORMAT rather than asking every time: a spreadsheet is filtered
// and pivoted, so prose in cell A1 is in the way; a PDF or Word file is read,
// so a wall of rows with no explanation is the thing in the way. The spec can
// override either direction.
func wantsNarrative(spec Spec, format Format) bool {
	if spec.Narrative != nil {
		return *spec.Narrative
	}
	return format == FormatPDF || format == FormatDOCX
}

// narrativeTableRows caps how much of each table a narrative document shows.
//
// An executive summary followed by 4,000 rows is not an executive summary. The
// full data is a click away as xlsx or csv, and the document says so rather
// than trailing off.
const narrativeTableRows = 25

// applyNarrative folds prose into the document and trims its tables.
func applyNarrative(doc *Doc, n *Narrative) {
	if n == nil {
		return
	}
	doc.Summary = strings.TrimSpace(n.Summary)
	for i := range doc.Sections {
		if i < len(n.Sections) {
			doc.Sections[i].Commentary = strings.TrimSpace(n.Sections[i])
		}
		if extra := len(doc.Sections[i].Rows) - narrativeTableRows; extra > 0 {
			doc.Sections[i].Rows = doc.Sections[i].Rows[:narrativeTableRows]
			doc.Sections[i].Note = strings.TrimSpace(fmt.Sprintf(
				"%s Showing the first %d of %d rows — export as Excel or CSV for the complete data.",
				doc.Sections[i].Note, narrativeTableRows, narrativeTableRows+extra))
		}
	}
}
