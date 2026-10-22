package site

import (
	"math"
	"testing"
)

// The radius decides who is inside, so the distance behind it has to be the
// real one. A flat-earth approximation is fine over a depot and wrong over a
// 50 km geofence, which the API allows.
func TestMetersBetween(t *testing.T) {
	// Monas to Kota Tua, Jakarta: about 4.1 km.
	got := metersBetween(-6.1754, 106.8272, -6.1352, 106.8133)
	if math.Abs(got-4600) > 400 {
		t.Errorf("Monas→Kota Tua = %.0f m, expected roughly 4.6 km", got)
	}
	// Jakarta to Semarang: about 400 km.
	got = metersBetween(-6.1754, 106.8272, -7.0051, 110.4381)
	if math.Abs(got-410_000) > 15_000 {
		t.Errorf("Jakarta→Semarang = %.0f m, expected roughly 410 km", got)
	}
	if d := metersBetween(-6.1754, 106.8272, -6.1754, 106.8272); d != 0 {
		t.Errorf("a point is %v m from itself", d)
	}
	// Symmetric, or a vehicle could be inside a fence the fence is not around.
	a := metersBetween(-6.2, 106.8, -6.3, 106.9)
	b := metersBetween(-6.3, 106.9, -6.2, 106.8)
	if math.Abs(a-b) > 0.001 {
		t.Errorf("distance is not symmetric: %v vs %v", a, b)
	}
}

func TestColumnPicksTheRightSite(t *testing.T) {
	if column(Hangar) != "v.hangar_id" || column(Geofence) != "v.geofence_id" {
		t.Error("a visit must be filtered by its own kind of site")
	}
	// Anything unknown falls back to geofence rather than to no filter at
	// all, which would read every site's visits as one.
	if column(Kind("nonsense")) != "v.geofence_id" {
		t.Error("an unknown kind must still filter by something")
	}
}

func TestNullIf(t *testing.T) {
	if p := nullIf(Geofence, Geofence, 7); p == nil || *p != 7 {
		t.Error("a geofence visit must carry its geofence id")
	}
	if p := nullIf(Geofence, Hangar, 7); p != nil {
		t.Error("a geofence visit must leave hangar_id null — the CHECK requires exactly one")
	}
}
