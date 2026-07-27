package airule

import (
	"encoding/json"
	"testing"
)

// decode is a tiny helper: run validate over a tool-input map and unmarshal the
// kept rules.
func decode(t *testing.T, input map[string]any) ([]ruleOut, string, []string) {
	t.Helper()
	raw, notes, problems := validate(input)
	var out []ruleOut
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("kept rules not valid JSON: %v", err)
	}
	return out, notes, problems
}

func TestValidate_FrequencyRule(t *testing.T) {
	out, _, problems := decode(t, map[string]any{
		"rules": []any{map[string]any{
			"rule_name": "Repeated smoking", "alert_code": "DMS_SMOKING",
			"threshold": 10, "window_minutes": 60, "urgency": "important",
		}},
	})
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 rule, got %d", len(out))
	}
	r := out[0]
	if r.AlertCode != "DMS_SMOKING" || r.Threshold != 10 {
		t.Fatalf("bad rule: %+v", r)
	}
	if r.WindowMinutes == nil || *r.WindowMinutes != 60 {
		t.Fatalf("window not carried: %+v", r)
	}
	if r.AppliesTo != "all" {
		t.Fatalf("applies_to should default to all, got %q", r.AppliesTo)
	}
}

func TestValidate_UnknownCodeRejected(t *testing.T) {
	_, _, problems := decode(t, map[string]any{
		"rules": []any{map[string]any{
			"rule_name": "Bogus", "alert_code": "DMS_TELEPORT",
			"threshold": 3, "window_minutes": 30, "urgency": "urgent",
		}},
	})
	if len(problems) == 0 {
		t.Fatal("expected a problem for an unknown alert_code")
	}
}

func TestValidate_ThresholdFloor(t *testing.T) {
	_, _, problems := decode(t, map[string]any{
		"rules": []any{map[string]any{
			"rule_name": "Zero", "alert_code": "DMS_PHONE", "threshold": 0, "urgency": "urgent",
		}},
	})
	if len(problems) == 0 {
		t.Fatal("expected a problem for threshold < 1")
	}
}

func TestValidate_InstantRuleOmitsWindow(t *testing.T) {
	out, _, problems := decode(t, map[string]any{
		"rules": []any{map[string]any{
			"rule_name": "Every panic", "alert_code": "DMS_PHONE", "threshold": 1, "urgency": "urgent",
		}},
	})
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if out[0].WindowMinutes != nil {
		t.Fatalf("instant rule should have no window, got %v", *out[0].WindowMinutes)
	}
}

func TestValidate_BadUrgencyFallsBack(t *testing.T) {
	out, _, problems := decode(t, map[string]any{
		"rules": []any{map[string]any{
			"rule_name": "Weird", "alert_code": "DMS_SMOKING", "threshold": 5,
			"window_minutes": 60, "urgency": "catastrophic",
		}},
	})
	if len(problems) != 0 {
		t.Fatalf("bad urgency should fall back, not fail: %v", problems)
	}
	if out[0].Urgency != "important" && out[0].Urgency != "urgent" {
		t.Fatalf("urgency not normalised: %q", out[0].Urgency)
	}
}
