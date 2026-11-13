package report

import (
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// Telemetry and Postgres hand back UTC. Formatting as-is printed every report
// time seven hours behind what the fleet read on the clock.
func TestTsFormatsInWIB(t *testing.T) {
	instant := time.Date(2026, 11, 12, 17, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"UTC crosses WIB midnight", instant, "2026-11-13 00:30"},
		{"already +07:00", instant.In(time.FixedZone("", 7*3600)), "2026-11-13 00:30"},
		{"any other zone", instant.In(time.FixedZone("", -5*3600)), "2026-11-13 00:30"},
		{"zero", time.Time{}, ""},
	}
	for _, c := range cases {
		if got := ts(c.in); got != c.want {
			t.Errorf("%s: ts() = %q, want %q", c.name, got, c.want)
		}
	}
}

// 17:00Z is WIB midnight, so the night shift must not be booked to the
// previous day.
func TestUsageByDaySplitsAtWIBMidnight(t *testing.T) {
	lat, lon, speed := -6.2, 106.8, 0.0
	at := func(h, m int) telemetry.Position {
		return telemetry.Position{Time: time.Date(2026, 11, 12, h, m, 0, 0, time.UTC), Lat: &lat, Lon: &lon, Speed: &speed}
	}
	got := usageByDay([]telemetry.Position{at(16, 40), at(16, 50), at(17, 10)})

	want := []dayUsage{{day: "2026-11-12", idleMin: 10}, {day: "2026-11-13", idleMin: 20}}
	if len(got) != len(want) {
		t.Fatalf("got %d days %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i].day != want[i].day || got[i].idleMin != want[i].idleMin {
			t.Errorf("day %d = %+v, want day %s idle %.0f", i, got[i], want[i].day, want[i].idleMin)
		}
	}
}
