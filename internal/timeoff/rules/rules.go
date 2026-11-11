// Package rules is the date validation of a time-off request (10 §6). Pure
// functions shared by the API (draft and confirm) and the absences mock; the
// front end repeats them in Zod, and both run docs/specs/s3/validation-cases.json.
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Limits of the test procedure (10 §6.4).
const (
	MaxWorkingDays = 5
	HorizonDays    = 90
)

// Violation codes.
const (
	Required      = "REQUIRED"
	InvalidDate   = "INVALID_DATE"
	DateInPast    = "DATE_IN_PAST"
	TooFar        = "TOO_FAR"
	WeekendDate   = "WEEKEND_DATE"
	RangeReversed = "RANGE_REVERSED"
	TooLong       = "TOO_LONG"
	Overlap       = "OVERLAP"
)

// Violation is one broken rule.
type Violation struct {
	Field  string         `json:"field"` // date_from | date_to | range
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

// Existing is a request of the same employee used for the overlap check.
type Existing struct {
	RequestID string
	DateFrom  time.Time
	DateTo    time.Time
	Status    string
}

// Result of a valid request.
type Result struct {
	From, To    time.Time
	WorkingDays int
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// IsWeekend reports Saturday and Sunday.
func IsWeekend(d time.Time) bool { w := d.Weekday(); return w == time.Saturday || w == time.Sunday }

// WorkingDays counts Monday–Friday in [from, to] inclusive.
func WorkingDays(from, to time.Time) int {
	from, to = dateOnly(from), dateOnly(to)
	n := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if !IsWeekend(d) {
			n++
		}
	}
	return n
}

// MaxDate is today + 90 days.
func MaxDate(today time.Time) time.Time { return dateOnly(today).AddDate(0, 0, HorizonDays) }

func parse(field, s string, today time.Time) (time.Time, *Violation) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, &Violation{Field: field, Code: Required}
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, &Violation{Field: field, Code: InvalidDate}
	}
	switch {
	case d.Before(today):
		return d, &Violation{Field: field, Code: DateInPast, Params: map[string]any{"today": today.Format("2006-01-02")}}
	case d.After(MaxDate(today)):
		return d, &Violation{Field: field, Code: TooFar, Params: map[string]any{"max_date": MaxDate(today).Format("2006-01-02")}}
	case IsWeekend(d):
		return d, &Violation{Field: field, Code: WeekendDate, Params: map[string]any{"date": s, "weekday": int(d.Weekday())}}
	}
	return d, nil
}

// Validate checks the request in the order of 10 §6.2: each field
// (REQUIRED → INVALID_DATE → DATE_IN_PAST → TOO_FAR → WEEKEND_DATE), then
// the range (RANGE_REVERSED → TOO_LONG), then OVERLAP with own registered requests.
func Validate(dateFrom, dateTo string, today time.Time, existing []Existing) (Result, []Violation) {
	today = dateOnly(today)
	var vs []Violation
	from, vf := parse("date_from", dateFrom, today)
	to, vt := parse("date_to", dateTo, today)
	if vf != nil {
		vs = append(vs, *vf)
	}
	if vt != nil {
		vs = append(vs, *vt)
	}
	if len(vs) > 0 {
		return Result{}, vs
	}
	if to.Before(from) {
		return Result{}, []Violation{{Field: "range", Code: RangeReversed}}
	}
	wd := WorkingDays(from, to)
	if wd > MaxWorkingDays {
		return Result{}, []Violation{{Field: "range", Code: TooLong, Params: map[string]any{"working_days": wd, "max": MaxWorkingDays}}}
	}
	for _, e := range existing {
		if e.Status != "registered" {
			continue
		}
		if !from.After(dateOnly(e.DateTo)) && !to.Before(dateOnly(e.DateFrom)) {
			return Result{}, []Violation{{Field: "range", Code: Overlap, Params: map[string]any{
				"request_id": e.RequestID, "date_from": e.DateFrom.Format("2006-01-02"), "date_to": e.DateTo.Format("2006-01-02")}}}
		}
	}
	return Result{From: from, To: to, WorkingDays: wd}, nil
}

// ParamsHash is SHA-256 of the canonical JSON {date_from, date_to, type}
// (keys sorted, no spaces) — the same in the API and the mock (M-15).
func ParamsHash(dateFrom, dateTo string) string {
	b, _ := json.Marshal(map[string]string{"date_from": dateFrom, "date_to": dateTo, "type": "time_off"})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SeedDates are the dates of the demo request ABS-000101: Monday–Tuesday of
// the week three weeks from today (10 §12.4).
func SeedDates(today time.Time) (time.Time, time.Time) {
	d := dateOnly(today).AddDate(0, 0, 21)
	off := (int(d.Weekday()) + 6) % 7 // days since Monday
	mon := d.AddDate(0, 0, -off)
	return mon, mon.AddDate(0, 0, 1)
}
