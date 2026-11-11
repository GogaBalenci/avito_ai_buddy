// Package path computes the intern's adaptation path (11 §1.5): day and
// period from the start date, the order of visible steps, the single "what
// next" step and progress. Pure functions: the logic stays in the buddy when
// the checklist mock is replaced by a real service (11 §0.4).
package path

import (
	"sort"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/checklist/fsm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
)

// Period is a stage with day bounds.
type Period struct {
	ID      string
	Title   string
	FromDay int
	ToDay   *int
}

// Step is a step with its status (from checklist.get_path).
type Step struct {
	ID           string
	TrackID      string
	ChecklistRef string
	Period       string
	Order        int
	Title        string
	Type         string
	Owner        string
	HelpKind     string
	KBDoc        string
	Resource     string
	Synthetic    bool
	Status       string
	RequestRef   string
}

// Next reasons and path states.
const (
	ReasonOverdue = "overdue"
	ReasonCurrent = "current"
	ReasonAhead   = "ahead"

	StateReady       = "ready"
	StatePreStart    = "pre_start"
	StateOnlyWaiting = "only_waiting"
	StateFinished    = "finished"
)

// PeriodView is a period with its state and counters.
type PeriodView struct {
	Period
	State string // past | current | future
	Done  int
	Total int
}

// Next is the single next step.
type Next struct {
	StepID string
	Reason string
}

// Progress counts done steps; waiting is not done.
type Progress struct{ PeriodDone, PeriodTotal, OverallDone, OverallTotal int }

// View is the computed path.
type View struct {
	Today, Start  time.Time
	Day           int
	State         string
	CurrentPeriod string
	Periods       []PeriodView
	Steps         []Step // visible (owner=intern), sorted
	Next          *Next
	Waiting       []string
	Progress      Progress
}

// Day returns (today − start) + 1 in calendar days.
func Day(start, today time.Time) int { return clock.DaysBetween(start, today) + 1 }

// CurrentPeriod returns the period of the day; day < 1 counts as the first
// period (pre-start).
func CurrentPeriod(periods []Period, day int) string {
	if len(periods) == 0 {
		return ""
	}
	if day < 1 {
		return periods[0].ID
	}
	for _, p := range periods {
		if day >= p.FromDay && (p.ToDay == nil || day <= *p.ToDay) {
			return p.ID
		}
	}
	return periods[len(periods)-1].ID
}

// Sort orders steps: period, order ↑, common before the direction, step id
// (11 §1.5.2: order values of common and a direction intersect).
func Sort(periods []Period, steps []Step, commonTrack string) {
	idx := map[string]int{}
	for i, p := range periods {
		idx[p.ID] = i
	}
	sort.SliceStable(steps, func(i, j int) bool {
		a, b := steps[i], steps[j]
		if idx[a.Period] != idx[b.Period] {
			return idx[a.Period] < idx[b.Period]
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		ac, bc := a.TrackID == commonTrack, b.TrackID == commonTrack
		if ac != bc {
			return ac
		}
		return a.ID < b.ID
	})
}

func open(s Step) bool {
	return s.Status == fsm.NotStarted || s.Status == fsm.InProgress || s.Status == ""
}

// Compute builds the view. Only owner=intern steps are visible (11 §1.5.2).
func Compute(periods []Period, all []Step, start, today time.Time, commonTrack string) View {
	v := View{Today: clock.Date(today), Start: clock.Date(start)}
	v.Day = Day(start, today)
	v.CurrentPeriod = CurrentPeriod(periods, v.Day)
	for _, s := range all {
		if s.Owner == "intern" {
			if s.Status == "" {
				s.Status = fsm.NotStarted
			}
			v.Steps = append(v.Steps, s)
		}
	}
	Sort(periods, v.Steps, commonTrack)

	state := map[string]string{}
	seenCurrent := false
	for _, p := range periods {
		switch {
		case p.ID == v.CurrentPeriod:
			state[p.ID] = "current"
			seenCurrent = true
		case !seenCurrent:
			state[p.ID] = "past"
		default:
			state[p.ID] = "future"
		}
	}
	counts := map[string]*[2]int{}
	for _, s := range v.Steps {
		c := counts[s.Period]
		if c == nil {
			c = &[2]int{}
			counts[s.Period] = c
		}
		c[1]++
		v.Progress.OverallTotal++
		if s.Status == fsm.Done {
			c[0]++
			v.Progress.OverallDone++
		}
		if s.Status == fsm.Waiting {
			v.Waiting = append(v.Waiting, s.ID)
		}
	}
	for _, p := range periods {
		pv := PeriodView{Period: p, State: state[p.ID]}
		if c := counts[p.ID]; c != nil {
			pv.Done, pv.Total = c[0], c[1]
		}
		v.Periods = append(v.Periods, pv)
		if p.ID == v.CurrentPeriod {
			v.Progress.PeriodDone, v.Progress.PeriodTotal = pv.Done, pv.Total
		}
	}
	v.Next = next(v.Steps, state, v.CurrentPeriod, periods, v.Day < 1)
	switch {
	case v.Next == nil && len(v.Waiting) > 0:
		v.State = StateOnlyWaiting
	case v.Next == nil:
		v.State = StateFinished
	case v.Day < 1:
		v.State = StatePreStart
	default:
		v.State = StateReady
	}
	return v
}

func next(steps []Step, state map[string]string, current string, periods []Period, preStart bool) *Next {
	for _, s := range steps { // 1. tails of past periods
		if state[s.Period] == "past" && open(s) {
			return &Next{StepID: s.ID, Reason: ReasonOverdue}
		}
	}
	for _, s := range steps { // 2. current period
		if s.Period == current && open(s) {
			if preStart {
				return &Next{StepID: s.ID, Reason: ReasonAhead}
			}
			return &Next{StepID: s.ID, Reason: ReasonCurrent}
		}
	}
	for _, p := range periods { // 3. nearest future period with open steps
		if state[p.ID] != "future" {
			continue
		}
		for _, s := range steps {
			if s.Period == p.ID && open(s) {
				return &Next{StepID: s.ID, Reason: ReasonAhead}
			}
		}
	}
	return nil
}

// StepByRef finds a visible step by id or checklist number ("1.13").
func (v View) StepByRef(ref string) (Step, bool) {
	for _, s := range v.Steps {
		if s.ID == ref || (s.ChecklistRef != "" && s.ChecklistRef == ref) {
			return s, true
		}
	}
	return Step{}, false
}

// Step returns a visible step by id.
func (v View) Step(id string) (Step, bool) {
	for _, s := range v.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// PeriodTitle returns the title of a period.
func (v View) PeriodTitle(id string) string {
	for _, p := range v.Periods {
		if p.ID == id {
			return p.Title
		}
	}
	return id
}
