package liveview

import (
	"testing"
	"time"
)

func TestClassifyStop(t *testing.T) {
	on, off := true, false
	road := "Jl. Raya Bekasi"
	mk := func(ign *bool, addr string, n int) []TripPoint {
		pts := make([]TripPoint, n)
		for i := range pts {
			pts[i] = TripPoint{Ignition: ign, Address: addr, Time: time.Now()}
		}
		return pts
	}
	cases := []struct {
		name  string
		c     stopCluster
		pts   []TripPoint
		geo   string
		want  string
		place string
	}{
		{"inside warehouse geofence", stopCluster{0, 3, 0, 0, time.Time{}, time.Time{}, 45}, mk(&on, road, 4), "Gudang Cikarang", "loading", "Gudang Cikarang"},
		{"short engine-on on a road", stopCluster{0, 3, 0, 0, time.Time{}, time.Time{}, 8}, mk(&on, road, 4), "", "traffic", ""},
		{"engine off", stopCluster{0, 3, 0, 0, time.Time{}, time.Time{}, 30}, mk(&off, road, 4), "", "rest", ""},
		{"very long", stopCluster{0, 3, 0, 0, time.Time{}, time.Time{}, 240}, mk(&on, road, 4), "", "rest", ""},
		{"medium engine-on off-road", stopCluster{0, 3, 0, 0, time.Time{}, time.Time{}, 35}, mk(&on, "", 4), "", "unknown", ""},
	}
	for _, tc := range cases {
		got, place := classifyStop(tc.c, tc.pts, tc.geo)
		if got != tc.want || place != tc.place {
			t.Errorf("%s: got %s/%q want %s/%q", tc.name, got, place, tc.want, tc.place)
		}
	}
}
