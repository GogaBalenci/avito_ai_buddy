// Package mockstest starts the real mcp-mocks on a per-test database for
// tests of the mocks and integration tests of the api.
package mockstest

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/server"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit/pgtest"
)

// Env is a running mock stack.
type Env struct {
	Mocks *server.Mocks
	URL   string // base URL of the httptest server
	Pool  *pgxpool.Pool
	DSN   string
}

// Fixtures are the employees absent from demo seeds (13 §2).
var Fixtures = []employees.Employee{
	{UserID: "u-mentor-other", DisplayName: "Другой наставник (фикстура)", Role: "mentor"},
	{UserID: "u-intern-other", DisplayName: "Чужой стажёр (фикстура)", Role: "intern", Track: "backend", MentorID: "u-mentor-other"},
}

// Start runs mcp-mocks with the clock and returns its URLs.
func Start(t testing.TB, clk clock.Clock) *Env {
	t.Helper()
	pool, dsn := pgtest.New(t)
	cfg, err := config.LoadMocks(func(k string) string {
		return map[string]string{"DATABASE_URL": dsn, "ACCESS_TIMER_MINUTES": "2"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := server.New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), clk, server.Options{ExtraEmployees: Fixtures})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(m.Handler)
	t.Cleanup(func() { ts.Close(); m.Pool.Close() })
	return &Env{Mocks: m, URL: ts.URL, Pool: pool, DSN: dsn}
}

// URLs returns MCP_*_URL for the api.
func (e *Env) URLs() map[string]string {
	return map[string]string{
		"checklist": e.URL + "/mcp/checklist", "knowledge": e.URL + "/mcp/knowledge",
		"absences": e.URL + "/mcp/absences", "access": e.URL + "/mcp/access",
	}
}
