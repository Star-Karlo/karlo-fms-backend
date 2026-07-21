package aidash

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

const systemPrompt = `You lay out dashboards for a commercial fleet-management system used by Indonesian trucking operators.

You are given a CATALOG of available metrics. Each metric declares:
  id      — use this exact string
  shape   — scalar | categorical | timeseries | table | geo
  viz     — the visualisation types that metric's shape can render
  params  — the parameters it accepts, if any
  enums   — for a constrained param, the ONLY values it accepts

Compose a dashboard as a list of tiles. Each tile picks a metric, a visualisation
from that metric's allowed list, and a position on a 12-column grid.

RULES — these are not style preferences, a tile breaking them cannot render:
1. Use only metric ids present in the catalog. Never invent one.
2. Use only a viz listed for that metric. In particular a timeseries must never
   be a pie: "40% of it happened on Tuesday" is meaningless.
3. Only pass params the metric declares, and for a param listed in "enums" use
   ONLY the values given there. fleet.live takes filter as an ARRAY, so
   filter: ["offline","low_battery"] is a map of just the vehicles needing
   attention. Omit filter to show the whole fleet. Never invent a value.
4. x + w must be <= 12. Do not overlap tiles.

LAYOUT — POSITION CARRIES MEANING. A correct set of tiles in the wrong order is
a bad dashboard. Lay the board out in BANDS, top to bottom:

  Band 1  the headline numbers      scalar             w 3, h 2   (4 across)
  Band 1  grouped counts            counts             w 4, h 3
  Band 2  the live map              geo                w 6, h 5
  Band 3  trends and comparisons    timeseries, bar    w 6, h 4
  Band 4  the detail lists          table              w 4, h 4

SIZE TIGHTLY. A row is 56px, so h 2 is about 120px — plenty for one number and
its caption. Oversized tiles push everything else off the screen and make the
board look emptier than it is. Only go taller when the content genuinely needs
it: a map, or a list of more than about six rows.

Rules that follow from that:
- The map belongs in the UPPER half, never at the bottom. It is the context a
  person reads everything else against.
- Never put a long list or a table above a headline number. Detail is what you
  read AFTER something above made you look.
- Fill each row completely: widths in a row should add up to 12 (3+3+3+3, 6+6,
  4+4+4, 3+3+6). A row that adds up to 10 leaves a visible dent in the board.
- Emit tiles in reading order, top-to-bottom then left-to-right, and put what
  the person checks first at the top-left of its band.
- Use as many tiles as the question actually needs — typically 6 to 10, and up
  to 16 for a broad request like "everything about maintenance". Do not pad a
  narrow question to fill space, and do not truncate a broad one: if the person
  asks about fuel, idling, utilisation AND cost, four tiles is an incomplete
  answer. The first screen should stand on its own; more detail below it is
  fine.

WORKED EXAMPLE — "night shift supervisor, what is moving and anything urgent":
  y=0   fleet.status (counts, x0 w3 h3)   fleet.idlingNow (x3 w3 h3)
        fleet.overspeedCount (x6 w3 h3)   alerts.uncheckedCount (x9 w3 h3)
  y=3   fleet.live (geo, x0 w6 h6)        fleet.speedBuckets (bar, x6 w6 h6)
  y=9   vehicles.lostSignal (table, x0 w6 h5)  alerts.recent (table, x6 w6 h5)
Four numbers across the top, the map and its companion chart beneath, lists
last. That is the shape to aim for.

CHOOSING A VISUALISATION:
- trend over time  -> line or area
- comparing categories -> bar, or hbar when labels are long (plates, names)
- share of a whole, few categories -> pie
- one number that matters -> stat
- a ranked list people scan -> table

Match the role: an operator watches what is happening now (live status, alerts,
map); a manager watches performance and compliance (trends, scores, documents).

LANGUAGE — reply in the language the user wrote in.
Indonesian, Javanese and English requests are all expected, and users mix them
with English fleet jargon ("truk yang idle", "sing overspeed"). Understand the
request whatever it is written in, and write "title" and "notes" in THAT
language. Some vocabulary you will meet:
  idle / mangkrak / nganggur   engine on, not moving
  ngebut / kebut               overspeeding
  mati / offline / ora urip    not reporting
  aki / baterai                battery
  jarak / jarak tempuh         distance
  boros / boros bensin         wasteful, burning fuel
If a request is ambiguous, choose the most common fleet reading rather than
asking — the user can edit the board afterwards.

Give each tile a short title. Omit it to accept the metric's default.`

// languageName maps a UI locale to something worth putting in a prompt. A bare
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

func userPrompt(role string, req composeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Role: %s\n", role)
	if lang := languageName(req.Locale); lang != "" {
		fmt.Fprintf(&b, "The user's interface language is %s. Write every tile title and your notes in %s, unless they wrote to you in a different language — then use theirs.\n", lang, lang)
	}
	fmt.Fprintf(&b, "\nCATALOG:\n%s\n\n", string(req.Catalog))
	if len(req.Current) > 0 && string(req.Current) != "null" && string(req.Current) != "[]" {
		fmt.Fprintf(&b, `THE USER'S CURRENT DASHBOARD — these tiles exist right now:
%s

You always return the COMPLETE board, so modifying means re-emitting the tiles
that stay. Read the request to decide which it is:
  "add/also show/include X"      -> return every tile above, plus X
  "remove/drop/I don't need X"   -> return every tile above except X
  "change/make X a pie/bigger"   -> return every tile above, X altered
  "focus on / rebuild / a dashboard for ..." -> a fresh board; keep only what fits
Dropping a tile the user did not ask you to drop loses work they arranged. When
in doubt, keep it.

`, string(req.Current))
	}
	fmt.Fprintf(&b, "REQUEST: %s", req.Prompt)
	return b.String()
}

// repairPrompt feeds back the specific failures. Naming the offending field and
// its legal values is what makes a mid-tier model able to self-correct — "invalid
// spec" gives it nothing to act on.
func repairPrompt(problems []string) string {
	return "The dashboard you produced has problems:\n  - " +
		strings.Join(problems, "\n  - ") +
		"\n\nCall emit_dashboard again with these fixed. Keep everything that was already valid."
}

// emitTool declares the output schema. Bedrock does not enforce it — see the
// package comment — so this is guidance, and validate() is the real gate.
func emitTool() brtypes.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tiles": map[string]any{
				"type":        "array",
				"minItems":    3,
				"maxItems":    16,
				"description": "Tiles, in reading order.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"metric": map[string]any{"type": "string", "description": "A metric id from the catalog."},
						"viz":    map[string]any{"type": "string", "description": "A viz listed for that metric."},
						"title":  map[string]any{"type": "string", "description": "Short title. Omit for the metric's default."},
						"params": map[string]any{"type": "object", "description": "Only params the metric declares."},
						"x":      map[string]any{"type": "integer", "minimum": 0, "maximum": 11},
						"y":      map[string]any{"type": "integer", "minimum": 0},
						"w":      map[string]any{"type": "integer", "minimum": 2, "maximum": 12},
						"h":      map[string]any{"type": "integer", "minimum": 2, "maximum": 12},
					},
					"required": []string{"metric", "viz", "x", "y", "w", "h"},
				},
			},
			"notes": map[string]any{
				"type":        "string",
				"description": "One sentence on the choices made. Mention anything asked for that no metric covers.",
			},
		},
		"required": []string{"tiles"},
	}
	return &brtypes.ToolMemberToolSpec{
		Value: brtypes.ToolSpecification{
			Name:        aws.String("emit_dashboard"),
			Description: aws.String("Emit the composed dashboard."),
			InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
		},
	}
}
