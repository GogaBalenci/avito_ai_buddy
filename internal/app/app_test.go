package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit/pgtest"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func closedAddr(t *testing.T) string {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func testEnv(t *testing.T) map[string]string {
	_, dsn := pgtest.New(t)
	mr := miniredis.RunT(t)
	return map[string]string{"HTTP_ADDR": "127.0.0.1:0", "DATABASE_URL": dsn, "REDIS_URL": "redis://" + mr.Addr() + "/0", "LOG_LEVEL": "error"}
}

func newApp(t *testing.T) *App {
	env := testEnv(t)
	cfg, err := config.LoadAPI(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(context.Background(), cfg, discard(), testkit.NewClock(testkit.Today))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func TestNewSeedsOnceAndServes(t *testing.T) {
	a := newApp(t)
	var n int
	if err := a.Pool.QueryRow(context.Background(), `SELECT count(*) FROM app.employees`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("employees = %d, %v", n, err)
	}
	var start time.Time
	_ = a.Pool.QueryRow(context.Background(), `SELECT start_date FROM app.employees WHERE user_id = 'u-intern-be'`).Scan(&start)
	if clock.ISO(start) != "2026-11-07" {
		t.Fatalf("start = %s, want today − 3", clock.ISO(start))
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/accounts", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "u-intern-be") || strings.Contains(rec.Body.String(), "u-intern-other") {
		t.Fatalf("accounts: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("no X-Request-Id")
	}
}

func TestHealthzDegradedWhenDependenciesDown(t *testing.T) {
	a := newApp(t)
	addr := closedAddr(t)
	cfg := a.Server.Cfg
	cfg.DatabaseURL = "postgres://u:p@" + addr + "/db"
	cfg.RedisURL = "redis://" + addr + "/0"
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz with live deps = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Status          string
		Postgres, Redis bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Status != "ok" || !body.Postgres || !body.Redis {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestNewErrors(t *testing.T) {
	old := connectWait
	connectWait = 200 * time.Millisecond
	t.Cleanup(func() { connectWait = old })
	addr := closedAddr(t)
	cases := map[string]config.Config{
		"bad db url":    {DatabaseURL: "http://x", RedisURL: "redis://r"},
		"bad redis url": {DatabaseURL: "postgres://x", RedisURL: "http://r"},
		"db down":       {DatabaseURL: "postgres://u:p@" + addr + "/db", RedisURL: "redis://" + addr},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(context.Background(), cfg, discard(), testkit.NewClock(testkit.Today)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRun(t *testing.T) {
	env := testEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, func(k string) string { return env[k] }, func(a net.Addr) { ready <- a }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("not ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), func(string) string { return "" }, nil); err == nil {
		t.Fatal("expected config error")
	}
	env["LLM_PROVIDER"] = "openai_compatible"
	env["DATABASE_URL"] = "http://x"
	if err := Run(context.Background(), func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("expected start error")
	}
}
