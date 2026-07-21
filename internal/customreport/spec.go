// Package customreport turns a saved, user-defined report spec into a file.
//
// THE DESIGN IN ONE LINE: a custom report is a named COMBINATION of the
// reports that already exist, not a new query language.
//
// Each section names a key from the existing catalog and the params to run it
// with. That is deliberate and worth defending — the 17 catalog reports are
// already tenant-scoped, already validated, already return {columns, rows},
// and several of them are expensive Go computations over telemetry that would
// be absurd to reimplement. It also means a custom report can never express a
// query the user was not already allowed to run, so there is no new authorisation
// surface: Run() goes through the same report service, under the same RLS.
//
// What a custom report adds on top is the part that was missing: combining
// several of them, choosing and renaming columns, a window that stays relative,
// a name, and a file at the end.
package customreport

import (
	"fmt"
	"strings"
	"time"
)

// SpecVersion is bumped when a stored spec's meaning changes. Stored with every
// spec so an old row can be read by new code rather than guessed at.
const SpecVersion = 1

// Spec is what gets saved. It is the artifact, not the rendered document — a
// report saved as "last 7 days" means the last 7 days whenever it is run.
type Spec struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Notes   string `json:"notes,omitempty"` // free text printed under the title

	Range    Range     `json:"range"`
	Sections []Section `json:"sections"`

	// Format is the default the user picked; the download can override it.
	Format Format `json:"format"`

	// Narrative overrides the by-format default: prose for pdf/docx, raw tables
	// for xlsx/csv. Nil means "use the default", which is almost always right —
	// the point is that the same saved report gives an executive a readable
	// document and an analyst a spreadsheet, without being configured twice.
	Narrative *bool `json:"narrative,omitempty"`
}

// Section is one block of the report: one underlying report, run with params.
type Section struct {
	Title  string         `json:"title"`
	Source string         `json:"source"` // a catalog key, e.g. "vehicle-trip-summary"
	Params map[string]any `json:"params,omitempty"`

	// Columns optionally narrows and reorders the source's columns, and can
	// rename them. Empty means "all of them, as the source ordered them".
	//
	// Renaming matters more than it looks: the point of a custom report is
	// often that someone downstream expects "Nopol" where we say
	// "license_plate", and forcing them to rename it every month is exactly
	// the friction this feature exists to remove.
	Columns []Column `json:"columns,omitempty"`

	// Note is printed under the section, for context the numbers do not carry.
	Note string `json:"note,omitempty"`
}

// Column selects a source column and optionally relabels it.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"` // empty = the source's own label
}

// Range is the window the report covers.
//
// Relative is the interesting case: a saved report is re-run later, so a frozen
// pair of timestamps would make "weekly idling" mean "that one week in July"
// forever. Absolute is still allowed for a one-off.
type Range struct {
	// Preset is one of the RangePreset values. When set, From/To are ignored.
	Preset RangePreset `json:"preset,omitempty"`
	From   string      `json:"from,omitempty"` // RFC3339, only when Preset is empty
	To     string      `json:"to,omitempty"`
}

type RangePreset string

const (
	RangeToday     RangePreset = "today"
	RangeYesterday RangePreset = "yesterday"
	Range7Days     RangePreset = "last_7_days"
	Range30Days    RangePreset = "last_30_days"
	RangeThisMonth RangePreset = "this_month"
	RangeLastMonth RangePreset = "last_month"
	RangeAbsolute  RangePreset = "" // use From/To
)

var rangePresets = map[RangePreset]bool{
	RangeToday: true, RangeYesterday: true, Range7Days: true,
	Range30Days: true, RangeThisMonth: true, RangeLastMonth: true,
}

// Format is the output document type.
type Format string

const (
	FormatXLSX Format = "xlsx"
	FormatPDF  Format = "pdf"
	FormatDOCX Format = "docx"
	FormatCSV  Format = "csv"
)

var formats = map[Format]bool{FormatXLSX: true, FormatPDF: true, FormatDOCX: true, FormatCSV: true}

// Resolve turns the range into concrete bounds.
//
// Days are LOCAL days. The fleet runs in WIB, so "yesterday" computed in UTC
// would start at 07:00 local and cut the night shift in half — the same class
// of error the telemetry rollup had to fix.
func (r Range) Resolve(now time.Time, loc *time.Location) (from, to time.Time, err error) {
	if loc == nil {
		loc = time.UTC
	}
	n := now.In(loc)
	midnight := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)

	switch r.Preset {
	case RangeToday:
		return midnight, midnight.AddDate(0, 0, 1), nil
	case RangeYesterday:
		return midnight.AddDate(0, 0, -1), midnight, nil
	case Range7Days:
		// Ends NOW, not at midnight: "last 7 days" asked for at 3pm should
		// include this morning.
		return midnight.AddDate(0, 0, -6), n, nil
	case Range30Days:
		return midnight.AddDate(0, 0, -29), n, nil
	case RangeThisMonth:
		first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
		return first, n, nil
	case RangeLastMonth:
		first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
		return first.AddDate(0, -1, 0), first, nil
	case RangeAbsolute:
		if r.From == "" || r.To == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("range: from and to are required when no preset is set")
		}
		f, err := time.Parse(time.RFC3339, r.From)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("range.from: %w", err)
		}
		t, err := time.Parse(time.RFC3339, r.To)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("range.to: %w", err)
		}
		if !t.After(f) {
			return time.Time{}, time.Time{}, fmt.Errorf("range: to must be after from")
		}
		return f, t, nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("range: unknown preset %q", r.Preset)
	}
}

// Label describes the range for a document header.
func (r Range) Label(from, to time.Time) string {
	if r.Preset != RangeAbsolute {
		return fmt.Sprintf("%s (%s to %s)", strings.ReplaceAll(string(r.Preset), "_", " "),
			from.Format("2 Jan 2006"), to.Format("2 Jan 2006"))
	}
	return fmt.Sprintf("%s to %s", from.Format("2 Jan 2006 15:04"), to.Format("2 Jan 2006 15:04"))
}

// Validate checks a spec is runnable BEFORE it is saved.
//
// Errors name the offending value and the legal set, because this is the same
// text the AI's repair pass reads — "invalid section" tells a model nothing,
// "unknown source X; valid: a, b, c" tells it exactly what to change.
func (s *Spec) Validate(knownSources map[string]bool) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(s.Name) > 120 {
		return fmt.Errorf("name is too long (max 120 characters)")
	}
	if len(s.Sections) == 0 {
		return fmt.Errorf("a report needs at least one section")
	}
	if len(s.Sections) > 12 {
		return fmt.Errorf("too many sections (%d); maximum 12", len(s.Sections))
	}
	if s.Range.Preset != RangeAbsolute && !rangePresets[s.Range.Preset] {
		return fmt.Errorf("unknown range preset %q; valid: %s", s.Range.Preset, presetList())
	}
	if s.Format != "" && !formats[s.Format] {
		return fmt.Errorf("unknown format %q; valid: xlsx, pdf, docx, csv", s.Format)
	}
	for i, sec := range s.Sections {
		if !knownSources[sec.Source] {
			return fmt.Errorf("section %d: unknown source %q; valid: %s", i+1, sec.Source, sourceList(knownSources))
		}
	}
	return nil
}

func presetList() string {
	out := make([]string, 0, len(rangePresets))
	for k := range rangePresets {
		out = append(out, string(k))
	}
	return strings.Join(sorted(out), ", ")
}

func sourceList(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return strings.Join(sorted(out), ", ")
}

// sorted is a tiny insertion sort — the lists here are tens of items, and
// pulling in sort for a message builder is not worth the import.
func sorted(in []string) []string {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	return in
}

// ValidFormat normalises a requested format, falling back to the spec's own.
func (s *Spec) ValidFormat(requested string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimSpace(requested)))
	if f == "" {
		f = s.Format
	}
	if f == "" {
		f = FormatXLSX
	}
	if !formats[f] {
		return "", fmt.Errorf("unknown format %q; valid: xlsx, pdf, docx, csv", requested)
	}
	return f, nil
}
