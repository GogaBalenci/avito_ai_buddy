package path

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

// Track describes the employee's direction.
type Track struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Synthetic  bool    `json:"synthetic"`
	Disclaimer *string `json:"disclaimer"`
}

// Request is the local copy of an access request bound to a step (S4).
type Request struct {
	RequestID     string
	Status        string
	AutoApproveAt *time.Time
	RejectReason  *string
	ResourceTitle string
	Policy        string
}

// Loaded is a computed path with its owner.
type Loaded struct {
	View
	Employee employees.Employee
	Track    Track
	Requests map[string]Request // by step id
}

// Service reads the path through ToolClient and computes the view.
type Service struct {
	Tools     mcpclient.Client
	Employees employees.Provider
	Clock     clock.Clock
	Guard     *guard.Guard
	Pool      *pgxpool.Pool // local copy of access requests (app.access_requests)
}

// RawStep is a step as returned by checklist.get_path / get_step.
type RawStep struct {
	StepID       string  `json:"step_id"`
	TrackID      string  `json:"track_id"`
	ChecklistRef *string `json:"checklist_ref"`
	Period       string  `json:"period"`
	Order        int     `json:"order"`
	Title        string  `json:"title"`
	Type         string  `json:"type"`
	Owner        string  `json:"owner"`
	Help         struct {
		Kind     string `json:"kind"`
		KBDoc    string `json:"kb_doc"`
		Resource string `json:"resource"`
	} `json:"help"`
	Synthetic  bool    `json:"synthetic"`
	Status     string  `json:"status"`
	RequestRef *string `json:"request_ref"`
}

// ToStep converts a raw step.
func (r RawStep) ToStep() Step {
	s := Step{ID: r.StepID, TrackID: r.TrackID, Period: r.Period, Order: r.Order, Title: r.Title, Type: r.Type,
		Owner: r.Owner, HelpKind: r.Help.Kind, KBDoc: r.Help.KBDoc, Resource: r.Help.Resource, Synthetic: r.Synthetic, Status: r.Status}
	if r.ChecklistRef != nil {
		s.ChecklistRef = *r.ChecklistRef
	}
	if r.RequestRef != nil {
		s.RequestRef = *r.RequestRef
	}
	return s
}

// ErrNoStart is returned for an intern without a start date.
var ErrNoStart = errors.New("employee has no start date")

// Load returns the path of employeeID ("" — the actor's own) after the guard.
func (s *Service) Load(ctx context.Context, a actor.Actor, employeeID string) (*Loaded, error) {
	if err := s.Guard.Authorize(ctx, a, guard.PathRead, guard.Object{EmployeeID: employeeID}); err != nil {
		return nil, err
	}
	target := employeeID
	if target == "" {
		target = a.UserID
	}
	e, err := s.Employees.Get(ctx, target)
	if err != nil {
		return nil, err
	}
	if e.StartDate == nil {
		return nil, ErrNoStart
	}
	in := map[string]any{"actor": a}
	if a.Role == actor.Mentor {
		in["employee_id"] = target
	}
	var out struct {
		Track   Track `json:"track"`
		Periods []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			FromDay int    `json:"from_day"`
			ToDay   *int   `json:"to_day"`
		} `json:"periods"`
		Steps []RawStep `json:"steps"`
	}
	if err := s.Tools.Call(ctx, "checklist", "get_path", in, &out); err != nil {
		return nil, err
	}
	periods := make([]Period, 0, len(out.Periods))
	for _, p := range out.Periods {
		periods = append(periods, Period{ID: p.ID, Title: p.Title, FromDay: p.FromDay, ToDay: p.ToDay})
	}
	steps := make([]Step, 0, len(out.Steps))
	for _, r := range out.Steps {
		steps = append(steps, r.ToStep())
	}
	l := &Loaded{View: Compute(periods, steps, *e.StartDate, s.Clock.Now(), "common"), Employee: e, Track: out.Track}
	l.Requests, err = s.Requests(ctx, target)
	return l, err
}

// Requests returns the latest local access request of each step.
func (s *Service) Requests(ctx context.Context, userID string) (map[string]Request, error) {
	out := map[string]Request{}
	if s.Pool == nil {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT ON (step_id) step_id, request_id, status, auto_approve_at, reject_reason, resource_title, policy
		FROM app.access_requests WHERE user_id = $1 AND step_id IS NOT NULL ORDER BY step_id, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stepID string
		var r Request
		if err := rows.Scan(&stepID, &r.RequestID, &r.Status, &r.AutoApproveAt, &r.RejectReason, &r.ResourceTitle, &r.Policy); err != nil {
			return nil, err
		}
		out[stepID] = r
	}
	return out, rows.Err()
}

// GetStep reads one step of the actor's path (404 if it is not there).
func (s *Service) GetStep(ctx context.Context, a actor.Actor, stepID string) (Step, error) {
	var out struct {
		Step RawStep `json:"step"`
	}
	if err := s.Tools.Call(ctx, "checklist", "get_step", map[string]any{"actor": a, "step_id": stepID}, &out); err != nil {
		return Step{}, err
	}
	return out.Step.ToStep(), nil
}

// SetStatus changes a step status for the intern (user_* reasons).
func (s *Service) SetStatus(ctx context.Context, a actor.Actor, stepID, status, expected, reason, key string) (Step, bool, bool, error) {
	in := map[string]any{"actor": a, "step_id": stepID, "status": status, "reason": reason, "idempotency_key": key}
	if expected != "" {
		in["expected_status"] = expected
	}
	var out struct {
		Step    RawStep `json:"step"`
		Changed bool    `json:"changed"`
		Replay  bool    `json:"idempotent_replay"`
	}
	if err := s.Tools.Call(ctx, "checklist", "set_step_status", in, &out); err != nil {
		return Step{}, false, false, err
	}
	return out.Step.ToStep(), out.Changed, out.Replay, nil
}
