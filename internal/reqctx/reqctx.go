// Package reqctx carries request-scoped values: request_id (12 §13, passes
// through to MCP _meta, logs and events), the session actor and session id.
package reqctx

import (
	"context"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
)

type key int

const (
	keyRequestID key = iota
	keyActor
	keySession
)

// WithRequestID stores the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// RequestID returns the request id or "".
func RequestID(ctx context.Context) string { s, _ := ctx.Value(keyRequestID).(string); return s }

// WithActor stores the session actor.
func WithActor(ctx context.Context, a actor.Actor) context.Context {
	return context.WithValue(ctx, keyActor, a)
}

// Actor returns the session actor.
func Actor(ctx context.Context) (actor.Actor, bool) {
	a, ok := ctx.Value(keyActor).(actor.Actor)
	return a, ok
}

// WithSessionID stores the session id (for events).
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keySession, id)
}

// SessionID returns the session id or "".
func SessionID(ctx context.Context) string { s, _ := ctx.Value(keySession).(string); return s }
