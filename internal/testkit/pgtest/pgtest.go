// Package pgtest gives each test its own PostgreSQL database cloned from a
// template with all migrations applied (12 §11.1): packages run in parallel
// and never see each other's rows. The server is the compose `postgres`
// (DATABASE_URL); without it database tests are skipped.
package pgtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/migrate"
	"github.com/GogaBalenci/avito_ai_buddy/migrations"
)

// URL returns the admin connection string or "" when tests have no database.
func URL() string { return os.Getenv("DATABASE_URL") }

// New creates a fresh database with app and mock_* migrations and returns a
// pool plus its connection string. The database is dropped after the test.
func New(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	admin := URL()
	if admin == "" {
		t.Skip("DATABASE_URL is not set: database tests run in `docker compose --profile test run --rm test-go`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmpl, err := ensureTemplate(ctx, admin)
	if err != nil {
		t.Fatalf("pgtest: template: %v", err)
	}
	name := "t_" + randHex(8)
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, name, tmpl)); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	dsn := withDB(admin, name)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name))
	})
	return pool, dsn
}

// ensureTemplate creates (once per migrations content) a template database.
func ensureTemplate(ctx context.Context, admin string) (string, error) {
	tmpl := "tmpl_" + migrationsHash()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	// Serialize template creation between test processes (packages run in parallel).
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('pgtest-template'))`); err != nil {
		return "", err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('pgtest-template'))`) }()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, tmpl).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return tmpl, nil
	}
	build := tmpl + "_build"
	_, _ = conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, build))
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, build)); err != nil {
		return "", err
	}
	pool, err := pgxpool.New(ctx, withDB(admin, build))
	if err != nil {
		return "", err
	}
	_, errApp := migrate.Apply(ctx, pool, migrations.App(), "app")
	_, errMocks := migrate.Apply(ctx, pool, migrations.Mocks(), "mock_admin")
	pool.Close()
	if errApp != nil || errMocks != nil {
		return "", fmt.Errorf("migrations: app=%v mocks=%v", errApp, errMocks)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s RENAME TO %s`, build, tmpl)); err != nil {
		return "", err
	}
	return tmpl, nil
}

func migrationsHash() string {
	h := sha256.New()
	for _, f := range []fs.FS{migrations.App(), migrations.Mocks()} {
		names, _ := fs.Glob(f, "*.sql")
		sort.Strings(names)
		for _, n := range names {
			b, _ := fs.ReadFile(f, n)
			h.Write([]byte(n))
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func withDB(dsn, db string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + db
	return u.String()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.ToLower(hex.EncodeToString(b))
}
