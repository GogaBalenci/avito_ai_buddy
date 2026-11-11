package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func closedAddr(t *testing.T) string {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestHealthzDegradedWhenDependenciesDown(t *testing.T) {
	addr := closedAddr(t)
	h, err := NewHandler(config.Config{
		DatabaseURL: "postgres://u:p@" + addr + "/db",
		RedisURL:    "redis://" + addr + "/0",
	}, discard())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d", rec.Code)
	}
	var body struct {
		Status          string
		Postgres, Redis bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Status != "degraded" || body.Postgres || body.Redis {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestAPIRoutesNotImplementedYet(t *testing.T) {
	h, _ := NewHandler(config.Config{DatabaseURL: "postgres://db", RedisURL: "redis://cache"}, discard())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/path", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestNewHandlerBadURLs(t *testing.T) {
	if _, err := NewHandler(config.Config{DatabaseURL: "http://x", RedisURL: "redis://r"}, discard()); err == nil {
		t.Fatal("expected DATABASE_URL error")
	}
	if _, err := NewHandler(config.Config{DatabaseURL: "postgres://x", RedisURL: "http://r"}, discard()); err == nil {
		t.Fatal("expected REDIS_URL error")
	}
}

func TestRun(t *testing.T) {
	env := map[string]string{
		"HTTP_ADDR":    "127.0.0.1:0",
		"DATABASE_URL": "postgres://db",
		"REDIS_URL":    "redis://cache",
		"LOG_LEVEL":    "error",
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, func(k string) string { return env[k] }, func(a net.Addr) { ready <- a }) }()

	select {
	case <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunErrors(t *testing.T) {
	if err := Run(context.Background(), func(string) string { return "" }, nil); err == nil {
		t.Fatal("expected config error")
	}
	env := map[string]string{"DATABASE_URL": "http://x", "REDIS_URL": "redis://r"}
	if err := Run(context.Background(), func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("expected handler error")
	}
}
