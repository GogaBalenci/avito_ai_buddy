package clock

import (
	"testing"
	"time"
)

func TestDates(t *testing.T) {
	msk, _ := time.LoadLocation("Europe/Moscow")
	late := time.Date(2026, 11, 10, 23, 30, 0, 0, msk) // 20:30 UTC, still the 10th in Moscow
	if ISO(Date(late)) != "2026-11-10" {
		t.Fatal(ISO(Date(late)))
	}
	if DaysBetween(time.Date(2026, 11, 7, 23, 0, 0, 0, msk), late) != 3 {
		t.Fatal("days")
	}
	if _, err := ParseISO("2026-02-30"); err == nil {
		t.Fatal("2026-02-30 parsed")
	}
	if d, err := ParseISO("2026-11-13"); err != nil || d.Weekday() != time.Friday {
		t.Fatal(d, err)
	}
	r := Real{Loc: msk}
	if r.Now().Location() != msk || (Real{}).Now().IsZero() || Today(r).Hour() != 0 {
		t.Fatal("Real")
	}
}
