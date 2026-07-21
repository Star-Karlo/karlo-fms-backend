package aidash

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Bedrock's toolConfig carries no schema guarantee, so this is the real gate.
// Every problem is phrased with the offending value AND the legal set, because
// the message goes straight back to the model as its repair instruction — the
// difference between "invalid metric" and `unknown metric "alerts.byWeek";
// valid: ...` is the difference between a model that fixes itself and one that
// guesses again.

type catalogEntry struct {
	ID     string   `json:"id"`
	Shape  string   `json:"shape"`
	Viz    []string `json:"viz"`
	Params []string `json:"params"`
	// Enums carries the legal values for constrained params. Without it the
	// model can only be told a param EXISTS, and it fills constrained ones with
	// plausible inventions ("broken", "problem") that match nothing and render
	// an empty tile which looks exactly like a working one.
	Enums map[string][]string `json:"enums,omitempty"`
}

type tileOut struct {
	Metric string         `json:"metric"`
	Viz    string         `json:"viz"`
	Title  string         `json:"title,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	X      int            `json:"x"`
	Y      int            `json:"y"`
	W      int            `json:"w"`
	H      int            `json:"h"`
}

// filterAllowed splits a param value against a vocabulary. Handles both a bare
// string and an array, since a model will send either for a list-valued param.
func filterAllowed(v any, allowed []string) (any, []string) {
	ok := func(s string) bool { return contains(allowed, s) }
	switch val := v.(type) {
	case string:
		if ok(val) {
			return val, nil
		}
		return nil, []string{val}
	case []any:
		var kept []any
		var bad []string
		for _, item := range val {
			s, isStr := item.(string)
			if !isStr {
				bad = append(bad, fmt.Sprintf("%v", item))
				continue
			}
			if ok(s) {
				kept = append(kept, s)
			} else {
				bad = append(bad, s)
			}
		}
		if len(kept) == 0 {
			return nil, bad
		}
		return kept, bad
	}
	return v, nil
}

// validate filters the model's tiles down to those that will actually render,
// and returns human-and-model-readable problems for the rest.
func validate(input map[string]any, catalogRaw json.RawMessage) (json.RawMessage, string, []string) {
	var catalog []catalogEntry
	_ = json.Unmarshal(catalogRaw, &catalog)
	byID := make(map[string]catalogEntry, len(catalog))
	ids := make([]string, 0, len(catalog))
	for _, c := range catalog {
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)

	notes, _ := input["notes"].(string)

	raw, err := json.Marshal(input["tiles"])
	if err != nil {
		return json.RawMessage("[]"), notes, []string{"tiles was not an array"}
	}
	var tiles []tileOut
	if err := json.Unmarshal(raw, &tiles); err != nil {
		return json.RawMessage("[]"), notes, []string{"tiles was not an array of tile objects"}
	}

	var problems []string
	kept := make([]tileOut, 0, len(tiles))

	for i, t := range tiles {
		entry, ok := byID[t.Metric]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"tile %d: unknown metric %q; valid ids: %s", i, t.Metric, strings.Join(ids, ", ")))
			continue
		}
		if !contains(entry.Viz, t.Viz) {
			problems = append(problems, fmt.Sprintf(
				"tile %d (%s): viz %q is not valid for a %s metric; allowed: %s",
				i, t.Metric, t.Viz, entry.Shape, strings.Join(entry.Viz, ", ")))
			continue
		}
		// Unknown params are dropped rather than rejected: the model invents
		// them, and one stray key shouldn't cost an otherwise good tile. But it
		// IS reported, so the repair pass can stop doing it.
		if len(t.Params) > 0 {
			for k, v := range t.Params {
				if !contains(entry.Params, k) {
					problems = append(problems, fmt.Sprintf(
						"tile %d (%s): unknown param %q (dropped); accepted: %s",
						i, t.Metric, k, orNone(entry.Params)))
					delete(t.Params, k)
					continue
				}
				allowed, constrained := entry.Enums[k]
				if !constrained {
					continue
				}
				// Constrained param: keep only values in the vocabulary. Naming
				// both the offender and the legal set is what lets the repair
				// pass fix it rather than guess again.
				kept, dropped := filterAllowed(v, allowed)
				if len(dropped) > 0 {
					problems = append(problems, fmt.Sprintf(
						"tile %d (%s): param %q has invalid value(s) %s; allowed: %s",
						i, t.Metric, k, strings.Join(dropped, ", "), strings.Join(allowed, ", ")))
				}
				if kept == nil {
					// Everything invalid: unset it. Leaving an empty list would
					// mean "select nothing", blanking a tile the model plainly
					// meant to populate.
					delete(t.Params, k)
				} else {
					t.Params[k] = kept
				}
			}
		}
		// Geometry: clamp rather than reject. An off-grid tile is a layout
		// nuisance, not a rendering failure, and the grid reflows anyway.
		t.W = clamp(t.W, 2, 12)
		t.H = clamp(t.H, 2, 12)
		t.X = clamp(t.X, 0, 12-t.W)
		if t.Y < 0 {
			t.Y = 0
		}
		kept = append(kept, t)
	}

	if len(kept) == 0 && len(problems) == 0 {
		problems = append(problems, "no tiles were produced")
	}

	out, _ := json.Marshal(kept)
	return out, notes, problems
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "(none)"
	}
	return strings.Join(xs, ", ")
}
