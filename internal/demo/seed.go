// Package demo applies the seeds of the app schema with dates relative to
// today (12 §8.4) and resets the demo (12 §8.5).
package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
)

// Seeder applies app seeds.
type Seeder struct {
	Pool      *pgxpool.Pool
	Clock     clock.Clock
	Employees []employees.Employee
	Getenv    func(string) string
	// Extra seeds of scenarios (S3, S4 copies of requests) run in the same transaction.
	Extra []func(ctx context.Context, tx pgx.Tx, today time.Time) error
}

// SeedIfEmpty applies seeds once, on the first start (12 §3.4): a second
// `docker compose up` must not erase what the expert has done.
func (s *Seeder) SeedIfEmpty(ctx context.Context) (bool, error) {
	var seeded bool
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('app-seed'))`); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.meta WHERE key = 'seeded_at'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		seeded = true
		return s.apply(ctx, tx)
	})
	return seeded, err
}

// Reset truncates demo data of the app schema and applies seeds again
// (12 §8.5, step 2). Events and sessions are kept.
func (s *Seeder) Reset(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('app-seed'))`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `TRUNCATE app.chat_messages, app.agent_replies, app.feedback, app.action_log,
			app.time_off_requests, app.access_requests, app.path_period_stats`); err != nil {
			return err
		}
		return s.apply(ctx, tx)
	})
}

func (s *Seeder) apply(ctx context.Context, tx pgx.Tx) error {
	today := clock.Today(s.Clock)
	// mentors first: interns reference them
	for _, pass := range []string{employees.RoleMentor, employees.RoleIntern} {
		for _, e := range s.Employees {
			if e.Role != pass {
				continue
			}
			var start *time.Time
			if e.Role == employees.RoleIntern {
				d := today.AddDate(0, 0, -e.StartDaysAgo(s.Getenv))
				start = &d
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.employees (user_id, display_name, role, track_id, start_date, mentor_id)
				VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''))
				ON CONFLICT (user_id) DO UPDATE SET display_name = EXCLUDED.display_name, role = EXCLUDED.role,
				  track_id = EXCLUDED.track_id, start_date = EXCLUDED.start_date, mentor_id = EXCLUDED.mentor_id`,
				e.UserID, e.DisplayName, e.Role, e.Track, start, e.MentorID); err != nil {
				return fmt.Errorf("seed employee %s: %w", e.UserID, err)
			}
		}
	}
	now := s.Clock.Now()
	for _, f := range [][3]string{
		{"llm_enabled", "on", "Аварийный выключатель LLM: off — работа как в demo без перезапуска"},
		{"llm_rephrase", "on", "Переформулировка ответов моделью (шаг 2)"},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO app.feature_flags (name, value, allowed_values, description, updated_at)
			VALUES ($1, $2, ARRAY['on','off'], $3, $4) ON CONFLICT (name) DO NOTHING`, f[0], f[1], f[2], now); err != nil {
			return err
		}
	}
	for _, x := range s.Extra {
		if err := x(ctx, tx, today); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO app.meta (key, value) VALUES ('seeded_at', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, now.Format(time.RFC3339))
	return err
}
