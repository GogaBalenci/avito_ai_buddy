// Package clock is the Clock interface (12 §5.1): "today" is computed only on
// the server, in Europe/Moscow; tests use a fake. SQL gets the time as a
// parameter ($now), never now().
package clock

import "time"

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Real is the wall clock in a location.
type Real struct{ Loc *time.Location }

// Now returns the current time in the location.
func (r Real) Now() time.Time {
	if r.Loc == nil {
		return time.Now()
	}
	return time.Now().In(r.Loc)
}

// Date drops the time of day: a calendar date as midnight UTC, so date
// arithmetic never depends on a time zone.
func Date(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Today returns the calendar date of the clock.
func Today(c Clock) time.Time { return Date(c.Now()) }

// DaysBetween returns b − a in whole calendar days.
func DaysBetween(a, b time.Time) int { return int(Date(b).Sub(Date(a)).Hours() / 24) }

// ISO formats a date as YYYY-MM-DD.
func ISO(t time.Time) string { return t.Format("2006-01-02") }

// ParseISO parses YYYY-MM-DD strictly (2026-02-30 is an error).
func ParseISO(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
