// Package checklist is the mock of the corporate "newcomer checklist" service:
// tracks from YAML and the progress of employees, MCP endpoint /mcp/checklist
// (11 §1.11). Permissions are checked here independently of the buddy (layer 3).
package checklist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/internal/checklist/fsm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mcpx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/tracks"
)

// Service is the checklist mock.
type Service struct {
	Pool      *pgxpool.Pool
	Clock     clock.Clock
	Employees employees.Provider
}

// LoadContent writes tracks, periods and steps into mock_checklist (upsert:
// progress rows reference steps).
func LoadContent(ctx context.Context, tx pgx.Tx, set *tracks.Set) error {
	for i, p := range set.Periods {
		if _, err := tx.Exec(ctx, `INSERT INTO mock_checklist.periods (id, title, from_day, to_day, sort) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, from_day=EXCLUDED.from_day, to_day=EXCLUDED.to_day, sort=EXCLUDED.sort`,
			p.ID, p.Title, p.FromDay, p.ToDay, i+1); err != nil {
			return err
		}
	}
	for _, id := range set.IDs() {
		t, _ := set.Track(id)
		var disc *string
		if t.Disclaimer != "" {
			disc = &t.Disclaimer
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mock_checklist.tracks (track_id, title, synthetic, disclaimer) VALUES ($1,$2,$3,$4)
			ON CONFLICT (track_id) DO UPDATE SET title=EXCLUDED.title, synthetic=EXCLUDED.synthetic, disclaimer=EXCLUDED.disclaimer`,
			t.ID, t.Title, t.Synthetic, disc); err != nil {
			return err
		}
		for _, s := range t.Steps {
			help, _ := json.Marshal(s.Help)
			var ref *string
			if s.ChecklistRef != "" {
				ref = &s.ChecklistRef
			}
			if _, err := tx.Exec(ctx, `INSERT INTO mock_checklist.steps (step_id, track_id, period, "order", title, type, owner, help, checklist_ref, synthetic)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (step_id) DO UPDATE SET track_id=EXCLUDED.track_id, period=EXCLUDED.period, "order"=EXCLUDED."order",
				  title=EXCLUDED.title, type=EXCLUDED.type, owner=EXCLUDED.owner, help=EXCLUDED.help,
				  checklist_ref=EXCLUDED.checklist_ref, synthetic=EXCLUDED.synthetic`,
				s.ID, t.ID, s.Period, s.Order, s.Title, s.Type, s.Owner, help, ref, s.Synthetic); err != nil {
				return fmt.Errorf("step %s: %w", s.ID, err)
			}
		}
	}
	return nil
}

// SeedProgress is a seeded status of a step (11 §1.13).
type SeedProgress struct {
	EmployeeID, StepID, Status string
	RequestRef                 string
}

// Seed writes seed progress rows.
func Seed(ctx context.Context, tx pgx.Tx, now time.Time, rows []SeedProgress) error {
	for _, r := range rows {
		var ref *string
		if r.RequestRef != "" {
			ref = &r.RequestRef
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mock_checklist.progress (employee_id, step_id, status, status_source, request_ref, updated_at)
			VALUES ($1,$2,$3,'seed',$4,$5) ON CONFLICT (employee_id, step_id) DO UPDATE SET status=EXCLUDED.status,
			status_source='seed', request_ref=EXCLUDED.request_ref, updated_at=EXCLUDED.updated_at`,
			r.EmployeeID, r.StepID, r.Status, ref, now); err != nil {
			return fmt.Errorf("seed progress %s/%s: %w", r.EmployeeID, r.StepID, err)
		}
	}
	return nil
}

// Register adds the three tools to the endpoint server.
func (s *Service) Register(srv *mcp.Server, log *slog.Logger) {
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "checklist", Name: "get_path", ReadOnly: true,
		Description: "Путь сотрудника: общий трек + трек направления, периоды и шаги со статусами. Стажёр — свой; наставник — своих стажёров (только чтение).",
		Handler:     s.getPath})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "checklist", Name: "get_step", ReadOnly: true,
		Description: "Шаг пути сотрудника со статусом. Шаг не из пути или не для стажёра — NOT_FOUND.",
		Handler:     s.getStep})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "checklist", Name: "set_step_status", Idempotent: true,
		Description: "Сменить статус шага по таблице переходов. Идемпотентно по idempotency_key.",
		Handler:     s.setStepStatus})
}

// target resolves the employee whose path is read and checks the actor
// (11 §1.9): intern — self; mentor — own intern, employee_id required.
func (s *Service) target(ctx context.Context, a mcpx.Actor, employeeID string) (employees.Employee, error) {
	switch a.Role {
	case "intern":
		if employeeID != "" && employeeID != a.UserID {
			return employees.Employee{}, mcpx.Denied("an intern reads only own path")
		}
		employeeID = a.UserID
	case "mentor":
		if employeeID == "" {
			return employees.Employee{}, mcpx.Denied("employee_id is required for a mentor")
		}
	default:
		return employees.Employee{}, mcpx.Denied("role is not allowed")
	}
	e, err := s.Employees.Get(ctx, employeeID)
	if errors.Is(err, employees.ErrNotFound) {
		return e, mcpx.NotFound("employee not found")
	}
	if err != nil {
		return e, err
	}
	if a.Role == "mentor" && (e.Role != "intern" || e.MentorID != a.UserID) {
		return e, mcpx.Denied("not an intern of this mentor")
	}
	if e.Role != "intern" || e.Track == "" {
		return e, mcpx.NotFound("employee has no path")
	}
	return e, nil
}

// Step is step.v1.json.
type Step struct {
	StepID          string          `json:"step_id"`
	TrackID         string          `json:"track_id"`
	ChecklistRef    *string         `json:"checklist_ref"`
	Period          string          `json:"period"`
	Order           int             `json:"order"`
	Title           string          `json:"title"`
	Type            string          `json:"type"`
	Owner           string          `json:"owner"`
	Help            json.RawMessage `json:"help"`
	Synthetic       bool            `json:"synthetic"`
	Status          string          `json:"status"`
	StatusUpdatedAt *time.Time      `json:"status_updated_at"`
	StatusSource    *string         `json:"status_source"`
	RequestRef      *string         `json:"request_ref"`
}

const stepSelect = `SELECT s.step_id, s.track_id, s.checklist_ref, s.period, s."order", s.title, s.type, s.owner, s.help, s.synthetic,
	COALESCE(p.status, 'not_started'), p.updated_at, p.status_source, p.request_ref
	FROM mock_checklist.steps s
	JOIN mock_checklist.periods pr ON pr.id = s.period
	LEFT JOIN mock_checklist.progress p ON p.step_id = s.step_id AND p.employee_id = $1`

func scanStep(row pgx.Row) (Step, error) {
	var st Step
	err := row.Scan(&st.StepID, &st.TrackID, &st.ChecklistRef, &st.Period, &st.Order, &st.Title, &st.Type, &st.Owner, &st.Help,
		&st.Synthetic, &st.Status, &st.StatusUpdatedAt, &st.StatusSource, &st.RequestRef)
	return st, err
}

func (s *Service) getPath(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor      mcpx.Actor `json:"actor"`
		EmployeeID string     `json:"employee_id"`
		Owners     []string   `json:"owners"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	for _, o := range in.Owners {
		if o != "intern" {
			return nil, mcpx.Denied("owners wider than [intern] are not allowed for this role")
		}
	}
	e, err := s.target(ctx, in.Actor, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	var track struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Synthetic  bool    `json:"synthetic"`
		Disclaimer *string `json:"disclaimer"`
	}
	if err := s.Pool.QueryRow(ctx, `SELECT track_id, title, synthetic, disclaimer FROM mock_checklist.tracks WHERE track_id = $1`, e.Track).
		Scan(&track.ID, &track.Title, &track.Synthetic, &track.Disclaimer); err != nil {
		return nil, mcpx.NotFound("track not found")
	}
	type period struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		FromDay int    `json:"from_day"`
		ToDay   *int   `json:"to_day"`
	}
	periods := []period{}
	rows, err := s.Pool.Query(ctx, `SELECT id, title, from_day, to_day FROM mock_checklist.periods ORDER BY sort`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.ID, &p.Title, &p.FromDay, &p.ToDay); err != nil {
			rows.Close()
			return nil, err
		}
		periods = append(periods, p)
	}
	rows.Close()
	steps := []Step{}
	rows, err = s.Pool.Query(ctx, stepSelect+` WHERE s.track_id IN ('common', $2) AND s.owner = 'intern'
		ORDER BY pr.sort, s."order", (s.track_id <> 'common'), s.step_id`, e.UserID, e.Track)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		st, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		steps = append(steps, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"employee_id": e.UserID, "track": track, "periods": periods, "steps": steps}, nil
}

func (s *Service) step(ctx context.Context, q pgx.Tx, e employees.Employee, stepID string, lock bool) (Step, error) {
	sql := stepSelect + ` WHERE s.step_id = $2 AND s.track_id IN ('common', $3) AND s.owner = 'intern'`
	var row pgx.Row
	if q != nil {
		if lock {
			// lock the progress row (if any) so concurrent updates serialize
			if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || '/' || $2))`, e.UserID, stepID); err != nil {
				return Step{}, err
			}
		}
		row = q.QueryRow(ctx, sql, e.UserID, stepID, e.Track)
	} else {
		row = s.Pool.QueryRow(ctx, sql, e.UserID, stepID, e.Track)
	}
	st, err := scanStep(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, mcpx.NotFound("step is not in the employee's path")
	}
	return st, err
}

func (s *Service) getStep(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor      mcpx.Actor `json:"actor"`
		StepID     string     `json:"step_id"`
		EmployeeID string     `json:"employee_id"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	e, err := s.target(ctx, in.Actor, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	st, err := s.step(ctx, nil, e, in.StepID, false)
	if err != nil {
		return nil, err
	}
	return map[string]any{"step": st}, nil
}

func (s *Service) setStepStatus(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor          mcpx.Actor `json:"actor"`
		EmployeeID     string     `json:"employee_id"`
		StepID         string     `json:"step_id"`
		Status         string     `json:"status"`
		ExpectedStatus string     `json:"expected_status"`
		Reason         string     `json:"reason"`
		RequestRef     string     `json:"request_ref"`
		Key            string     `json:"idempotency_key"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	// 2. guard: user_* only intern on own path; access_* only system
	system := fsm.IsSystemReason(in.Reason)
	var e employees.Employee
	switch {
	case in.Actor.Role == "intern" && !system:
		var err error
		if e, err = s.target(ctx, in.Actor, in.EmployeeID); err != nil {
			return nil, err
		}
	case in.Actor.Role == "system" && system:
		if in.EmployeeID == "" {
			return nil, mcpx.Err("VALIDATION_FAILED", "employee_id is required for system", nil)
		}
		if in.RequestRef == "" {
			return nil, mcpx.Err("VALIDATION_FAILED", "request_ref is required for access_* reasons", nil)
		}
		var err error
		e, err = s.Employees.Get(ctx, in.EmployeeID)
		if err != nil || e.Role != "intern" {
			return nil, mcpx.NotFound("employee not found")
		}
	default:
		return nil, mcpx.Denied("this role may not use this reason")
	}
	var result map[string]any
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		st, err := s.step(ctx, tx, e, in.StepID, true) // 3. step in path
		if err != nil {
			return err
		}
		// 4. idempotency key
		var prev struct {
			step, status, reason string
			result               []byte
		}
		err = tx.QueryRow(ctx, `SELECT step_id, status, reason, result FROM mock_checklist.idempotency WHERE idempotency_key = $1`, in.Key).
			Scan(&prev.step, &prev.status, &prev.reason, &prev.result)
		if err == nil {
			if prev.step != in.StepID || prev.status != in.Status || prev.reason != in.Reason {
				return mcpx.Err("IDEMPOTENCY_CONFLICT", "idempotency_key was used with other parameters", nil)
			}
			if err := json.Unmarshal(prev.result, &result); err != nil {
				return err
			}
			result["idempotent_replay"] = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// 5. optimistic lock
		if in.ExpectedStatus != "" && in.ExpectedStatus != st.Status {
			return mcpx.Err("STATUS_CONFLICT", "status has changed", map[string]any{"current_status": st.Status})
		}
		// 6. transition table
		var help struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(st.Help, &help)
		changed, err := fsm.Check(st.Status, in.Status, in.Reason, help.Kind, in.RequestRef)
		if errors.Is(err, fsm.ErrRequestRefNeed) {
			return mcpx.Err("VALIDATION_FAILED", err.Error(), nil)
		}
		if err != nil {
			return mcpx.Err("TRANSITION_NOT_ALLOWED", fmt.Sprintf("%s → %s by %s is not allowed", st.Status, in.Status, in.Reason),
				map[string]any{"current_status": st.Status})
		}
		now := s.Clock.Now()
		if changed { // 8. upsert progress
			src := "user"
			if system {
				src = "system"
			}
			var ref *string
			if in.RequestRef != "" {
				ref = &in.RequestRef
			}
			if _, err := tx.Exec(ctx, `INSERT INTO mock_checklist.progress (employee_id, step_id, status, status_source, request_ref, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (employee_id, step_id) DO UPDATE SET status=EXCLUDED.status,
				status_source=EXCLUDED.status_source, request_ref=COALESCE(EXCLUDED.request_ref, mock_checklist.progress.request_ref),
				updated_at=EXCLUDED.updated_at`, e.UserID, in.StepID, in.Status, src, ref, now); err != nil {
				return err
			}
			if st, err = s.step(ctx, tx, e, in.StepID, false); err != nil {
				return err
			}
		}
		result = map[string]any{"step": st, "changed": changed, "idempotent_replay": false}
		b, _ := json.Marshal(result)
		_, err = tx.Exec(ctx, `INSERT INTO mock_checklist.idempotency (idempotency_key, employee_id, step_id, status, reason, result, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.Key, e.UserID, in.StepID, in.Status, in.Reason, b, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
