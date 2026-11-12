// Package accesssync returns the status of access requests into «Мой путь»
// (11 §3.6): the access service knows nothing about steps, the link
// «request ↔ step» is ours. It is the only code that acts as `system`.
//
// Order: first the step (idempotent), then the local copy with a conditional
// UPDATE — so a step never stays `waiting` for an approved request, and
// access_request_decided is published exactly once even with parallel polls.
package accesssync

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/auth/sysactor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

// Request is access/request.v1.json.
type Request struct {
	RequestID     string     `json:"request_id"`
	EmployeeID    string     `json:"employee_id"`
	ResourceID    string     `json:"resource_id"`
	Status        string     `json:"status"`
	Policy        string     `json:"policy"`
	AccessLevel   string     `json:"access_level"`
	Justification *string    `json:"justification"`
	CreatedAt     time.Time  `json:"created_at"`
	AutoApproveAt *time.Time `json:"auto_approve_at"`
	DecidedAt     *time.Time `json:"decided_at"`
	DecidedBy     *string    `json:"decided_by"`
	RejectReason  *string    `json:"reject_reason"`
}

// Syncer synchronizes local copies with the access service.
type Syncer struct {
	Tools  mcpclient.Client
	Pool   *pgxpool.Pool
	Events events.Publisher
	Clock  clock.Clock
}

// stepReason maps a request status to the checklist transition.
func stepReason(status string) (target, reason string) {
	switch status {
	case "approved":
		return "done", "access_approved"
	case "rejected":
		return "in_progress", "access_rejected"
	}
	return "waiting", "access_pending"
}

// SetStep moves the step of a request as `system`; idempotent by
// uuid5(request_id + status). A transition that is no longer possible
// (the step is already there) is not an error.
func (s *Syncer) SetStep(ctx context.Context, userID, stepID, requestID, status string) error {
	target, reason := stepReason(status)
	key := uuid.NewSHA1(uuid.NameSpaceURL, []byte("buddy:access:"+requestID+":"+status)).String()
	err := s.Tools.Call(ctx, "checklist", "set_step_status", map[string]any{"actor": sysactor.New(), "employee_id": userID,
		"step_id": stepID, "status": target, "reason": reason, "request_ref": requestID, "idempotency_key": key}, nil)
	if mcpclient.IsCode(err, "TRANSITION_NOT_ALLOWED") || mcpclient.IsCode(err, "STATUS_CONFLICT") {
		return nil
	}
	return err
}

// Run synchronizes the pending local copies of one user.
func (s *Syncer) Run(ctx context.Context, userID string) error {
	type local struct {
		id, resource string
		step         *string
		created      time.Time
	}
	rows, err := s.Pool.Query(ctx, `SELECT request_id, resource_id, step_id, created_at FROM app.access_requests
		WHERE user_id = $1 AND status = 'pending' ORDER BY created_at LIMIT 50`, userID)
	if err != nil {
		return err
	}
	var pending []local
	for rows.Next() {
		var l local
		if err := rows.Scan(&l.id, &l.resource, &l.step, &l.created); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	ids := make([]string, len(pending))
	byID := map[string]local{}
	for i, l := range pending {
		ids[i] = l.id
		byID[l.id] = l
	}
	var out struct {
		Items []Request `json:"items"`
	}
	if err := s.Tools.Call(ctx, "access", "list_requests", map[string]any{"actor": sysactor.New(), "employee_id": userID,
		"request_ids": ids, "limit": 50}, &out); err != nil {
		return err
	}
	var firstErr error
	for _, r := range out.Items {
		l, ok := byID[r.RequestID]
		if !ok || r.Status == "pending" {
			continue
		}
		if err := s.apply(ctx, userID, l.step, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Apply records a changed status: step first, then the copy (11 §3.6, p. 2).
func (s *Syncer) Apply(ctx context.Context, userID string, stepID *string, r Request) error {
	return s.apply(ctx, userID, stepID, r)
}

func (s *Syncer) apply(ctx context.Context, userID string, stepID *string, r Request) error {
	if stepID != nil {
		if err := s.SetStep(ctx, userID, *stepID, r.RequestID, r.Status); err != nil {
			return err
		}
	}
	var created time.Time
	err := s.Pool.QueryRow(ctx, `UPDATE app.access_requests SET status = $2, decided_by = $3, reject_reason = $4, synced_at = $5
		WHERE request_id = $1 AND status = 'pending' RETURNING created_at`,
		r.RequestID, r.Status, r.DecidedBy, r.RejectReason, s.Clock.Now()).Scan(&created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // another poll was first: the event is already published
	}
	if err != nil {
		return err
	}
	s.decided(ctx, r, created)
	return nil
}

// decided publishes access_request_decided.
func (s *Syncer) decided(ctx context.Context, r Request, created time.Time) {
	decision := "approve"
	if r.Status == "rejected" {
		decision = "reject"
	}
	by := ""
	if r.DecidedBy != nil {
		by = *r.DecidedBy
	}
	at := s.Clock.Now()
	if r.DecidedAt != nil {
		at = *r.DecidedAt
	}
	p := map[string]any{"request_id": r.RequestID, "resource_id": r.ResourceID, "decision": decision, "decided_by": by,
		"wait_ms": at.Sub(created).Milliseconds()}
	if r.RejectReason != nil {
		p["reject_reason"] = *r.RejectReason
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "access_request_decided", Payload: p})
}

// Copy is what the local copy keeps besides the request itself.
type Copy struct {
	UserID        string
	ResourceTitle string
	Approver      *string
	StepID        *string
	ActionID      string
}

// Created stores a request just created (or found as active) and moves the
// step: approved → done, pending → waiting. inserted reports the first
// insert of the copy (events are published once).
func (s *Syncer) Created(ctx context.Context, c Copy, r Request) (inserted bool, err error) {
	var action *string
	if c.ActionID != "" {
		action = &c.ActionID
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO app.access_requests (request_id, user_id, resource_id, resource_title, step_id, status, policy,
		access_level, approver, justification, auto_approve_at, decided_by, reject_reason, action_id, created_at, synced_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT (request_id) DO NOTHING`,
		r.RequestID, c.UserID, r.ResourceID, c.ResourceTitle, c.StepID, r.Status, r.Policy, r.AccessLevel, c.Approver,
		r.Justification, r.AutoApproveAt, r.DecidedBy, r.RejectReason, action, r.CreatedAt, s.Clock.Now())
	if err != nil {
		return false, err
	}
	inserted = tag.RowsAffected() > 0
	if c.StepID != nil {
		if err := s.SetStep(ctx, c.UserID, *c.StepID, r.RequestID, r.Status); err != nil {
			return inserted, err
		}
	}
	if inserted && r.Status != "pending" {
		s.decided(ctx, r, r.CreatedAt) // auto: decided at once, wait_ms 0
	}
	return inserted, nil
}
