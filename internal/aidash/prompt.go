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

Compose a dashboard as a list of tiles. Each tile picks a metric, a visualisation
from that metric's allowed list, and a position on a 12-column grid.

RULES — these are not style preferences, a tile breaking them cannot render:
1. Use only metric ids present in the catalog. Never invent one.
2. Use only a viz listed for that metric. In particular a timeseries must never
   be a pie: "40% of it happened on Tuesday" is meaningless.
3. Only pass params the metric declares.
4. x + w must be <= 12. Do not overlap tiles.

LAYOUT — how a fleet dashboard is read:
- Single numbers (scalar) go at the top, small: w 3, h 3.
- Charts sit below them, w 6, h 5.
- The live map and lists want room: w 6, h 6.
- Put what the person checks first at the top-left. Reading order is
  top-to-bottom, left-to-right.
- 5 to 8 tiles. A dashboard that needs scrolling to be understood has failed.

CHOOSING A VISUALISATION:
- trend over time  -> line or area
- comparing categories -> bar, or hbar when labels are long (plates, names)
- share of a whole, few categories -> pie
- one number that matters -> stat
- a ranked list people scan -> table

Match the role: an operator watches what is happening now (live status, alerts,
map); a manager watches performance and compliance (trends, scores, documents).

Give each tile a short title. Omit it to accept the metric's default.`

func userPrompt(role string, req composeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Role: %s\n\nCATALOG:\n%s\n\n", role, string(req.Catalog))
	if len(req.Current) > 0 && string(req.Current) != "null" && string(req.Current) != "[]" {
		fmt.Fprintf(&b, "CURRENT DASHBOARD (replace it unless asked to add to it):\n%s\n\n", string(req.Current))
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
				"maxItems":    12,
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
