// Package server assembles mcp-mocks: four MCP endpoints (Streamable HTTP,
// stateless), /internal/demo and /healthz (12 §7.1). At start: migrations of
// mock_* → content (tracks, knowledge base, catalog; an error stops the
// service, P-A15) → seeds if absent → serve.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/health"
	"github.com/GogaBalenci/avito_ai_buddy/internal/httpx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/kb"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/absences"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/access"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/admin"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/catalog"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/checklist"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/knowledge"
	"github.com/GogaBalenci/avito_ai_buddy/internal/migrate"
	"github.com/GogaBalenci/avito_ai_buddy/internal/tracks"
	"github.com/GogaBalenci/avito_ai_buddy/migrations"
)

// Run loads configuration and serves until ctx is canceled.
func Run(ctx context.Context, getenv config.Getenv, ready func(net.Addr)) error {
	cfg, err := config.LoadMocks(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := httpx.NewLogger(cfg.LogLevel, "mcp-mocks")
	m, err := New(ctx, cfg, log, clock.Real{Loc: cfg.Location}, Options{})
	if err != nil {
		return err
	}
	defer m.Pool.Close()
	return httpx.Serve(ctx, cfg.Addr, m.Handler, log, ready)
}

// Options adjust the mocks for tests.
type Options struct {
	ExtraEmployees []employees.Employee // fixtures such as u-intern-other
}

// Content is the loaded and checked content of the corporate services.
type Content struct {
	Tracks    *tracks.Set
	Docs      []kb.Doc
	Resources []catalog.Resource
	Employees []employees.Employee
}

// LoadContent loads and cross-checks all content (fail fast).
func LoadContent(fsys fs.FS) (*Content, error) {
	sub := func(d string) fs.FS { s, _ := fs.Sub(fsys, d); return s }
	docs, err := kb.Load(sub("kb"))
	if err != nil {
		return nil, err
	}
	res, err := catalog.Load(sub("access"))
	if err != nil {
		return nil, err
	}
	set, err := tracks.Load(sub("tracks"), tracks.Refs{KBDocs: kb.Slugs(docs), Resources: catalog.IDs(res)})
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, id := range set.IDs() {
		known[id] = true
	}
	people, err := employees.Load(sub("seeds"), known)
	if err != nil {
		return nil, err
	}
	return &Content{Tracks: set, Docs: docs, Resources: res, Employees: people}, nil
}

// Mocks is the assembled mcp-mocks service.
type Mocks struct {
	Handler   http.Handler
	Pool      *pgxpool.Pool
	Clock     clock.Clock
	Content   *Content
	Flags     *admin.Flags
	Checklist *checklist.Service
	Absences  *absences.Service
	Access    *access.Service
	Knowledge *knowledge.Service
	cfg       config.Config
}

// New connects, migrates, loads content, seeds and builds the handler.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, clk clock.Clock, opts Options) (*Mocks, error) {
	if err := contracts.Check(); err != nil {
		return nil, err
	}
	c, err := LoadContent(content.FS())
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	pg, err := health.Postgres(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if _, err := migrate.Apply(ctx, pool, migrations.Mocks(), "mock_admin"); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	people := employees.NewStatic(append(append([]employees.Employee{}, c.Employees...), opts.ExtraEmployees...))
	m := &Mocks{Pool: pool, Clock: clk, Content: c, Flags: &admin.Flags{Pool: pool}, cfg: cfg,
		Checklist: &checklist.Service{Pool: pool, Clock: clk, Employees: people}}
	m.Absences = &absences.Service{Pool: pool, Clock: clk, Employees: people, Flags: m.Flags}
	m.Knowledge = &knowledge.Service{Pool: pool}
	m.Access = &access.Service{Pool: pool, Clock: clk, Employees: people, Flags: m.Flags, TimerMinutes: cfg.AccessTimerMinutes}
	if err := m.init(ctx, false); err != nil {
		pool.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(map[string]health.Probe{"postgres": pg}, log))
	endpoint := func(name string, register func(*mcp.Server)) {
		srv := mcp.NewServer(&mcp.Implementation{Name: "mock-" + name, Version: "1.0.0"}, nil)
		register(srv)
		mux.Handle("/mcp/"+name, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
			&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	}
	endpoint("checklist", func(s *mcp.Server) { m.Checklist.Register(s, log) })
	endpoint("knowledge", func(s *mcp.Server) { m.Knowledge.Register(s, log) })
	endpoint("absences", func(s *mcp.Server) { m.Absences.Register(s, log) })
	endpoint("access", func(s *mcp.Server) { m.Access.Register(s, log) })
	if cfg.DemoToolsEnabled {
		mux.Handle("/internal/demo/", admin.Handler(m.Flags, func(ctx context.Context) error { return m.init(ctx, true) }))
	}
	m.Handler = httpx.LogRequests(log, mux)
	return m, nil
}

// init writes content and applies seeds: at start only when there are no
// seeds yet; on reset after truncating all mock data (12 §8.5, step 1).
func (m *Mocks) init(ctx context.Context, reset bool) error {
	return pgx.BeginFunc(ctx, m.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('mocks-seed'))`); err != nil {
			return err
		}
		if err := checklist.LoadContent(ctx, tx, m.Content.Tracks); err != nil {
			return fmt.Errorf("checklist content: %w", err)
		}
		if err := access.LoadContent(ctx, tx, m.Content.Resources); err != nil {
			return fmt.Errorf("access content: %w", err)
		}
		if err := knowledge.LoadContent(ctx, tx, m.Content.Docs, m.Clock.Now()); err != nil {
			return fmt.Errorf("knowledge content: %w", err)
		}
		if reset {
			if _, err := tx.Exec(ctx, `TRUNCATE mock_checklist.progress, mock_checklist.idempotency`); err != nil {
				return err
			}
			if err := absences.Truncate(ctx, tx); err != nil {
				return err
			}
			if err := access.Truncate(ctx, tx); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM mock_admin.meta WHERE key = 'seeded_at'`); err != nil {
				return err
			}
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM mock_admin.meta WHERE key = 'seeded_at'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		now := m.Clock.Now()
		if err := admin.ResetTx(ctx, tx); err != nil {
			return err
		}
		if err := checklist.Seed(ctx, tx, now, SeedProgress(m.Content)); err != nil {
			return err
		}
		if err := absences.Seed(ctx, tx, now); err != nil {
			return err
		}
		if err := access.Seed(ctx, tx, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO mock_admin.meta (key, value) VALUES ('seeded_at', $1)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, now.Format(time.RFC3339))
		return err
	})
}

// SeedProgress returns the seeded statuses (11 §1.13).
func SeedProgress(c *Content) []checklist.SeedProgress {
	rows := []checklist.SeedProgress{
		{EmployeeID: "u-intern-be", StepID: "cm-1.6", Status: "done"},
		{EmployeeID: "u-intern-be", StepID: "cm-1.9", Status: "done"},
		{EmployeeID: "u-intern-be", StepID: "cm-1.10", Status: "waiting", RequestRef: "ACC-000101"},
		{EmployeeID: "u-intern-be", StepID: "be-1.11", Status: "in_progress"},
	}
	if fe, err := c.Tracks.Path("frontend"); err == nil {
		for _, s := range fe {
			if s.Period == "week1" && s.Owner == "intern" && s.ID != "fe-w1-50" {
				rows = append(rows, checklist.SeedProgress{EmployeeID: "u-intern-fe", StepID: s.ID, Status: "done"})
			}
		}
	}
	return rows
}

var connectWait = 30 * time.Second

func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
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
