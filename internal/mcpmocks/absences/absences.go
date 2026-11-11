// Package absences is the mock of the corporate absence service, endpoint
// /mcp/absences (10 §11): create_request (idempotent by action_id, advisory
// lock against two drafts racing) and list_requests.
package absences

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/admin"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mcpx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/timeoff/rules"
)

// SlowDelay is the delay of the "slow" fault: longer than MCP_TIMEOUT_MS.
var SlowDelay = 4 * time.Second

// Service is the absences mock.
type Service struct {
	Pool      *pgxpool.Pool
	Clock     clock.Clock
	Employees employees.Provider
	Flags     *admin.Flags
}

// Request is request.v1.json.
type Request struct {
	RequestID   string    `json:"request_id"`
	EmployeeID  string    `json:"employee_id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	DateFrom    string    `json:"date_from"`
	DateTo      string    `json:"date_to"`
	WorkingDays int       `json:"working_days"`
	CreatedAt   time.Time `json:"created_at"`
}

const cols = `request_id, employee_id, type, status, date_from, date_to, working_days, created_at`

func scan(row pgx.Row) (Request, error) {
	var r Request
	var from, to time.Time
	if err := row.Scan(&r.RequestID, &r.EmployeeID, &r.Type, &r.Status, &from, &to, &r.WorkingDays, &r.CreatedAt); err != nil {
		return r, err
	}
	r.DateFrom, r.DateTo = from.Format("2006-01-02"), to.Format("2006-01-02")
	return r, nil
}

// Register adds the tools.
func (s *Service) Register(srv *mcp.Server, log *slog.Logger) {
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "absences", Name: "create_request", Idempotent: true,
		Description: "Создать заявку на отгул (тестовый сервис отсутствий). Без вида оплаты и без причины. Идемпотентно по idempotency_key.",
		Handler:     s.create})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "absences", Name: "list_requests", ReadOnly: true,
		Description: "Список заявок на отгул. Стажёр — только свои; наставник — своих стажёров.",
		Handler:     s.list})
}

// Seed inserts the demo request ABS-000101 of the Backend intern.
func Seed(ctx context.Context, tx pgx.Tx, now time.Time) error {
	from, to := rules.SeedDates(now)
	_, err := tx.Exec(ctx, `INSERT INTO mock_absences.requests (request_id, employee_id, status, date_from, date_to, working_days, idempotency_key, params_hash, created_at)
		VALUES ('ABS-000101', 'u-intern-be', 'registered', $1, $2, 2, '00000000-0000-4000-8000-000000000101', $3, $4)`,
		from, to, rules.ParamsHash(from.Format("2006-01-02"), to.Format("2006-01-02")), now.Add(-24*time.Hour))
	return err
}

// Truncate removes requests and restarts numbering at 102 (12 §8.5).
func Truncate(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `TRUNCATE mock_absences.requests`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT setval('mock_absences.request_seq', 101)`)
	return err
}

func (s *Service) create(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor      mcpx.Actor `json:"actor"`
		Key        string     `json:"idempotency_key"`
		ParamsHash string     `json:"params_hash"`
		DateFrom   string     `json:"date_from"`
		DateTo     string     `json:"date_to"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Actor.Role != "intern" { // 2. guard (A14)
		return nil, mcpx.Denied("only an intern may request time off")
	}
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.createOnce(ctx, in.Actor.UserID, in.Key, in.ParamsHash, in.DateFrom, in.DateTo)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue // the same key won a race: repeat step 3 → idempotent replay (M-04)
		}
		return out, err
	}
	return nil, mcpx.Err("INTERNAL", "idempotency race was not resolved", nil)
}

func (s *Service) replay(ctx context.Context, q pgx.Tx, key, hash string) (any, bool, error) {
	var h string
	r, err := scan(q.QueryRow(ctx, `SELECT `+cols+` FROM mock_absences.requests WHERE idempotency_key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	_ = q.QueryRow(ctx, `SELECT params_hash FROM mock_absences.requests WHERE idempotency_key = $1`, key).Scan(&h)
	if h != hash {
		return nil, true, mcpx.Err("IDEMPOTENCY_CONFLICT", "idempotency_key was used with other parameters", nil)
	}
	return map[string]any{"request": r, "idempotent_replay": true}, true, nil
}

func (s *Service) createOnce(ctx context.Context, employeeID, key, hash, from, to string) (any, error) {
	var out any
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		// 3. idempotency
		if res, found, err := s.replay(ctx, tx, key, hash); found || err != nil {
			out = res
			return err
		}
		// 4. fault flag (10 §11.6): only writes are affected
		switch admin.GetIn(ctx, tx, admin.FlagAbsencesFault) {
		case "unavailable":
			return mcpx.Err("UNAVAILABLE", "absence service is unavailable (demo fault)", nil)
		case "slow":
			select {
			case <-time.After(SlowDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		// 5. rules + 6. overlap under an advisory lock per employee
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('absences:' || $1))`, employeeID); err != nil {
			return err
		}
		// the same key may have been committed while we waited for the lock
		if res, found, err := s.replay(ctx, tx, key, hash); found || err != nil {
			out = res
			return err
		}
		existing, err := own(ctx, tx, employeeID)
		if err != nil {
			return err
		}
		res, vs := rules.Validate(from, to, s.Clock.Now(), existing)
		if len(vs) > 0 {
			if vs[0].Code == rules.Overlap {
				return mcpx.Err("OVERLAP", "dates overlap an existing request", map[string]any{"conflicts": []any{vs[0].Params}})
			}
			return mcpx.Err("VALIDATION_FAILED", "dates break the rules", map[string]any{"violations": vs})
		}
		// 7. insert (unique index on idempotency_key — second line against duplicates)
		r, err := scan(tx.QueryRow(ctx, `INSERT INTO mock_absences.requests (employee_id, status, date_from, date_to, working_days, idempotency_key, params_hash, created_at)
			VALUES ($1, 'registered', $2, $3, $4, $5, $6, $7) RETURNING `+cols,
			employeeID, res.From, res.To, res.WorkingDays, key, hash, s.Clock.Now()))
		if err != nil {
			return err
		}
		out = map[string]any{"request": r, "idempotent_replay": false}
		return nil
	})
	return out, err
}

func own(ctx context.Context, tx pgx.Tx, employeeID string) ([]rules.Existing, error) {
	rows, err := tx.Query(ctx, `SELECT request_id, date_from, date_to, status FROM mock_absences.requests WHERE employee_id = $1 AND status = 'registered'`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rules.Existing
	for rows.Next() {
		var e rules.Existing
		if err := rows.Scan(&e.RequestID, &e.DateFrom, &e.DateTo, &e.Status); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) list(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor      mcpx.Actor `json:"actor"`
		EmployeeID string     `json:"employee_id"`
		Statuses   []string   `json:"statuses"`
		DateToGTE  string     `json:"date_to_gte"`
		Limit      int        `json:"limit"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	target := in.EmployeeID
	switch in.Actor.Role {
	case "intern":
		if target != "" && target != in.Actor.UserID {
			return nil, mcpx.Denied("an intern sees only own requests")
		}
		target = in.Actor.UserID
	case "mentor":
		if target == "" || !employees.IsMentee(ctx, s.Employees, in.Actor.UserID, target) {
			return nil, mcpx.Denied("a mentor sees requests of own interns only")
		}
	default:
		return nil, mcpx.Denied("role is not allowed")
	}
	if len(in.Statuses) == 0 {
		in.Statuses = []string{"registered"}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	var gte *time.Time
	if in.DateToGTE != "" {
		d, _ := time.Parse("2006-01-02", in.DateToGTE)
		gte = &d
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+cols+` FROM mock_absences.requests
		WHERE employee_id = $1 AND status = ANY($2) AND ($3::date IS NULL OR date_to >= $3)
		ORDER BY date_from LIMIT $4`, target, in.Statuses, gte, in.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Request{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return map[string]any{"items": items, "total": len(items)}, rows.Err()
}
