// Package server assembles mcp-mocks: four MCP endpoints, /internal/demo and
// /healthz (12 §7.1). At C0 only /healthz is implemented; the MCP endpoints
// answer 501 until the corresponding checkpoints.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/health"
	"github.com/GogaBalenci/avito_ai_buddy/internal/httpx"
)

// Run loads configuration and serves until ctx is canceled.
func Run(ctx context.Context, getenv config.Getenv, ready func(net.Addr)) error {
	cfg, err := config.LoadMocks(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := httpx.NewLogger(cfg.LogLevel, "mcp-mocks")
	h, err := NewHandler(cfg, log)
	if err != nil {
		return err
	}
	return httpx.Serve(ctx, cfg.Addr, h, log, ready)
}

// NewHandler builds the HTTP routes of mcp-mocks.
func NewHandler(cfg config.Config, log *slog.Logger) (http.Handler, error) {
	pg, err := health.Postgres(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(map[string]health.Probe{"postgres": pg}, log))
	mux.HandleFunc("/mcp/", httpx.NotImplemented)
	mux.HandleFunc("/internal/", httpx.NotImplemented)
	return httpx.LogRequests(log, mux), nil
}
