package customreport

import (
	"testing"
	"time"
)

func jkt(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	return loc
}

func day(t *testing.T, loc *time.Location, y int, m time.Month, d, h int) time.Time {
	t.Helper()
	return time.Date(y, m, d, h, 0, 0, 0, loc)
}

// The period comes from the CADENCE, not from the report. A schedule reusing
// the report's saved dates would email the same frozen week forever and look
// entirely normal doing it.
func TestPeriodIsTheCompletedPreviousWindow(t *testing.T) {
	loc := jkt(t)
	// Monday 21 July 2026, 07:00 — a weekly report going out that morning.
	now := day(t, loc, 2026, time.July, 21, 7)

	weekly := Schedule{Cadence: CadenceWeekly}.Period(now, loc)
	from, _ := time.Parse(time.RFC3339, weekly.From)
	to, _ := time.Parse(time.RFC3339, weekly.To)
	if got := from.In(loc).Format("2006-01-02 15:04"); got != "2026-07-14 00:00" {
		t.Errorf("weekly from = %s, want 2026-07-14 00:00", got)
	}
	// Ends at the end of YESTERDAY. A window running to "now" would include
	// part of Monday, so consecutive weekly reports would overlap.
	if got := to.In(loc).Format("2006-01-02 15:04"); got != "2026-07-20 23:59" {
		t.Errorf("weekly to = %s, want 2026-07-20 23:59 (not today)", got)
	}

	daily := Schedule{Cadence: CadenceDaily}.Period(now, loc)
	dFrom, _ := time.Parse(time.RFC3339, daily.From)
	if got := dFrom.In(loc).Format("2006-01-02"); got != "2026-07-20" {
		t.Errorf("daily from = %s, want yesterday 2026-07-20", got)
	}

	// Monthly on 1 August covers the WHOLE of July, not the last 30 days.
	monthly := Schedule{Cadence: CadenceMonthly}.Period(day(t, loc, 2026, time.August, 1, 7), loc)
	mFrom, _ := time.Parse(time.RFC3339, monthly.From)
	mTo, _ := time.Parse(time.RFC3339, monthly.To)
	if got := mFrom.In(loc).Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("monthly from = %s, want 2026-07-01", got)
	}
	if got := mTo.In(loc).Format("2006-01-02"); got != "2026-07-31" {
		t.Errorf("monthly to = %s, want 2026-07-31", got)
	}
}

// Periods must not overlap or leave gaps between consecutive runs, or numbers
// are double-counted or missed and nothing says so.
func TestConsecutiveWeeklyPeriodsAreContiguous(t *testing.T) {
	loc := jkt(t)
	s := Schedule{Cadence: CadenceWeekly, Hour: 7}

	first := s.Period(day(t, loc, 2026, time.July, 14, 7), loc)
	second := s.Period(day(t, loc, 2026, time.July, 21, 7), loc)

	firstTo, _ := time.Parse(time.RFC3339, first.To)
	secondFrom, _ := time.Parse(time.RFC3339, second.From)

	gap := secondFrom.Sub(firstTo)
	if gap < 0 {
		t.Errorf("periods overlap by %v — rows counted twice", -gap)
	}
	if gap > time.Minute {
		t.Errorf("gap of %v between consecutive weeks — data falls through", gap)
	}
}

func TestNextRunIsAlwaysInTheFuture(t *testing.T) {
	loc := jkt(t)
	mon := 1
	dom := 15
	cases := []Schedule{
		{Cadence: CadenceDaily, Hour: 7},
		{Cadence: CadenceDaily, Hour: 0},
		{Cadence: CadenceWeekly, Hour: 7, DayOfWeek: &mon},
		{Cadence: CadenceMonthly, Hour: 7, DayOfMonth: &dom},
	}
	// Including the exact firing instant: "now" must schedule the NEXT one, or
	// the runner claims the same row repeatedly and sends in a loop.
	for _, now := range []time.Time{
		day(t, loc, 2026, time.July, 21, 6),
		day(t, loc, 2026, time.July, 21, 7),
		day(t, loc, 2026, time.July, 21, 23),
		day(t, loc, 2026, time.July, 15, 7),
	} {
		for _, s := range cases {
			next := s.NextRun(now, loc)
			if !next.After(now) {
				t.Errorf("%s at %02d:00: next run %v is not after %v", s.Cadence, s.Hour, next, now)
			}
			if next.In(loc).Hour() != s.Hour {
				t.Errorf("%s: next run at %d:00 local, want %d:00", s.Cadence, next.In(loc).Hour(), s.Hour)
			}
		}
	}
}

func TestWeeklyLandsOnTheChosenDay(t *testing.T) {
	loc := jkt(t)
	for dow := 0; dow <= 6; dow++ {
		d := dow
		s := Schedule{Cadence: CadenceWeekly, Hour: 7, DayOfWeek: &d}
		next := s.NextRun(day(t, loc, 2026, time.July, 21, 12), loc)
		if int(next.In(loc).Weekday()) != dow {
			t.Errorf("day_of_week=%d fired on %v", dow, next.In(loc).Weekday())
		}
		if next.Sub(day(t, loc, 2026, time.July, 21, 12)) > 8*24*time.Hour {
			t.Errorf("day_of_week=%d scheduled more than a week out: %v", dow, next)
		}
	}
}

func TestValidation(t *testing.T) {
	ok := []string{"ops@karlo.id"}
	for _, tc := range []struct {
		name string
		s    Schedule
		bad  bool
	}{
		{"valid", Schedule{Cadence: CadenceWeekly, Hour: 7, Recipients: ok, Format: FormatPDF}, false},
		{"unknown cadence", Schedule{Cadence: "fortnightly", Recipients: ok}, true},
		{"hour out of range", Schedule{Cadence: CadenceDaily, Hour: 24, Recipients: ok}, true},
		{"no recipients", Schedule{Cadence: CadenceDaily, Hour: 7}, true},
		{"malformed recipient", Schedule{Cadence: CadenceDaily, Hour: 7, Recipients: []string{"not-an-email"}}, true},
		// Header injection: a newline in a recipient can add headers to the
		// outgoing message.
		{"newline in recipient", Schedule{Cadence: CadenceDaily, Hour: 7, Recipients: []string{"a@b.c\nBcc: x@y.z"}}, true},
		{"unknown format", Schedule{Cadence: CadenceDaily, Hour: 7, Recipients: ok, Format: "pptx"}, true},
		{"unknown timezone", Schedule{Cadence: CadenceDaily, Hour: 7, Recipients: ok, Timezone: "Mars/Olympus"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.s
			err := s.Validate()
			if tc.bad && err == nil {
				t.Error("expected a validation error")
			}
			if !tc.bad && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// A monthly schedule on the 30th would skip February entirely — the kind of
// gap nobody notices until a month-end report never arrives.
func TestMonthlyDayIsCappedSoEveryMonthHasIt(t *testing.T) {
	for _, d := range []int{29, 30, 31} {
		day := d
		s := Schedule{Cadence: CadenceMonthly, Hour: 7, DayOfMonth: &day, Recipients: []string{"a@b.c"}}
		if err := s.Validate(); err == nil {
			t.Errorf("day_of_month=%d was accepted; February has no such day", d)
		}
	}
	valid := 28
	s := Schedule{Cadence: CadenceMonthly, Hour: 7, DayOfMonth: &valid, Recipients: []string{"a@b.c"}}
	if err := s.Validate(); err != nil {
		t.Errorf("day_of_month=28 should be valid: %v", err)
	}
}
