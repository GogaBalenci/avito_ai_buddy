// Package migrate applies embedded SQL migrations in order, each file once.
//
// A small runner instead of a migration library: two independent chains share
// one database (app and mock_*), each keeps versions in its own schema
// (12 §8.1), and every file runs in one transaction under an advisory lock, so
// parallel starts of a service do not race.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Apply runs *.sql files from fsys that are not yet recorded in
// <versionSchema>.schema_migrations. It returns the names of applied files.
func Apply(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, versionSchema string) ([]string, error) {
	if !schemaName.MatchString(versionSchema) {
		return nil, fmt.Errorf("migrate: bad schema name %q", versionSchema)
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	table := versionSchema + ".schema_migrations"
	var applied []string
	for _, name := range names {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return applied, err
		}
		done, err := applyOne(ctx, pool, versionSchema, table, name, string(body))
		if err != nil {
			return applied, fmt.Errorf("migrate %s: %w", name, err)
		}
		if done {
			applied = append(applied, name)
		}
	}
	return applied, nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, schema, table, name, body string) (bool, error) {
	var applied bool
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('migrate:' || $1))`, schema); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+table+
			" (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE version = $1)", name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		if strings.TrimSpace(body) != "" {
			if _, err := tx.Exec(ctx, body); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "INSERT INTO "+table+" (version) VALUES ($1)", name); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}
