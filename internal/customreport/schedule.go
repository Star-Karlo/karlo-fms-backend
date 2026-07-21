package customreport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// Scheduled reports.
//
// THE SCHEDULE OWNS THE PERIOD, not the report. A saved report is a template
// whose window is chosen per run; a schedule derives it from its cadence —
// daily means yesterday, weekly the last seven days, monthly last month.
//
// That is the whole reason this is not "save a report with dates and email it":
// a schedule reusing the report's saved dates would email the same frozen week
// every week, indefinitely, and look completely normal doing it. It also means
// one saved report can go weekly to operations and monthly to management
// without being duplicated.

type Cadence string

const (
	CadenceDaily   Cadence = "daily"
	CadenceWeekly  Cadence = "weekly"
	CadenceMonthly Cadence = "monthly"
)

var cadences = map[Cadence]bool{CadenceDaily: true, CadenceWeekly: true, CadenceMonthly: true}

// Schedule is a saved report plus when and to whom it is sent.
type Schedule struct {
	ID       int64   `json:"id"`
	ReportID int64   `json:"report_id"`
	Cadence  Cadence `json:"cadence"`
	// DayOfWeek is 0=Sunday, weekly only. DayOfMonth is 1-28, monthly only.
	DayOfWeek  *int   `json:"day_of_week,omitempty"`
	DayOfMonth *int   `json:"day_of_month,omitempty"`
	Hour       int    `json:"hour"`
	Timezone   string `json:"timezone"`

	Format     Format   `json:"format"`
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject,omitempty"`
	Enabled    bool     `json:"enabled"`

	NextRunAt  time.Time  `json:"next_run_at"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	LastError  string     `json:"last_error,omitempty"`

	// ReportName is joined in for display, so a list of schedules is readable
	// without a second request per row.
	ReportName string `json:"report_name,omitempty"`
}

// Period returns the window this cadence covers, ending at the moment it runs.
//
// Each is the COMPLETED previous period, not one ending now: a Monday-morning
// weekly report is about last week, and one that ran to "now" would include a
// few hours of Monday and make two consecutive reports overlap.
func (s Schedule) Period(now time.Time, loc *time.Location) Range {
	n := now.In(loc)
	midnight := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	iso := func(t time.Time) string { return t.Format(time.RFC3339) }

	switch s.Cadence {
	case CadenceDaily:
		return Range{From: iso(midnight.AddDate(0, 0, -1)), To: iso(midnight.Add(-time.Second))}
	case CadenceWeekly:
		return Range{From: iso(midnight.AddDate(0, 0, -7)), To: iso(midnight.Add(-time.Second))}
	case CadenceMonthly:
		first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
		return Range{From: iso(first.AddDate(0, -1, 0)), To: iso(first.Add(-time.Second))}
	default:
		return Range{From: iso(midnight.AddDate(0, 0, -7)), To: iso(midnight.Add(-time.Second))}
	}
}

// NextRun computes the next firing strictly after `after`.
//
// Local time throughout: a 07:00 report means 07:00 where the fleet is, and
// computing it in UTC would deliver it at 14:00 WIB.
func (s Schedule) NextRun(after time.Time, loc *time.Location) time.Time {
	a := after.In(loc)
	at := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), s.Hour, 0, 0, 0, loc)
	}

	switch s.Cadence {
	case CadenceDaily:
		next := at(a)
		if !next.After(a) {
			next = next.AddDate(0, 0, 1)
		}
		return next

	case CadenceWeekly:
		want := 1 // Monday by default
		if s.DayOfWeek != nil {
			want = *s.DayOfWeek
		}
		next := at(a)
		for i := 0; i < 8; i++ {
			if int(next.Weekday()) == want && next.After(a) {
				return next
			}
			next = next.AddDate(0, 0, 1)
		}
		return next

	case CadenceMonthly:
		day := 1
		if s.DayOfMonth != nil {
			day = *s.DayOfMonth
		}
		next := time.Date(a.Year(), a.Month(), day, s.Hour, 0, 0, 0, loc)
		if !next.After(a) {
			next = next.AddDate(0, 1, 0)
		}
		return next
	}
	return a.Add(24 * time.Hour)
}

func (s *Schedule) Validate() error {
	if !cadences[s.Cadence] {
		return fmt.Errorf("unknown cadence %q; valid: daily, weekly, monthly", s.Cadence)
	}
	if s.Hour < 0 || s.Hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23")
	}
	if s.Cadence == CadenceWeekly && s.DayOfWeek != nil && (*s.DayOfWeek < 0 || *s.DayOfWeek > 6) {
		return fmt.Errorf("day_of_week must be between 0 (Sunday) and 6")
	}
	if s.Cadence == CadenceMonthly && s.DayOfMonth != nil && (*s.DayOfMonth < 1 || *s.DayOfMonth > 28) {
		// 28, not 31: a schedule for the 30th would skip February entirely,
		// which nobody notices until a month-end report never arrives.
		return fmt.Errorf("day_of_month must be between 1 and 28, so every month has that day")
	}
	if s.Format != "" && !formats[s.Format] {
		return fmt.Errorf("unknown format %q; valid: xlsx, pdf, docx, csv", s.Format)
	}
	if len(s.Recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	if len(s.Recipients) > 20 {
		return fmt.Errorf("too many recipients (%d); maximum 20", len(s.Recipients))
	}
	for _, r := range s.Recipients {
		if !strings.Contains(r, "@") || strings.ContainsAny(r, " \t\r\n,;") {
			return fmt.Errorf("%q is not a valid email address", r)
		}
	}
	if s.Timezone == "" {
		s.Timezone = fleetTZ
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("unknown timezone %q", s.Timezone)
	}
	return nil
}

func (s Schedule) location() *time.Location {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ---- storage ----------------------------------------------------------------

// Two spellings of the same column list. The SELECTs join report_definitions
// and need the alias; INSERT/UPDATE ... RETURNING has no FROM clause, so a
// prefixed name there is a "missing FROM-clause entry" error rather than
// anything that looks like a typo.
const scheduleCols = `s.id, s.report_id, s.cadence, s.day_of_week, s.day_of_month, s.hour, s.timezone,
	s.format, s.recipients, s.subject, s.enabled, s.next_run_at, s.last_run_at, s.last_status, s.last_error`

const scheduleColsBare = `id, report_id, cadence, day_of_week, day_of_month, hour, timezone,
	format, recipients, subject, enabled, next_run_at, last_run_at, last_status, last_error`

func scanSchedule(row pgx.Row, withName bool) (*Schedule, error) {
	var s Schedule
	var name *string
	dest := []any{&s.ID, &s.ReportID, &s.Cadence, &s.DayOfWeek, &s.DayOfMonth, &s.Hour, &s.Timezone,
		&s.Format, &s.Recipients, &s.Subject, &s.Enabled, &s.NextRunAt, &s.LastRunAt, &s.LastStatus, &s.LastError}
	if withName {
		dest = append(dest, &name)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if name != nil {
		s.ReportName = *name
	}
	return &s, nil
}

func (svc *Service) ListSchedules(ctx context.Context, tenantID, userID int64) ([]Schedule, error) {
	out := []Schedule{}
	err := db.Tx(ctx, svc.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT `+scheduleCols+`, d.name
FROM report_schedules s
JOIN report_definitions d ON d.id = s.report_id
WHERE s.user_id = $1 ORDER BY s.updated_at DESC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanSchedule(rows, true)
			if err != nil {
				return err
			}
			out = append(out, *s)
		}
		return rows.Err()
	})
	return out, err
}

func (svc *Service) SaveSchedule(ctx context.Context, tenantID, userID int64, s *Schedule) (*Schedule, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.Format == "" {
		s.Format = FormatPDF
	}
	next := s.NextRun(time.Now(), s.location())

	var out *Schedule
	err := db.Tx(ctx, svc.pool, tenantID, func(tx pgx.Tx) error {
		// The report must belong to this user — RLS scopes the tenant, but a
		// tenant can have several users and a schedule should not be able to
		// email someone else's saved report.
		var owned bool
		if err := tx.QueryRow(ctx,
			`SELECT true FROM report_definitions WHERE id = $1 AND user_id = $2`,
			s.ReportID, userID).Scan(&owned); err != nil {
			return fmt.Errorf("report not found")
		}

		q := `
INSERT INTO report_schedules
  (tenant_id, user_id, report_id, cadence, day_of_week, day_of_month, hour, timezone,
   format, recipients, subject, enabled, next_run_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING ` + scheduleColsBare
		args := []any{tenantID, userID, s.ReportID, s.Cadence, s.DayOfWeek, s.DayOfMonth, s.Hour,
			s.Timezone, s.Format, s.Recipients, s.Subject, s.Enabled, next}

		if s.ID != 0 {
			q = `
UPDATE report_schedules SET
  report_id=$3, cadence=$4, day_of_week=$5, day_of_month=$6, hour=$7, timezone=$8,
  format=$9, recipients=$10, subject=$11, enabled=$12, next_run_at=$13, updated_at=now()
-- tenant_id is redundant with RLS and deliberately kept: it also gives $1 a
-- reference, without which Postgres cannot infer the parameter's type and the
-- UPDATE fails with "could not determine data type of parameter $1" — an error
-- that says nothing about the actual mistake.
WHERE id=$14 AND user_id=$2 AND tenant_id=$1
RETURNING ` + scheduleColsBare
			args = append(args, s.ID)
			args[0] = tenantID
		}
		var err error
		out, err = scanSchedule(tx.QueryRow(ctx, q, args...), false)
		return err
	})
	return out, err
}

func (svc *Service) DeleteSchedule(ctx context.Context, tenantID, userID, id int64) error {
	return db.Tx(ctx, svc.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM report_schedules WHERE id=$1 AND user_id=$2`, id, userID)
		return err
	})
}
