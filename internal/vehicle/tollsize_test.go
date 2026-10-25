package vehicle

import "testing"

func i16(v int16) *int16     { return &v }
func f64(v float64) *float64 { return &v }

// The bounds exist because the legacy truck master holds dimensions on 96% of
// its rows at values including 23 m wide, 32 m tall and 1.232 kg — all of
// which pass a "greater than zero" check, which is how they survived.
func TestValidTollAndSizeRejectsTheLegacyNoise(t *testing.T) {
	cases := []struct {
		name                          string
		axles, gol                    *int16
		length, width, height, weight *float64
		wantErr                       bool
	}{
		{name: "nothing set is fine — every field is optional"},
		{name: "a 5-axle trailer", axles: i16(5), length: f64(12.5), width: f64(2.5),
			height: f64(4.2), weight: f64(40000)},

		// The real values from the legacy data.
		{name: "23 m wide", width: f64(23), wantErr: true},
		{name: "32 m tall", height: f64(32), wantErr: true},

		// 1.232 kg IS NOT CATCHABLE HERE, and pretending otherwise would mean
		// moving a bound to suit a test. It appeared on a "CDD Box", for which
		// it is absurd — a CDD grosses around 8 tonnes — but it is a perfectly
		// ordinary figure for a pickup, and this function does not know the
		// vehicle type. What saves that particular row is its width of 23 m:
		// the same form-filler typed 1, 2, 3 across the fields, and the
		// dimensions catch what the weight cannot.
		{name: "1232 kg alone is low but not impossible", weight: f64(1232)},
		{name: "1232 kg beside a 23 m width is rejected on the width",
			weight: f64(1232), width: f64(23), wantErr: true},
		{name: "100 kg is not a vehicle", weight: f64(100), wantErr: true},

		// Golongan I and II both exist at two axles, so 2 must be allowed —
		// the override is what distinguishes them, not a rejection here.
		{name: "two axles is valid", axles: i16(2)},
		{name: "one axle is not a lorry", axles: i16(1), wantErr: true},
		{name: "axle count far too high", axles: i16(40), wantErr: true},

		{name: "override in range", gol: i16(1)},
		{name: "override above golongan V", gol: i16(6), wantErr: true},
		{name: "override of zero", gol: i16(0), wantErr: true},
	}
	for _, c := range cases {
		err := validTollAndSize(c.axles, c.gol, c.length, c.width, c.height, c.weight)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: wantErr=%v, got %v", c.name, c.wantErr, err)
		}
		// A rejection has to name the field, or an operator cannot tell which
		// of six inputs was wrong.
		if err != nil && len(err.Error()) < 10 {
			t.Errorf("%s: message too terse to act on: %q", c.name, err)
		}
	}
}
