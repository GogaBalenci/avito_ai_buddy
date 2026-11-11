// Package httpx holds HTTP plumbing shared by api and mcp-mocks:
// server lifecycle with graceful shutdown, request logging and JSON errors.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ShutdownTimeout is how long in-flight requests may take after SIGTERM.
const ShutdownTimeout = 10 * time.Second

// Serve listens on addr and serves h until ctx is canceled, then shuts down
// gracefully. ready (optional) receives the bound address once listening.
func Serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger, ready func(net.Addr)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	log.Info("http server started", "addr", ln.Addr().String())
	if ready != nil {
		ready(ln.Addr())
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("http server stopped")
	return nil
}

// Error is the common error body (OpenAPI schema Error).
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

// WriteJSON writes v with the given status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// NotImplemented answers 501 for endpoints scheduled for later checkpoints (12 §6).
func NotImplemented(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusNotImplemented, Error{Code: "NOT_IMPLEMENTED", Message: "Эндпоинт ещё не реализован"})
}

// LogRequests logs method, path, status and duration. Bodies and query
// strings are never logged (they may contain user text).
func LogRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		lvl := slog.LevelInfo
		if r.URL.Path == "/healthz" && sw.code < 500 {
			lvl = slog.LevelDebug // healthchecks every 5 s would flood the log
		}
		log.Log(r.Context(), lvl, "http request",
			"method", r.Method, "path", r.URL.Path, "status", sw.code,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// NewLogger returns a JSON slog logger (12 §13).
func NewLogger(level slog.Level, service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: level})).With("service", service)
}
