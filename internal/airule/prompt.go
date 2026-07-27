package airule

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/karlo/fms-backend/internal/alert"
)

const systemPrompt = `You configure driver-safety notification rules for a commercial fleet-management
system used by Indonesian trucking operators.

A rule watches ONE event type and fires a notification. There are two kinds:
  - FREQUENCY rule: fire when an event happens at least N times within a window
    of minutes for one vehicle. This is the default and the most useful kind —
    "smoking 10 times in 60 minutes", "3 forward-collision warnings in 15
    minutes". Set threshold to N and window_minutes to the window.
  - INSTANT rule: fire on every single event. Use ONLY when the user clearly
    wants to know about each occurrence (e.g. "tell me every time the panic
    button is pressed"). Omit window_minutes; set threshold to 1.

You are given a CATALOG of event codes. Each row is:
  code — use this EXACT string as alert_code
  name — what it means
  source — camera (the driver-facing DMS / road-facing ADAS lens) or gps

RULES — a rule breaking these cannot be saved:
1. alert_code MUST be a code from the catalog. Never invent one.
2. threshold is a whole number >= 1 (the count of events).
3. window_minutes, when present, is a whole number of minutes >= 1. Omit it only
   for an instant rule.
4. urgency is "important" or "urgent". Use "urgent" for anything about
   impairment, collision, or the driver leaving the frame; "important" otherwise.
5. applies_to defaults to "all" (every vehicle). Only use "selected" if the user
   named specific vehicles — you cannot resolve names to ids, so prefer "all".

Give each rule a short, human rule_name describing what it catches, e.g.
"Repeated smoking" or "Frequent tailgating".

Make ONE rule per distinct thing the user asked to be warned about. A request
mentioning smoking, yawning AND phone use is THREE rules, not one.

Pick sensible numbers when the user is vague ("a lot", "keeps happening"):
prefer a 60-minute window and a threshold around 5–10 for minor events, lower
(2–3) for dangerous ones. State any number you chose in "notes".

LANGUAGE — write rule_name and notes in the language the user wrote in
(Indonesian and English are both expected; users mix in English fleet jargon).`

// catalog renders the event taxonomy the model may choose from. Built from the
// server-owned taxonomy so it can never drift from what a rule can actually
// match. Ordered camera-first, since safety rules are overwhelmingly camera.
func catalog() string {
	var cam, gps []alert.AlertType
	for _, t := range alert.Taxonomy {
		if t.Source == alert.SourceCamera {
			cam = append(cam, t)
		} else {
			gps = append(gps, t)
		}
	}
	var b strings.Builder
	write := func(list []alert.AlertType) {
		for _, t := range list {
			fmt.Fprintf(&b, "  %-20s %-28s %s\n", t.Code, t.NameEN, t.Source)
		}
	}
	b.WriteString("code                 name                         source\n")
	write(cam)
	write(gps)
	return b.String()
}

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

func userPrompt(req composeRequest) string {
	var b strings.Builder
	if lang := languageName(req.Locale); lang != "" {
		fmt.Fprintf(&b, "The user's interface language is %s. Write rule_name and notes in %s unless they wrote to you in another language.\n\n", lang, lang)
	}
	fmt.Fprintf(&b, "CATALOG:\n%s\n", catalog())
	fmt.Fprintf(&b, "REQUEST: %s", req.Prompt)
	return b.String()
}

func repairPrompt(problems []string) string {
	return "The rules you produced have problems:\n  - " +
		strings.Join(problems, "\n  - ") +
		"\n\nCall emit_rules again with these fixed. Keep everything that was already valid."
}

// emitTool declares the output schema. Bedrock does not enforce it, so validate()
// is the real gate.
func emitTool() brtypes.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rules": map[string]any{
				"type":        "array",
				"minItems":    1,
				"maxItems":    12,
				"description": "One rule per thing to be warned about.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"rule_name":      map[string]any{"type": "string", "description": "Short human name for the rule."},
						"alert_code":     map[string]any{"type": "string", "description": "An event code from the catalog."},
						"threshold":      map[string]any{"type": "integer", "minimum": 1, "description": "Event count that triggers the rule."},
						"window_minutes": map[string]any{"type": "integer", "minimum": 1, "description": "Window in minutes. Omit for an instant (every-event) rule."},
						"urgency":        map[string]any{"type": "string", "enum": []string{"important", "urgent"}},
						"applies_to":     map[string]any{"type": "string", "enum": []string{"all", "group", "selected"}},
					},
					"required": []string{"rule_name", "alert_code", "threshold", "urgency"},
				},
			},
			"notes": map[string]any{
				"type":        "string",
				"description": "One or two sentences: numbers you chose, or anything asked for that no event covers.",
			},
		},
		"required": []string{"rules"},
	}
	return &brtypes.ToolMemberToolSpec{
		Value: brtypes.ToolSpecification{
			Name:        aws.String("emit_rules"),
			Description: aws.String("Emit the proposed notification rules."),
			InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
		},
	}
}
