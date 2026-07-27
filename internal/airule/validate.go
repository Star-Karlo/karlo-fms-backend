package airule

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/karlo/fms-backend/internal/alert"
)

// Bedrock's toolConfig carries no schema guarantee, so this is the real gate.
// Each problem names the offending value AND the legal set, because it goes
// straight back to the model as its repair instruction.

// ruleIn is the loosely-typed shape the model emits.
type ruleIn struct {
	RuleName      string `json:"rule_name"`
	AlertCode     string `json:"alert_code"`
	Threshold     *int   `json:"threshold"`
	WindowMinutes *int   `json:"window_minutes"`
	Urgency       string `json:"urgency"`
	AppliesTo     string `json:"applies_to"`
}

// ruleOut is the validated shape, matching the notifrule create body so the
// client can POST it unchanged.
type ruleOut struct {
	RuleName         string  `json:"rule_name"`
	AlertCode        string  `json:"alert_code"`
	NotificationName string  `json:"notification_name"`
	Threshold        float64 `json:"threshold"`
	WindowMinutes    *int    `json:"window_minutes,omitempty"`
	Urgency          string  `json:"urgency"`
	AppliesTo        string  `json:"applies_to"`
}

// validate keeps the rules that can actually be saved and matched, returning
// model-readable problems for the rest.
func validate(input map[string]any) (json.RawMessage, string, []string) {
	notes, _ := input["notes"].(string)

	codes := make([]string, 0, len(alert.Taxonomy))
	for _, t := range alert.Taxonomy {
		codes = append(codes, t.Code)
	}
	sort.Strings(codes)

	raw, err := json.Marshal(input["rules"])
	if err != nil {
		return json.RawMessage("[]"), notes, []string{"rules was not an array"}
	}
	var rules []ruleIn
	if err := json.Unmarshal(raw, &rules); err != nil {
		return json.RawMessage("[]"), notes, []string{"rules was not an array of rule objects"}
	}

	var problems []string
	kept := make([]ruleOut, 0, len(rules))

	for i, r := range rules {
		tax, ok := alert.Lookup(r.AlertCode)
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"rule %d: unknown alert_code %q; valid codes: %s", i, r.AlertCode, strings.Join(codes, ", ")))
			continue
		}
		if r.Threshold == nil || *r.Threshold < 1 {
			problems = append(problems, fmt.Sprintf(
				"rule %d (%s): threshold must be a whole number >= 1", i, r.AlertCode))
			continue
		}
		if r.WindowMinutes != nil && *r.WindowMinutes < 1 {
			problems = append(problems, fmt.Sprintf(
				"rule %d (%s): window_minutes must be >= 1, or omitted for an instant rule", i, r.AlertCode))
			continue
		}
		urgency := r.Urgency
		if urgency != "important" && urgency != "urgent" {
			urgency = tax.Severity // fall back to the event's own default rather than reject
			if urgency != "urgent" {
				urgency = "important"
			}
		}
		appliesTo := r.AppliesTo
		if appliesTo != "all" && appliesTo != "group" && appliesTo != "selected" {
			appliesTo = "all"
		}
		name := strings.TrimSpace(r.RuleName)
		if name == "" {
			name = tax.NameEN
		}
		kept = append(kept, ruleOut{
			RuleName:         name,
			AlertCode:        r.AlertCode,
			NotificationName: tax.NameEN,
			Threshold:        float64(*r.Threshold),
			WindowMinutes:    r.WindowMinutes,
			Urgency:          urgency,
			AppliesTo:        appliesTo,
		})
	}

	if len(kept) == 0 && len(problems) == 0 {
		problems = append(problems, "no rules were produced")
	}

	out, _ := json.Marshal(kept)
	return out, notes, problems
}
