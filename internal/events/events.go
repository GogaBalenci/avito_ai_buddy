// Package events is EventPublisher (12 §5.1): product events go to the table
// app.events (in production — Kafka). Events never contain message text,
// dates of absence or personal data (AG-20).
package events

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// Event is one product event. Common fields not set explicitly are taken from
// the request context (request_id, actor, session).
type Event struct {
	Name          string
	RequestID     string
	UserID        string
	Role          string
	SessionID     string
	Mode          string
	PromptVersion string
	Payload       map[string]any
}

// Publisher publishes events.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// pgxExecer is satisfied by *pgxpool.Pool and pgx.Tx, so an event can be
// written in the same transaction as the data it describes.
type pgxExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PG writes events to app.events.
type PG struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// Publish inserts the event.
func (p *PG) Publish(ctx context.Context, e Event) error {
	return insert(ctx, p.Pool, p.Clock, e)
}

// PublishTx inserts the event inside tx.
func PublishTx(ctx context.Context, tx pgx.Tx, c clock.Clock, e Event) error {
	return insert(ctx, tx, c, e)
}

// Fill sets common fields from the request context.
func Fill(ctx context.Context, e Event) Event {
	if e.RequestID == "" {
		e.RequestID = reqctx.RequestID(ctx)
	}
	if a, ok := reqctx.Actor(ctx); ok {
		if e.UserID == "" {
			e.UserID = a.UserID
		}
		if e.Role == "" {
			e.Role = a.Role
		}
	}
	if e.SessionID == "" {
		e.SessionID = reqctx.SessionID(ctx)
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return e
}

func insert(ctx context.Context, db pgxExecer, c clock.Clock, e Event) error {
	e = Fill(ctx, e)
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO app.events
		(event_id, ts, name, request_id, user_id, role, session_id, mode, prompt_version, payload)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10)
		ON CONFLICT DO NOTHING`,
		uuid.New(), c.Now(), e.Name, e.RequestID, e.UserID, e.Role, e.SessionID, e.Mode, e.PromptVersion, payload)
	return err
}

// Recorder is an in-memory Publisher for tests.
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

// Publish records the event.
func (r *Recorder) Publish(ctx context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Fill(ctx, e))
	return nil
}

// Events returns recorded events (optionally only with the given name).
func (r *Recorder) Events(name string) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.events {
		if name == "" || e.Name == name {
			out = append(out, e)
		}
	}
	return out
}
