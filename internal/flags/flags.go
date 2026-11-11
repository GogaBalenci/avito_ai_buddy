// Package flags is FlagProvider (12 §5.1): runtime feature flags of the
// application in app.feature_flags, read on every request without restart.
// Mock flags (mock_absences_fault, access_mentor_approval) belong to
// mcp-mocks and are not here (12 §1, decision 6).
package flags

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Application flags (13 §4: the demo panel cannot change them).
const (
	LLMEnabled  = "llm_enabled"
	LLMRephrase = "llm_rephrase"
)

// Provider reads flags.
type Provider interface {
	Get(ctx context.Context, name string) (string, error)
}

// ErrUnknown is returned for a flag that does not exist.
var ErrUnknown = errors.New("unknown flag")

// PG reads app.feature_flags.
type PG struct{ Pool *pgxpool.Pool }

// Get returns the flag value.
func (p *PG) Get(ctx context.Context, name string) (string, error) {
	var v string
	err := p.Pool.QueryRow(ctx, `SELECT value FROM app.feature_flags WHERE name = $1`, name).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnknown
	}
	return v, err
}

// On reports whether a flag is "on"; an unreadable flag is treated as off
// (fail safe: the LLM switch defaults to demo).
func On(ctx context.Context, p Provider, name string) bool {
	v, err := p.Get(ctx, name)
	return err == nil && v == "on"
}

// Static is an in-memory Provider for tests.
type Static map[string]string

// Get returns the flag value.
func (s Static) Get(_ context.Context, name string) (string, error) {
	v, ok := s[name]
	if !ok {
		return "", ErrUnknown
	}
	return v, nil
}
