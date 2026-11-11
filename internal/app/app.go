// Package app wires the api service: configuration, dependencies, HTTP routes.
// Business routes from api/openapi.yaml are added by checkpoints (12 §6);
// until then /api/* answers 501.
package app

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
	cfg, err := config.LoadAPI(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := httpx.NewLogger(cfg.LogLevel, "api")
	h, err := NewHandler(cfg, log)
	if err != nil {
		return err
	}
	return httpx.Serve(ctx, cfg.Addr, h, log, ready)
}

// NewHandler builds the HTTP routes of api.
func NewHandler(cfg config.Config, log *slog.Logger) (http.Handler, error) {
	pg, err := health.Postgres(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	rd, err := health.Redis(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(map[string]health.Probe{"postgres": pg, "redis": rd}, log))
	mux.HandleFunc("/api/", httpx.NotImplemented)
	return httpx.LogRequests(log, mux), nil
}
