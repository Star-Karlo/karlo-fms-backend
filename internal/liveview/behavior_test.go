package liveview

import (
	"encoding/json"
	"strings"
	"testing"
)

// hull_no is a published field: integrations are told it is null when unset
// and that it leads the response, so both are part of the contract.
func TestBehaviorResultHullNo(t *testing.T) {
	hull := "S3487"
	cases := []struct {
		name string
		res  BehaviorResult
		want string
	}{
		{"unset is null, not omitted", BehaviorResult{}, `"hull_no":null`},
		{"set", BehaviorResult{HullNo: &hull}, `"hull_no":"S3487"`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.res)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		out := string(b)
		if !strings.HasPrefix(out, "{"+c.want+",") {
			t.Errorf("%s: response must lead with %s, got %s", c.name, c.want, out)
		}
		if strings.Index(out, `"hull_no"`) > strings.Index(out, `"summary"`) {
			t.Errorf("%s: hull_no must come before summary, got %s", c.name, out)
		}
	}
}
