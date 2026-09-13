package report

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func at(sec int64) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Unix(sec, 0), Valid: true}
}
func stillFitted() pgtype.Timestamptz { return pgtype.Timestamptz{} } // unfitted_at NULL

// clampPeriod intersects the report window with a device's fitted window, so a
// report only ever attributes the telemetry from when a device was on THIS truck.
func TestClampPeriod(t *testing.T) {
	from, to := time.Unix(10, 0), time.Unix(20, 0)
	cases := []struct {
		name             string
		fitted, unfitted pgtype.Timestamptz
		wantLo, wantHi   int64
		empty            bool // hi <= lo → skipped
	}{
		{"fully covers window", at(1), stillFitted(), 10, 20, false},
		{"device left mid-range", at(1), at(15), 10, 15, false},
		{"device arrived mid-range", at(12), stillFitted(), 12, 20, false},
		{"arrived and left within window", at(12), at(18), 12, 18, false},
		{"left before window starts", at(1), at(8), 0, 0, true},
		{"arrived after window ends", at(25), stillFitted(), 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lo, hi := clampPeriod(from, to, c.fitted, c.unfitted)
			if c.empty {
				if hi.After(lo) {
					t.Fatalf("expected empty (hi<=lo), got lo=%d hi=%d", lo.Unix(), hi.Unix())
				}
				return
			}
			if lo.Unix() != c.wantLo || hi.Unix() != c.wantHi {
				t.Fatalf("got [%d,%d], want [%d,%d]", lo.Unix(), hi.Unix(), c.wantLo, c.wantHi)
			}
		})
	}
}
