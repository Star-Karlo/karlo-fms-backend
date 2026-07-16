// Package conv converts between JSON-friendly Go values and the pgtype values
// that sqlc generates for nullable numeric/date/timestamp columns.
package conv

import (
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const dateLayout = "2006-01-02"

// Num builds a pgtype.Numeric from an optional float. nil ⇒ SQL NULL.
func Num(f *float64) pgtype.Numeric {
	var n pgtype.Numeric
	if f == nil {
		return n
	}
	_ = n.Scan(strconv.FormatFloat(*f, 'f', -1, 64))
	return n
}

// NumF converts a pgtype.Numeric to an optional float. Invalid ⇒ nil.
func NumF(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

// Date parses an optional "YYYY-MM-DD" string into a pgtype.Date.
func Date(s *string) (pgtype.Date, error) {
	var d pgtype.Date
	if s == nil || *s == "" {
		return d, nil
	}
	t, err := time.Parse(dateLayout, *s)
	if err != nil {
		return d, err
	}
	d.Time = t
	d.Valid = true
	return d, nil
}

// DateStr formats a pgtype.Date as "YYYY-MM-DD". Invalid ⇒ nil.
func DateStr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format(dateLayout)
	return &s
}

// Time returns the time from a pgtype.Timestamptz. Invalid ⇒ nil.
func Time(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// StrPtr returns nil for an empty string, else a pointer to it. Handy for
// turning empty query params into SQL NULLs (skipping optional filters).
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
