package path

import (
	"io/fs"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/internal/tracks"
)

var today = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func load(t *testing.T, track string, status map[string]string) ([]Period, []Step) {
	t.Helper()
	sub, _ := fs.Sub(content.FS(), "tracks")
	set, err := tracks.Load(sub, tracks.Refs{})
	if err != nil {
		t.Fatal(err)
	}
	var ps []Period
	for _, p := range set.Periods {
		ps = append(ps, Period{ID: p.ID, Title: p.Title, FromDay: p.FromDay, ToDay: p.ToDay})
	}
	all, _ := set.Path(track)
	var steps []Step
	for _, s := range all {
		steps = append(steps, Step{ID: s.ID, TrackID: s.TrackID, ChecklistRef: s.ChecklistRef, Period: s.Period, Order: s.Order,
			Title: s.Title, Owner: s.Owner, HelpKind: s.Help.Kind, Status: status[s.ID]})
	}
	return ps, steps
}

// P-01
func TestDayAndPeriod(t *testing.T) {
	ps, _ := load(t, "backend", nil)
	cases := []struct {
		ago  int
		day  int
		want string
	}{{0, 1, "week1"}, {3, 4, "week1"}, {7, 8, "week2"}, {14, 15, "month1"}, {90, 91, "after3"}, {-2, -1, "week1"}}
	for _, c := range cases {
		d := Day(today.AddDate(0, 0, -c.ago), today)
		if d != c.day || CurrentPeriod(ps, d) != c.want {
			t.Errorf("ago %d: day %d period %s", c.ago, d, CurrentPeriod(ps, d))
		}
	}
	if CurrentPeriod(nil, 3) != "" {
		t.Fatal("no periods")
	}
}

var demoBE = map[string]string{"cm-1.6": "done", "cm-1.9": "done", "cm-1.10": "waiting", "be-1.11": "in_progress"}

// P-03, P-06, AC-P01, AC-P09: first screen of the Backend demo.
func TestDemoBackend(t *testing.T) {
	ps, steps := load(t, "backend", demoBE)
	v := Compute(ps, steps, today.AddDate(0, 0, -3), today, "common")
	if v.Day != 4 || v.CurrentPeriod != "week1" || v.State != StateReady {
		t.Fatalf("day %d period %s state %s", v.Day, v.CurrentPeriod, v.State)
	}
	if v.Next == nil || v.Next.StepID != "cm-1.7" || v.Next.Reason != ReasonCurrent {
		t.Fatalf("next = %+v", v.Next)
	}
	if len(v.Waiting) != 1 || v.Waiting[0] != "cm-1.10" {
		t.Fatalf("waiting = %v", v.Waiting)
	}
	if v.Progress.PeriodDone != 2 || v.Progress.PeriodTotal != 12 || v.Progress.OverallTotal != 41 {
		t.Fatalf("progress = %+v", v.Progress)
	}
	for _, s := range v.Steps {
		if s.Owner != "intern" {
			t.Fatalf("AC-P04: mentor step %s visible", s.ID)
		}
	}
	if s, ok := v.StepByRef("1.13"); !ok || s.ID != "be-1.13" {
		t.Fatal("StepByRef")
	}
	if _, ok := v.Step("cm-1.1"); ok {
		t.Fatal("mentor step found")
	}
	if v.PeriodTitle("week2") != "Неделя 2 — Инструменты и платформа" || v.PeriodTitle("x") != "x" {
		t.Fatal("PeriodTitle")
	}
	if v.Periods[0].State != "current" || v.Periods[1].State != "future" {
		t.Fatal("period states")
	}
}

// P-03: Frontend demo — the tail of week 1 is overdue.
func TestDemoFrontend(t *testing.T) {
	ps, steps := load(t, "frontend", nil)
	status := map[string]string{}
	for _, s := range steps {
		if s.Period == "week1" && s.ID != "fe-w1-50" {
			status[s.ID] = "done"
		}
	}
	for i := range steps {
		steps[i].Status = status[steps[i].ID]
	}
	v := Compute(ps, steps, today.AddDate(0, 0, -10), today, "common")
	if v.Day != 11 || v.CurrentPeriod != "week2" || v.Next.StepID != "fe-w1-50" || v.Next.Reason != ReasonOverdue {
		t.Fatalf("day %d %s next %+v", v.Day, v.CurrentPeriod, v.Next)
	}
	if v.Periods[0].State != "past" {
		t.Fatal("week1 must be past")
	}
}

// P-02: ties of order — common before the direction, then step id.
func TestSortTie(t *testing.T) {
	ps := []Period{{ID: "week1"}}
	steps := []Step{{ID: "fe-w1-10", TrackID: "frontend", Period: "week1", Order: 10}, {ID: "cm-1.6", TrackID: "common", Period: "week1", Order: 10},
		{ID: "fe-a", TrackID: "frontend", Period: "week1", Order: 5}, {ID: "fe-b", TrackID: "frontend", Period: "week1", Order: 10}}
	Sort(ps, steps, "common")
	got := steps[0].ID + "," + steps[1].ID + "," + steps[2].ID + "," + steps[3].ID
	if got != "fe-a,cm-1.6,fe-b,fe-w1-10" {
		t.Fatal(got)
	}
}

// P-04, P-05, AC-P03, AC-P11, AC-P12
func TestNextRules(t *testing.T) {
	ten := 7
	ps := []Period{{ID: "week1", FromDay: 1, ToDay: &ten}, {ID: "week2", FromDay: 8}}
	mk := func(st ...string) []Step {
		ids := []string{"a", "b", "c"}
		var out []Step
		for i, s := range st {
			p := "week1"
			if i == 2 {
				p = "week2"
			}
			out = append(out, Step{ID: ids[i], TrackID: "common", Period: p, Order: i, Owner: "intern", Status: s})
		}
		return out
	}
	start := today.AddDate(0, 0, -2)
	v := Compute(ps, mk("done", "waiting", "not_started"), start, today, "common")
	if v.Next == nil || v.Next.StepID != "c" || v.Next.Reason != ReasonAhead || v.State != StateReady {
		t.Fatalf("AC-P11: %+v %s", v.Next, v.State)
	}
	v = Compute(ps, mk("done", "waiting", "done"), start, today, "common")
	if v.Next != nil || v.State != StateOnlyWaiting {
		t.Fatalf("only waiting: %+v", v)
	}
	v = Compute(ps, mk("done", "done", "done"), start, today, "common")
	if v.State != StateFinished {
		t.Fatal("finished")
	}
	v = Compute(ps, mk("not_started", "in_progress", "not_started"), start, today, "common")
	if v.Next.StepID != "a" {
		t.Fatal("P-05: in_progress must not overtake an earlier not_started")
	}
	v = Compute(ps, mk("", "", ""), today.AddDate(0, 0, 2), today, "common")
	if v.State != StatePreStart || v.Next.Reason != ReasonAhead || v.Day != -1 {
		t.Fatalf("pre_start: %+v", v)
	}
}

func BenchmarkCompute(b *testing.B) {
	t := &testing.T{}
	ps, steps := load(t, "backend", demoBE)
	for i := 0; i < b.N; i++ {
		Compute(ps, append([]Step(nil), steps...), today.AddDate(0, 0, -3), today, "common")
	}
}
