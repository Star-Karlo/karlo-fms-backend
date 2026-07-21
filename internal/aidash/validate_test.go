package aidash

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A constrained param is the one place the model reliably invents values —
// asked for "trucks with problems" it emits filter: ["broken"]. That matches
// nothing and renders an EMPTY tile, which is indistinguishable from a working
// tile reporting good news. So invalid values must be caught and named.
func TestValidateFiltersEnumValues(t *testing.T) {
	catalog := json.RawMessage(`[{"id":"fleet.live","shape":"geo","viz":["map","table"],
		"params":["filter","overspeedKph"],
		"enums":{"filter":["moving","idle","parking","offline","low_battery","overspeeding"]}}]`)

	cases := []struct {
		name     string
		in       string
		wantKept any
		wantProb bool
	}{
		{"valid values pass through", `["offline","low_battery"]`, []any{"offline", "low_battery"}, false},
		{"partly invalid keeps the good ones", `["offline","broken"]`, []any{"offline"}, true},
		{"all invalid unsets rather than blanking", `["broken","nonsense"]`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{"tiles": []any{map[string]any{
				"metric": "fleet.live", "viz": "map",
				"params": map[string]any{"filter": mustJSON(tc.in)},
				"x":      0.0, "y": 0.0, "w": 6.0, "h": 6.0,
			}}}
			out, _, problems := validate(input, catalog)

			var tiles []struct {
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(out, &tiles); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(tiles) != 1 {
				t.Fatalf("tile was dropped entirely; a bad param value must not cost the tile")
			}
			gotFilter := tiles[0].Params["filter"]
			if !reflect.DeepEqual(gotFilter, tc.wantKept) {
				t.Errorf("filter = %#v, want %#v", gotFilter, tc.wantKept)
			}
			if (len(problems) > 0) != tc.wantProb {
				t.Errorf("problems = %v, want reported=%v", problems, tc.wantProb)
			}
			// The repair pass can only fix what it is told, so the message must
			// name the legal set, not just say "invalid".
			if tc.wantProb {
				joined := strings.Join(problems, " ")
				if !strings.Contains(joined, "allowed:") || !strings.Contains(joined, "low_battery") {
					t.Errorf("problem does not name the legal values: %q", joined)
				}
			}
		})
	}
}

func mustJSON(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return v
}
