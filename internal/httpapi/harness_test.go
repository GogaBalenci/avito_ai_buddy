package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/auth"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/demo"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	"github.com/GogaBalenci/avito_ai_buddy/internal/health"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
	"github.com/GogaBalenci/avito_ai_buddy/internal/store"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit/pgtest"
)

// harness is the API with a real app schema, miniredis, a fake clock and a
// fake ToolClient (or the real mock services in integration tests).
type harness struct {
	t      *testing.T
	srv    *Server
	h      http.Handler
	clock  *testkit.Clock
	tools  *mcpclient.Fake
	events *events.Recorder
}

func newHarness(t *testing.T) *harness { return build(t, false) }

// newIntegration runs the API against the real mcp-mocks (12 §11.1, layer
// «integration with mocks»): its own database, the same clock.
func newIntegration(t *testing.T) *harness { return build(t, true) }

func build(t *testing.T, withMocks bool) *harness {
	t.Helper()
	pool, _ := pgtest.New(t)
	rc, _ := testkit.Redis(t)
	clk := testkit.NewClock(testkit.Today)
	cfg, err := config.LoadAPI(func(k string) string {
		return map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://x"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	seeds, _ := fs.Sub(content.FS(), "seeds")
	people, err := employees.Load(seeds, nil)
	if err != nil {
		t.Fatal(err)
	}
	people = append(people, // fixture: a foreign intern of another mentor (13 §2)
		employees.Employee{UserID: "u-mentor-other", DisplayName: "Другой наставник", Role: "mentor"},
		employees.Employee{UserID: "u-intern-other", DisplayName: "Чужой стажёр", Role: "intern", Track: "backend", MentorID: "u-mentor-other", StartDaysAgoDefault: 5})
	seeder := &demo.Seeder{Pool: pool, Clock: clk, Employees: people}
	if _, err := seeder.SeedIfEmpty(context.Background()); err != nil {
		t.Fatal(err)
	}
	emp := &store.Employees{Pool: pool}
	rec := &events.Recorder{}
	fake := mcpclient.NewFake()
	var tools mcpclient.Client = fake
	if withMocks {
		env := mockstest.Start(t, clk)
		tools = &mcpclient.MCP{URLs: env.URLs(), Timeout: 5 * time.Second}
	}
	g := &guard.Guard{Employees: emp}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{Deps: Deps{
		Cfg: cfg, Log: log, Pool: pool, Redis: rc, Clock: clk,
		Sessions:     &auth.Sessions{Redis: rc, Employees: emp, Clock: clk, TTL: time.Hour},
		Limiter:      &auth.Limiter{Redis: rc, Clock: clk, Max: 20},
		Employees:    emp,
		Accounts:     emp,
		AccountNotes: map[string]string{"u-intern-be": "Backend", "u-intern-fe": "Frontend", "u-mentor": "Наставник"},
		Events:       rec,
		Flags:        flags.Static{"llm_enabled": "on", "llm_rephrase": "on"},
		Tools:        tools,
		Guard:        g,
		Path:         &path.Service{Tools: tools, Employees: emp, Clock: clk, Guard: g, Pool: pool},
		Health:       health.Handler(map[string]health.Probe{"postgres": health.ProbeFunc(func(context.Context) error { return nil })}, log),
	}}
	h, err := Handler(s)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, srv: s, h: h, clock: clk, tools: fake, events: rec}
}

// client is a browser session against the harness.
type client struct {
	h      *harness
	cookie string
}

func (h *harness) anon() *client { return &client{h: h} }

func (h *harness) login(userID string) *client {
	c := &client{h: h}
	rec := c.do("POST", "/api/auth/login", map[string]any{"user_id": userID})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login %s: %d %s", userID, rec.Code, rec.Body)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CookieName {
			c.cookie = ck.Value
		}
	}
	return c
}

func (c *client) do(method, path string, body any) *httptest.ResponseRecorder {
	return c.doWith(method, path, body, map[string]string{"Origin": "http://localhost:8380"})
}

func (c *client) doWith(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = "localhost:8380"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if c.cookie != "" {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: c.cookie})
	}
	rec := httptest.NewRecorder()
	c.h.h.ServeHTTP(rec, r)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return v
}

func newID() string { return uuid.NewString() }

func expect(t *testing.T, rec *httptest.ResponseRecorder, code int, contains ...string) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status %d, want %d: %s", rec.Code, code, rec.Body)
	}
	for _, s := range contains {
		if !strings.Contains(rec.Body.String(), s) {
			t.Fatalf("body has no %q: %s", s, rec.Body)
		}
	}
}
