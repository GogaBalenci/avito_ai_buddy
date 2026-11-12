// Package app wires the api service: configuration, storage, migrations,
// seeds and the HTTP API (12 §3.4). cmd/api only calls Run.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/llm"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/access"
	accesssync "github.com/GogaBalenci/avito_ai_buddy/internal/access/sync"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/router"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	"github.com/GogaBalenci/avito_ai_buddy/internal/auth"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/demo"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	"github.com/GogaBalenci/avito_ai_buddy/internal/health"
	"github.com/GogaBalenci/avito_ai_buddy/internal/httpapi"
	"github.com/GogaBalenci/avito_ai_buddy/internal/httpx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/migrate"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
	"github.com/GogaBalenci/avito_ai_buddy/internal/store"
	"github.com/GogaBalenci/avito_ai_buddy/migrations"
)

// Run loads configuration and serves until ctx is canceled.
func Run(ctx context.Context, getenv config.Getenv, ready func(net.Addr)) error {
	cfg, err := config.LoadAPI(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := httpx.NewLogger(cfg.LogLevel, "api")
	if cfg.LLM.Misconfigured != "" {
		log.Warn("LLM is not configured, working in demo mode", "missing", cfg.LLM.Misconfigured)
	}
	a, err := New(ctx, cfg, log, clock.Real{Loc: cfg.Location})
	if err != nil {
		return err
	}
	defer a.Close()
	return httpx.Serve(ctx, cfg.Addr, a.Handler, log, ready)
}

// App is the assembled api.
type App struct {
	Handler http.Handler
	Server  *httpapi.Server
	Pool    *pgxpool.Pool
	Redis   *redis.Client
}

// Close releases connections.
func (a *App) Close() {
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}

// New connects to storage, applies migrations and seeds and builds the API.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, clk clock.Clock) (*App, error) {
	if err := contracts.Check(); err != nil {
		return nil, fmt.Errorf("contracts: %w", err)
	}
	pgProbe, err := health.Postgres(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	rdProbe, err := health.Redis(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL: %w", err)
	}
	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a := &App{Pool: pool, Redis: redis.NewClient(ropts)}
	if _, err := migrate.Apply(ctx, pool, migrations.App(), "app"); err != nil {
		a.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	seeds, _ := fs.Sub(content.FS(), "seeds")
	people, err := employees.Load(seeds, nil)
	if err != nil {
		a.Close()
		return nil, err
	}
	seeder := &demo.Seeder{Pool: pool, Clock: clk, Employees: people, Getenv: cfg.Getenv,
		Extra: []func(context.Context, pgx.Tx, time.Time) error{demo.SeedTimeOff, demo.SeedAccess}}
	if seeded, err := seeder.SeedIfEmpty(ctx); err != nil {
		a.Close()
		return nil, fmt.Errorf("seeds: %w", err)
	} else if seeded {
		log.Info("demo seeds applied")
	}
	notes := map[string]string{}
	for _, p := range people {
		notes[p.UserID] = p.Description
	}
	emp := &store.Employees{Pool: pool}
	tools := &mcpclient.MCP{URLs: cfg.MCPURLs, Timeout: cfg.MCPTimeout, Log: log}
	g := &guard.Guard{Employees: emp}
	rec := &events.PG{Pool: pool, Clock: clk}
	catalog := &access.Catalog{Tools: tools}
	syncer := &accesssync.Syncer{Tools: tools, Pool: pool, Events: rec, Clock: clk}
	paths := &path.Service{Tools: tools, Employees: emp, Clock: clk, Guard: g, Pool: pool}
	srv := &httpapi.Server{Deps: httpapi.Deps{
		Cfg: cfg, Log: log, Pool: pool, Redis: a.Redis, Clock: clk,
		Sessions:     &auth.Sessions{Redis: a.Redis, Employees: emp, Clock: clk, TTL: cfg.SessionTTL, Secure: cfg.CookieSecure},
		Limiter:      &auth.Limiter{Redis: a.Redis, Clock: clk, Max: 20},
		Employees:    emp,
		Accounts:     emp,
		AccountNotes: notes,
		Events:       rec,
		Flags:        &flags.PG{Pool: pool},
		Tools:        tools,
		Guard:        g,
		Path:         paths,
		Actions: &actions.Service{Store: &actions.Store{Redis: a.Redis, Clock: clk}, Tools: tools, Guard: g, Pool: pool,
			Events: rec, Clock: clk, Catalog: catalog, Path: paths, Sync: syncer},
		Sync:       syncer.Run,
		AccessSync: syncer,
		Catalog:    catalog,
		Seeder:     seeder,
		AdminHTTP:  &http.Client{Timeout: 10 * time.Second},
		Health:     health.Handler(map[string]health.Probe{"postgres": pgProbe, "redis": rdProbe}, log),
	}}
	h, err := httpapi.Handler(srv)
	if err != nil {
		a.Close()
		return nil, err
	}
	srv.Agent = &router.Agent{Rules: rules.MustLoad(), Path: srv.Path, Actions: srv.Actions, Guard: g, Events: rec,
		Docs: router.MCPDocs{Tools: tools}, Redis: a.Redis, Clock: clk, Requests: srv.RequestsOf,
		Catalog: catalog, Inbox: srv.PendingInbox, Tools: tools, RHigh: cfg.KBRHigh, RLow: cfg.KBRLow,
		LLM: llm.New(cfg.LLM, srv.Flags, clk)}
	a.Handler, a.Server = h, srv
	return a, nil
}

// connectWait is how long New waits for PostgreSQL (tests shorten it).
var connectWait = 30 * time.Second

// connect waits up to 30 s for PostgreSQL (compose starts api after the
// healthcheck, but a restart of postgres must not crash-loop the api).
func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(connectWait)
	for {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = pool.Ping(pctx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, errors.Join(errors.New("postgres is not reachable"), err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
