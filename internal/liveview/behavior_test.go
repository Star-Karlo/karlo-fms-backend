package liveview

import (
	"encoding/json"
	"strings"
	"testing"
)

// hull_no and license_plate are published fields: integrations are told
// hull_no is null when unset and that the two lead the response, so all of
// that is part of the contract.
func TestBehaviorResultVehicleFields(t *testing.T) {
	hull := "S3487"
	cases := []struct {
		name string
		res  BehaviorResult
		want string
	}{
		{"unset hull is null, not omitted", BehaviorResult{LicensePlate: "H 9049 OH"}, `"hull_no":null,"license_plate":"H 9049 OH"`},
		{"set", BehaviorResult{HullNo: &hull, LicensePlate: "H 9049 OH"}, `"hull_no":"S3487","license_plate":"H 9049 OH"`},
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
