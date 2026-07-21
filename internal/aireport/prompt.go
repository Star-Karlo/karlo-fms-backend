package aireport

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/karlo/fms-backend/internal/report"
)

// systemPrompt teaches the model what a report IS here.
//
// The governing constraint, and the reason this works at all: the model does
// not write a query. It picks from reports that already exist and says how to
// present them. Everything it can express, a user could have built by hand.
const systemPrompt = `You compose FLEET REPORTS for a fleet management system.

You are given SOURCES — the reports that already exist. Each has:
  key         — use this exact string
  title       — what it reports
  description — what it means
  columns     — the fields it produces, as key:Label
  needs       — parameters it REQUIRES ("vehicle" means one vehicle must be named)

Compose a report as a list of SECTIONS. Each section is one source, optionally
with a chosen subset of its columns, optionally renamed.

RULES — a report breaking these cannot be generated:
1. Use only source keys from the list. Never invent one.
2. Use only column keys that source declares. A column from a different source
   is not available in this one.
3. A source whose "needs" includes "vehicle" reports on ONE vehicle and requires
   params.vehicle_id set to that vehicle's numeric id. There is NO "all" and no
   fleet-wide mode for these. If the user has not named a specific vehicle,
   choose a source without "needs" instead — vehicle-alerts-summary,
   vehicle-alerts-history, service-scheduling-detail and driver-management all
   cover the whole fleet.
4. 1 to 8 sections. A report is read start to finish; twelve tables is a data
   dump, not a report.

RANGE — pick one preset covering the whole report:
  today | yesterday | last_7_days | last_30_days | this_month | last_month
Use the preset that matches the request. "This week" -> last_7_days. "Bulan
ini" -> this_month. Only use absolute from/to if the user names exact dates.

CHOOSING SOURCES:
- Ask what DECISION the report supports, then include what informs it.
- A monthly management report wants summaries, not every GPS ping. Prefer
  vehicle-alerts-summary over vehicle-alerts-history unless the user asked to
  see individual events.
- Order sections the way the reader thinks: summary first, then detail, then
  supporting reference data.
- Do not pad. Three sections that answer the question beat eight that bury it.

COLUMNS:
- Omit "columns" entirely to include everything the source produces. That is
  usually right.
- Choose columns when the user asked for something specific, or to match
  headers they gave you.
- Rename a column when the user's own wording differs from ours — the point of
  a custom report is often that someone downstream expects "Nopol" where we say
  "Plate".
- A RENAME MUST NOT CHANGE WHAT THE COLUMN MEANS. "Plate" -> "Nopol" is the
  same fact in their words. Labelling a driver's status column "Driver Rating"
  because they asked for a rating is not: the report then states something
  false with our name on it, and the reader has no way to tell. If no column
  holds what a header asks for, put that header in "unmapped" and leave it out.

MATCHING AN EXISTING SPREADSHEET:
If the user provides column headers from a file they already use, map each to
the closest column we produce, and set "label" to THEIR header so the output
matches what they expect. Headers you cannot source from any report must be
listed in "unmapped" — do not invent a column key to cover one. Being straight
about a column we cannot fill is far more useful than a plausible-looking
report with a wrong number in it.

LANGUAGE — reply in the language the user wrote in.
Indonesian, Javanese and English are all expected. Write "name", section
"title" and "notes" in that language. Vocabulary you will meet:
  laporan (report), harian/mingguan/bulanan (daily/weekly/monthly),
  kendaraan/truk (vehicle), pengemudi/sopir (driver), jarak (distance),
  bahan bakar (fuel), servis/perawatan (service), pelanggaran (violation),
  nopol (plate number), ringkasan (summary), rincian (detail)

Give the report a short, specific name — what it is, not "Custom Report".`

// sourceEntry is one report as the model sees it. Deliberately compact: the
// catalog is sent on every request and every token is paid each time.
type sourceEntry struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Columns     []string `json:"columns"`
	Needs       []string `json:"needs,omitempty"`
}

// perVehicle mirrors the report runner's own requirement. Kept as a list here
// rather than exported from report because it is presentational — it exists to
// stop the model proposing a per-vehicle report with no vehicle, and a
// mismatch costs a repair round, not a wrong answer.
var perVehicle = map[string]bool{
	"vehicle-trip-history": true, "vehicle-trip-summary": true,
	"vehicle-usage-summary": true, "vehicle-stop-history": true,
	"gps-connection-history": true, "geofence-inout": true,
	"speeding-violations": true, "harsh-driving": true,
	"fuel-history": true, "temperature-history": true,
	"door-open-duration": true, "pto-active-duration": true,
}

// buildCatalog derives the source list from the report catalog, so a report
// retired there disappears from here rather than lingering as a valid choice.
func buildCatalog() []sourceEntry {
	out := make([]sourceEntry, 0, len(report.Catalog))
	for _, m := range report.Catalog {
		if !m.Runnable {
			continue
		}
		e := sourceEntry{Key: m.Key, Title: m.Title, Description: m.Description}
		for _, c := range report.ColumnsFor(m.Key) {
			e.Columns = append(e.Columns, c.Key+":"+c.Label)
		}
		if perVehicle[m.Key] {
			e.Needs = append(e.Needs, "vehicle")
		}
		out = append(out, e)
	}
	return out
}

func userPrompt(req composeRequest) string {
	var b strings.Builder

	if lang := languageName(req.Locale); lang != "" {
		fmt.Fprintf(&b, "The user's interface language is %s. Write the report name, section titles and notes in %s, unless they wrote to you in a different language — then use theirs.\n\n", lang, lang)
	}

	cat, _ := json.Marshal(buildCatalog())
	fmt.Fprintf(&b, "SOURCES:\n%s\n\n", cat)

	if len(req.Headers) > 0 {
		fmt.Fprintf(&b, `THE USER'S EXISTING SPREADSHEET has these column headers:
%s

Match each to a column we produce and use their header as the label, so the
output lines up with the file they already use. List any you cannot source in
"unmapped".

`, strings.Join(req.Headers, " | "))
	}
	if req.Sample != "" {
		fmt.Fprintf(&b, "A few sample rows from that file, for context on what the columns hold:\n%s\n\n", req.Sample)
	}

	if req.Current != nil && len(req.Current.Sections) > 0 {
		cur, _ := json.Marshal(req.Current)
		fmt.Fprintf(&b, `THE REPORT BEING EDITED:
%s

You return the COMPLETE report, so changing it means re-emitting the sections
that stay. Keep anything the user did not ask you to change.

`, cur)
	}

	fmt.Fprintf(&b, "REQUEST: %s", req.Prompt)
	return b.String()
}

// languageName maps a UI locale to something worth putting in a prompt — a bare
// tag ("id") is a weaker instruction to a model than the language's name.
func languageName(locale string) string {
	switch {
	case strings.HasPrefix(locale, "id"):
		return "Bahasa Indonesia"
	case strings.HasPrefix(locale, "jv"):
		return "Basa Jawa"
	case strings.HasPrefix(locale, "en"):
		return "English"
	default:
		return ""
	}
}

// emitTool is the schema the model must fill.
//
// Bedrock has no equivalent of strict schema enforcement, so this constrains
// what it will usually produce and validate.go decides what is actually
// accepted. Enums are stated anyway because they measurably reduce repairs.
func emitTool() brtypes.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string", "description": "Short, specific report name."},
			"notes": map[string]any{"type": "string", "description": "One line on what the report covers. Optional."},
			"range": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preset": map[string]any{
						"type": "string",
						"enum": []string{"today", "yesterday", "last_7_days", "last_30_days", "this_month", "last_month"},
					},
					"from": map[string]any{"type": "string", "description": "RFC3339. Only when no preset fits."},
					"to":   map[string]any{"type": "string", "description": "RFC3339."},
				},
			},
			"sections": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 8,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":  map[string]any{"type": "string"},
						"source": map[string]any{"type": "string", "description": "A source key from the list."},
						"params": map[string]any{"type": "object", "description": "Only vehicle_id, alert_code or limit."},
						"note":   map[string]any{"type": "string", "description": "Context printed under the section. Optional."},
						"columns": map[string]any{
							"type":        "array",
							"description": "Optional. Omit for all columns.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"key":   map[string]any{"type": "string"},
									"label": map[string]any{"type": "string", "description": "Rename. Omit to keep ours."},
								},
								"required": []string{"key"},
							},
						},
					},
					"required": []string{"source"},
				},
			},
			"unmapped": map[string]any{
				"type":        "array",
				"description": "Spreadsheet headers that no report can supply.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"name", "range", "sections"},
	}
	return &brtypes.ToolMemberToolSpec{
		Value: brtypes.ToolSpecification{
			Name:        aws.String("emit_report"),
			Description: aws.String("Emit a custom report definition."),
			InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
		},
	}
}
