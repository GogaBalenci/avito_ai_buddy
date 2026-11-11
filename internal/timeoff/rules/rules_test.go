package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type vector struct {
	Today      string `json:"today"`
	EmployeeID string `json:"employee_id"`
	Cases      []struct {
		ID       string `json:"id"`
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
		Existing []struct {
			RequestID  string `json:"request_id"`
			EmployeeID string `json:"employee_id"`
			DateFrom   string `json:"date_from"`
			DateTo     string `json:"date_to"`
			Status     string `json:"status"`
		} `json:"existing"`
		Expect struct {
			OK          bool        `json:"ok"`
			WorkingDays int         `json:"working_days"`
			Violations  []Violation `json:"violations"`
		} `json:"expect"`
	} `json:"cases"`
}

// V-01…V-21 from the shared vectors (10 §16.1).
func TestValidationVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "specs", "s3", "validation-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	today, _ := time.Parse("2006-01-02", v.Today)
	if len(v.Cases) != 21 {
		t.Fatalf("cases = %d", len(v.Cases))
	}
	for _, c := range v.Cases {
		t.Run(c.ID, func(t *testing.T) {
			var ex []Existing
			for _, e := range c.Existing {
				if e.EmployeeID != v.EmployeeID {
					continue // overlap only with own requests (V-20)
				}
				f, _ := time.Parse("2006-01-02", e.DateFrom)
				to, _ := time.Parse("2006-01-02", e.DateTo)
				ex = append(ex, Existing{RequestID: e.RequestID, DateFrom: f, DateTo: to, Status: e.Status})
			}
			res, vs := Validate(c.DateFrom, c.DateTo, today.Add(15*time.Hour), ex)
			if c.Expect.OK {
				if len(vs) != 0 || res.WorkingDays != c.Expect.WorkingDays {
					t.Fatalf("got %+v %+v", res, vs)
				}
				return
			}
			if len(vs) != len(c.Expect.Violations) {
				t.Fatalf("got %+v, want %+v", vs, c.Expect.Violations)
			}
			for i, w := range c.Expect.Violations {
				if vs[i].Field != w.Field || vs[i].Code != w.Code {
					t.Fatalf("violation %d: got %+v, want %+v", i, vs[i], w)
				}
			}
		})
	}
}

func TestWorkingDaysAndHelpers(t *testing.T) {
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }
	if WorkingDays(d("2026-11-13"), d("2026-11-16")) != 2 || WorkingDays(d("2026-11-16"), d("2026-11-20")) != 5 || WorkingDays(d("2026-11-13"), d("2026-11-13")) != 1 {
		t.Fatal("WorkingDays")
	}
	h1, h2 := ParamsHash("2026-11-13", "2026-11-13"), ParamsHash("2026-11-13", "2026-11-13")
	if h1 != h2 || h1 == ParamsHash("2026-11-13", "2026-11-14") || len(ParamsHash("a", "b")) != 64 {
		t.Fatal("hash")
	}
	from, to := SeedDates(d("2026-11-10"))
	if from.Format("2006-01-02") != "2026-11-30" || to.Format("2006-01-02") != "2026-12-01" || from.Weekday() != time.Monday {
		t.Fatalf("seed %s %s", from, to)
	}
	from, _ = SeedDates(d("2026-11-15")) // Sunday
	if from.Weekday() != time.Monday || from.Before(d("2026-11-30")) {
		t.Fatalf("seed from Sunday: %s", from)
	}
	if MaxDate(d("2026-11-10")).Format("2006-01-02") != "2027-02-08" {
		t.Fatal("max date")
	}
}

func TestMessages(t *testing.T) {
	cases := map[string]Violation{
		"Укажи дату":        {Code: Required},
		"Не похоже на дату": {Code: InvalidDate},
		"Дата уже прошла — выбери сегодня или позже":                       {Code: DateInPast},
		"Можно выбрать дату не позже 08.02.2027":                           {Code: TooFar, Params: map[string]any{"max_date": "2027-02-08"}},
		"14.11 — суббота. Отгул начинается и заканчивается в рабочий день": {Code: WeekendDate, Params: map[string]any{"date": "2026-11-14", "weekday": 6}},
		"«До» не может быть раньше «от»":                                   {Code: RangeReversed},
		"Пересекается с заявкой ABS-000101 на 30.11–01.12":                 {Code: Overlap, Params: map[string]any{"request_id": "ABS-000101", "date_from": "2026-11-30", "date_to": "2026-12-01"}},
		"Сервис отклонил даты: проверь их или спроси наставника":           {Code: "WHAT"},
	}
	for want, v := range cases {
		if got := Message(v); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if !strings.HasPrefix(Message(Violation{Code: TooLong, Params: map[string]any{"working_days": 6}}), "Выбрано 6 рабочих") {
		t.Error("too long")
	}
	if ddmm("x") != "x" {
		t.Error("ddmm")
	}
}
