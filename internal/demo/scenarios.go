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
