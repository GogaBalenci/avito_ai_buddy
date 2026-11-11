package server

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
)

// fakePostgres answers SSLRequest with 'N', like a server without TLS.
func fakePostgres(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 8)
			if _, err := io.ReadFull(c, buf); err == nil && binary.BigEndian.Uint32(buf[4:]) == 80877103 {
				_, _ = c.Write([]byte{'N'})
			}
			c.Close()
		}
	}()
	return ln.Addr().String()
}

func TestHandler(t *testing.T) {
	h, err := NewHandler(config.Config{DatabaseURL: "postgres://" + fakePostgres(t) + "/db"},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/healthz":             http.StatusOK,
		"/mcp/absences":        http.StatusNotImplemented,
		"/internal/demo/reset": http.StatusNotImplemented,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s: code = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestNewHandlerBadURL(t *testing.T) {
	if _, err := NewHandler(config.Config{DatabaseURL: "http://x"}, slog.Default()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRun(t *testing.T) {
	env := map[string]string{"HTTP_ADDR": "127.0.0.1:0", "DATABASE_URL": "postgres://db", "LOG_LEVEL": "error"}
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
	if err := Run(context.Background(), func(string) string { return "" }, nil); err == nil {
		t.Fatal("expected config error")
	}
	bad := map[string]string{"DATABASE_URL": "http://x"}
	if err := Run(context.Background(), func(k string) string { return bad[k] }, nil); err == nil {
		t.Fatal("expected handler error")
	}
}
