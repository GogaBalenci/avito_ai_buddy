package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServeAndShutdown(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, "127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hi"))
		}), log, func(a net.Addr) { addrCh <- a })
	}()

	addr := <-addrCh
	resp, err := http.Get("http://" + addr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hi" {
		t.Fatalf("body = %q", b)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServeListenError(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Serve(context.Background(), "256.0.0.1:bad", http.NotFoundHandler(), log, nil); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestNotImplemented(t *testing.T) {
	rec := httptest.NewRecorder()
	NotImplemented(rec, httptest.NewRequest(http.MethodGet, "/api/path", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code = %d", rec.Code)
	}
	var e Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Code != "NOT_IMPLEMENTED" {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestLogRequests(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := LogRequests(log, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x?text=secret", nil))
	out := buf.String()
	if !strings.Contains(out, `"status":418`) || strings.Contains(out, "secret") {
		t.Fatalf("log = %s", out)
	}

	buf.Reset()
	ok := LogRequests(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ok.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("successful healthz must be logged at debug: %s", buf.String())
	}
}

func TestNewLogger(t *testing.T) {
	r, w, _ := os.Pipe()
	old := logOutput
	logOutput = w
	t.Cleanup(func() { logOutput = old })

	NewLogger(slog.LevelInfo, "api").Info("hello")
	w.Close()
	b, _ := io.ReadAll(r)
	if !strings.Contains(string(b), `"service":"api"`) {
		t.Fatalf("log = %s", b)
	}
}
