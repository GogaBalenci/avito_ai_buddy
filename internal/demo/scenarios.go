package demo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GogaBalenci/avito_ai_buddy/internal/timeoff/rules"
)

// SeedTimeOff writes the local copy of the demo request ABS-000101 (10 §12.4);
// the mock holds the original with the same relative dates.
func SeedTimeOff(ctx context.Context, tx pgx.Tx, today time.Time) error {
	from, to := rules.SeedDates(today)
	created := today.Add(-12 * time.Hour)
	_, err := tx.Exec(ctx, `INSERT INTO app.time_off_requests (external_id, user_id, date_from, date_to, working_days, status, created_at, synced_at)
		VALUES ('ABS-000101', 'u-intern-be', $1, $2, 2, 'registered', $3, $3) ON CONFLICT (external_id) DO NOTHING`, from, to, created)
	return err
}

// SeedAccess writes the local copy of ACC-000101 with its step cm-1.10
// (11 §3.11): the Backend intern waits for the mentor since yesterday noon.
func SeedAccess(ctx context.Context, tx pgx.Tx, today time.Time) error {
	created := today.Add(-24*time.Hour + 9*time.Hour) // yesterday 12:00 MSK
	_, err := tx.Exec(ctx, `INSERT INTO app.access_requests (request_id, user_id, resource_id, resource_title, step_id, status, policy,
		access_level, approver, justification, created_at, synced_at)
		VALUES ('ACC-000101', 'u-intern-be', 'space-cluster', 'Пространство кластера и команды в базе знаний', 'cm-1.10', 'pending', 'mentor',
		'read', 'Владелец пространства', 'Шаг 1.10 чек-листа адаптации: Запроси доступ в пространство своего кластера и команды и изучи его', $1, $1)
		ON CONFLICT (request_id) DO NOTHING`, created)
	return err
}
