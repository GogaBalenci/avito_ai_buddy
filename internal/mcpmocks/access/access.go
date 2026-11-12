// Package access is the mock of the corporate access service, endpoint
// /mcp/access (11 §3.10): a catalog with roles, requests processed by the
// rule of the resource (auto / mentor / timer), mentor decisions. The timer
// is applied on read — there are no background jobs (11 §3.5.2).
package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/catalog"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mcpx"
)

// Service is the access mock.
type Service struct {
	Pool         *pgxpool.Pool
	Clock        clock.Clock
	Employees    employees.Provider
	Flags        *admin.Flags
	TimerMinutes int
}

// Resource is resource.v1.json.
type Resource struct {
	ResourceID      string   `json:"resource_id"`
	Title           string   `json:"title"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	Policy          string   `json:"policy"`
	EffectivePolicy string   `json:"effective_policy"`
	TimerMinutes    *int     `json:"timer_minutes"`
	AccessLevel     string   `json:"access_level"`
	Approver        *string  `json:"approver"`
	Allowed         bool     `json:"allowed"`
	DenyReasonCode  *string  `json:"deny_reason_code"`
	DenyExplanation *string  `json:"deny_explanation"`
	Aliases         []string `json:"aliases"`
}

// Request is request.v1.json.
type Request struct {
	RequestID       string     `json:"request_id"`
	EmployeeID      string     `json:"employee_id"`
	ResourceID      string     `json:"resource_id"`
	Status          string     `json:"status"`
	Policy          string     `json:"policy"`
	AccessLevel     string     `json:"access_level"`
	Justification   *string    `json:"justification"`
	CreatedAt       time.Time  `json:"created_at"`
	AutoApproveAt   *time.Time `json:"auto_approve_at"`
	DecidedAt       *time.Time `json:"decided_at"`
	DecidedBy       *string    `json:"decided_by"`
	DecidedByUserID *string    `json:"decided_by_user_id"`
	RejectReason    *string    `json:"reject_reason"`
}

const reqCols = `request_id, employee_id, resource_id, status, policy, access_level, justification, created_at,
	auto_approve_at, decided_at, decided_by, decided_by_user_id, reject_reason`

func scanReq(row pgx.Row) (Request, error) {
	var r Request
	err := row.Scan(&r.RequestID, &r.EmployeeID, &r.ResourceID, &r.Status, &r.Policy, &r.AccessLevel, &r.Justification,
		&r.CreatedAt, &r.AutoApproveAt, &r.DecidedAt, &r.DecidedBy, &r.DecidedByUserID, &r.RejectReason)
	return r, err
}

// ParamsHash is SHA-256 of the canonical JSON {"resource_id": …} (11 §3.12).
func ParamsHash(resourceID string) string {
	b, _ := json.Marshal(map[string]string{"resource_id": resourceID})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Register adds the tools.
func (s *Service) Register(srv *mcp.Server, log *slog.Logger) {
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "access", Name: "list_resources", ReadOnly: true,
		Description: "Каталог ресурсов тестового сервиса доступов с признаком, может ли роль их запросить.",
		Handler:     s.listResources})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "access", Name: "create_request", Idempotent: true,
		Description: "Создать заявку на доступ (тестовый сервис). Правило обработки задаётся ресурсом. Идемпотентно по idempotency_key.",
		Handler:     s.create})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "access", Name: "list_requests",
		Description: "Заявки на доступ. Стажёр — свои, наставник — своих стажёров. Перед выдачей применяет правило автоматического одобрения по created_at.",
		Handler:     s.listRequests})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "access", Name: "decide_request", Idempotent: true,
		Description: "Решение наставника по заявке своего стажёра. Идемпотентно по idempotency_key.",
		Handler:     s.decide})
}

// LoadContent writes the catalog into mock_access.resources (upsert).
func LoadContent(ctx context.Context, tx pgx.Tx, rs []catalog.Resource) error {
	for i, r := range rs {
		roles := r.AllowedRoles
		if roles == nil {
			roles = []string{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mock_access.resources (resource_id, title, kind, description, policy, access_level,
			approver, allowed_roles, deny_explanation, aliases, sort) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (resource_id) DO UPDATE SET title = EXCLUDED.title, kind = EXCLUDED.kind, description = EXCLUDED.description,
			  policy = EXCLUDED.policy, access_level = EXCLUDED.access_level, approver = EXCLUDED.approver,
			  allowed_roles = EXCLUDED.allowed_roles, deny_explanation = EXCLUDED.deny_explanation, aliases = EXCLUDED.aliases,
			  sort = EXCLUDED.sort`,
			r.ID, r.Title, r.Kind, r.Description, r.Policy, r.AccessLevel, r.Approver, roles, r.DenyExplanation, r.Aliases, i); err != nil {
			return err
		}
	}
	return nil
}

// SeedKey is the idempotency key of the seeded ACC-000101.
const SeedKey = "00000000-0000-4000-8000-000000000201"

// Seed inserts ACC-000101: the Backend intern waits for the mentor on
// space-cluster since yesterday (11 §3.11).
func Seed(ctx context.Context, tx pgx.Tx, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO mock_access.requests (request_id, employee_id, resource_id, status, policy, access_level,
		justification, idempotency_key, params_hash, created_at)
		VALUES ('ACC-000101', 'u-intern-be', 'space-cluster', 'pending', 'mentor', 'read',
		'Шаг 1.10 чек-листа адаптации: Запроси доступ в пространство своего кластера и команды и изучи его', $1, $2, $3)`,
		SeedKey, ParamsHash("space-cluster"), now.Add(-24*time.Hour))
	return err
}

// Truncate removes requests and decisions and restarts numbering at 102.
func Truncate(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `TRUNCATE mock_access.decisions, mock_access.requests`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT setval('mock_access.request_seq', 101)`)
	return err
}

type q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// mentorApproval reads the flag: off → mentor behaves like timer (11 §3.5.2).
func (s *Service) mentorApproval(ctx context.Context, db q) bool {
	return admin.GetIn(ctx, db, admin.FlagMentorApproval) != "off"
}

func (s *Service) effective(policy string, mentorOn bool) string {
	if policy == "mentor" && !mentorOn {
		return "timer"
	}
	return policy
}

func (s *Service) timer() time.Duration { return time.Duration(s.timerMinutes()) * time.Minute }

func (s *Service) timerMinutes() int {
	if s.TimerMinutes > 0 {
		return s.TimerMinutes
	}
	return 2
}

// applyTimer approves pending requests whose time has come — the only side
// effect of reading (11 §3.10.4). It is idempotent.
func (s *Service) applyTimer(ctx context.Context, db q, employeeIDs []string, mentorOn bool) ([]string, error) {
	rows, err := db.Query(ctx, `UPDATE mock_access.requests r
		SET status = 'approved', decided_by = 'timer', decided_at = r.created_at + make_interval(mins => $1)
		WHERE r.status = 'pending'
		  AND (r.policy = 'timer' OR (r.policy = 'mentor' AND NOT $2))
		  AND r.created_at + make_interval(mins => $1) <= $3
		  AND r.employee_id = ANY($4)
		RETURNING r.request_id`, s.timerMinutes(), mentorOn, s.Clock.Now(), employeeIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// view fills the effective policy and auto_approve_at of a request.
func (s *Service) view(r Request, mentorOn bool) Request {
	r.Policy = s.effective(r.Policy, mentorOn)
	if r.Status == "pending" && r.Policy == "timer" {
		t := r.CreatedAt.Add(s.timer())
		r.AutoApproveAt = &t
	}
	return r
}

type resourceRow struct {
	catalog.Resource
}

func (s *Service) resources(ctx context.Context, db q) ([]resourceRow, error) {
	rows, err := db.Query(ctx, `SELECT resource_id, title, kind, description, policy, access_level, approver, allowed_roles,
		deny_explanation, aliases FROM mock_access.resources ORDER BY sort`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []resourceRow
	for rows.Next() {
		var r resourceRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Kind, &r.Description, &r.Policy, &r.AccessLevel, &r.Approver,
			&r.AllowedRoles, &r.DenyExplanation, &r.Aliases); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) resourceView(r catalog.Resource, role string, mentorOn bool) Resource {
	v := Resource{ResourceID: r.ID, Title: r.Title, Kind: r.Kind, Description: r.Description, Policy: r.Policy,
		EffectivePolicy: s.effective(r.Policy, mentorOn), AccessLevel: r.AccessLevel, Approver: r.Approver,
		Allowed: r.AllowedFor(role), Aliases: r.Aliases}
	if v.Aliases == nil {
		v.Aliases = []string{}
	}
	if v.EffectivePolicy == "timer" {
		n := s.timerMinutes()
		v.TimerMinutes = &n
	}
	if !v.Allowed {
		code := "ROLE_NOT_ALLOWED"
		v.DenyReasonCode = &code
		v.DenyExplanation = r.DenyExplanation
		if v.DenyExplanation == nil {
			e := "Этот доступ роли не положен по тестовой матрице прав."
			v.DenyExplanation = &e
		}
	}
	return v
}

func (s *Service) listResources(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor         mcpx.Actor `json:"actor"`
		IncludeDenied *bool      `json:"include_denied"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	switch in.Actor.Role {
	case "intern", "mentor", "system":
	default:
		return nil, mcpx.Denied("role is not allowed")
	}
	rs, err := s.resources(ctx, s.Pool)
	if err != nil {
		return nil, err
	}
	mentorOn := s.mentorApproval(ctx, s.Pool)
	items := []Resource{}
	for _, r := range rs {
		v := s.resourceView(r.Resource, in.Actor.Role, mentorOn)
		if !v.Allowed && in.IncludeDenied != nil && !*in.IncludeDenied {
			continue
		}
		items = append(items, v)
	}
	return map[string]any{"items": items, "total": len(items)}, nil
}

func (s *Service) create(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor         mcpx.Actor `json:"actor"`
		Key           string     `json:"idempotency_key"`
		ParamsHash    string     `json:"params_hash"`
		ResourceID    string     `json:"resource_id"`
		Justification *string    `json:"justification"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Actor.Role != "intern" { // 2
		return nil, mcpx.Denied("only an intern may request access")
	}
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.createOnce(ctx, in.Actor.UserID, in.Key, in.ParamsHash, in.ResourceID, in.Justification)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// a parallel call won: the same key → replay (XM-15),
			// another key on the same resource → DUPLICATE_ACTIVE (XM-05)
			continue
		}
		return out, err
	}
	return nil, mcpx.Err("INTERNAL", "idempotency race was not resolved", nil)
}

func (s *Service) replay(ctx context.Context, tx pgx.Tx, key, hash string, mentorOn bool) (any, bool, error) {
	var h string
	r, err := scanReq(tx.QueryRow(ctx, `SELECT `+reqCols+` FROM mock_access.requests WHERE idempotency_key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	_ = tx.QueryRow(ctx, `SELECT params_hash FROM mock_access.requests WHERE idempotency_key = $1`, key).Scan(&h)
	if h != hash {
		return nil, true, mcpx.Err("IDEMPOTENCY_CONFLICT", "idempotency_key was used with other parameters", nil)
	}
	return map[string]any{"request": s.view(r, mentorOn), "idempotent_replay": true}, true, nil
}

func (s *Service) createOnce(ctx context.Context, employeeID, key, hash, resourceID string, justification *string) (any, error) {
	var out any
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		mentorOn := s.mentorApproval(ctx, tx)
		var r catalog.Resource
		err := tx.QueryRow(ctx, `SELECT resource_id, policy, access_level, allowed_roles FROM mock_access.resources WHERE resource_id = $1`,
			resourceID).Scan(&r.ID, &r.Policy, &r.AccessLevel, &r.AllowedRoles)
		if errors.Is(err, pgx.ErrNoRows) { // 3
			return mcpx.NotFound("no such resource in the test catalog")
		}
		if err != nil {
			return err
		}
		if !r.AllowedFor("intern") { // 4
			return mcpx.Err("PERMISSION_DENIED", "the role may not request this resource",
				map[string]any{"reason": "ROLE_NOT_ALLOWED", "resource_id": resourceID})
		}
		if res, found, err := s.replay(ctx, tx, key, hash, mentorOn); found || err != nil { // 5
			out = res
			return err
		}
		// 6. lock per (employee, resource), apply the timer, check active ones
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('access:' || $1 || ':' || $2))`, employeeID, resourceID); err != nil {
			return err
		}
		if res, found, err := s.replay(ctx, tx, key, hash, mentorOn); found || err != nil {
			out = res
			return err
		}
		if _, err := s.applyTimer(ctx, tx, []string{employeeID}, mentorOn); err != nil {
			return err
		}
		ex, err := scanReq(tx.QueryRow(ctx, `SELECT `+reqCols+` FROM mock_access.requests
			WHERE employee_id = $1 AND resource_id = $2 AND status IN ('pending', 'approved')`, employeeID, resourceID))
		if err == nil {
			return mcpx.Err("DUPLICATE_ACTIVE", "there is an active request for this resource",
				map[string]any{"existing": s.view(ex, mentorOn)})
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// 7. insert
		now := s.Clock.Now()
		status, decidedBy, decidedAt := "pending", (*string)(nil), (*time.Time)(nil)
		if s.effective(r.Policy, mentorOn) == "auto" {
			by := "auto"
			status, decidedBy, decidedAt = "approved", &by, &now
		}
		var autoAt *time.Time
		if r.Policy == "timer" {
			t := now.Add(s.timer())
			autoAt = &t
		}
		row, err := scanReq(tx.QueryRow(ctx, `INSERT INTO mock_access.requests (employee_id, resource_id, status, policy, access_level,
			justification, idempotency_key, params_hash, created_at, auto_approve_at, decided_at, decided_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+reqCols,
			employeeID, resourceID, status, r.Policy, r.AccessLevel, justification, key, hash, now, autoAt, decidedAt, decidedBy))
		if err != nil {
			return err
		}
		out = map[string]any{"request": s.view(row, mentorOn), "idempotent_replay": false}
		return nil
	})
	return out, err
}

// visible returns the employees whose requests the actor may read.
func (s *Service) visible(ctx context.Context, a mcpx.Actor, employeeID string) ([]string, error) {
	switch a.Role {
	case "intern":
		if employeeID != "" && employeeID != a.UserID {
			return nil, mcpx.Denied("an intern sees only own requests")
		}
		return []string{a.UserID}, nil
	case "mentor":
		if employeeID != "" {
			if !employees.IsMentee(ctx, s.Employees, a.UserID, employeeID) {
				return nil, mcpx.Denied("a mentor sees requests of own interns only")
			}
			return []string{employeeID}, nil
		}
		interns, err := s.Employees.InternsOf(ctx, a.UserID)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for _, e := range interns {
			ids = append(ids, e.UserID)
		}
		return ids, nil
	case "system":
		if employeeID == "" {
			return nil, mcpx.Err("VALIDATION_FAILED", "employee_id is required for system", nil)
		}
		return []string{employeeID}, nil
	}
	return nil, mcpx.Denied("role is not allowed")
}

func (s *Service) listRequests(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor      mcpx.Actor `json:"actor"`
		EmployeeID string     `json:"employee_id"`
		Statuses   []string   `json:"statuses"`
		RequestIDs []string   `json:"request_ids"`
		Limit      int        `json:"limit"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	ids, err := s.visible(ctx, in.Actor, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	mentorOn := s.mentorApproval(ctx, s.Pool)
	approved, err := s.applyTimer(ctx, s.Pool, ids, mentorOn)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+reqCols+` FROM mock_access.requests
		WHERE employee_id = ANY($1) AND (cardinality($2::text[]) = 0 OR status = ANY($2))
		  AND (cardinality($3::text[]) = 0 OR request_id = ANY($3))
		ORDER BY created_at DESC, request_id DESC LIMIT $4`, ids, nonNil(in.Statuses), nonNil(in.RequestIDs), in.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Request{}
	for rows.Next() {
		r, err := scanReq(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s.view(r, mentorOn))
	}
	return map[string]any{"items": items, "total": len(items), "auto_approved_now": approved}, rows.Err()
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (s *Service) decide(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor        mcpx.Actor `json:"actor"`
		Key          string     `json:"idempotency_key"`
		RequestID    string     `json:"request_id"`
		Decision     string     `json:"decision"`
		RejectReason *string    `json:"reject_reason"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Actor.Role != "mentor" {
		return nil, mcpx.Denied("only a mentor decides on access requests")
	}
	var out any
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		mentorOn := s.mentorApproval(ctx, tx)
		r, err := scanReq(tx.QueryRow(ctx, `SELECT `+reqCols+` FROM mock_access.requests WHERE request_id = $1 FOR UPDATE`, in.RequestID))
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !employees.IsMentee(ctx, s.Employees, in.Actor.UserID, r.EmployeeID) {
			return mcpx.NotFound("no such request") // existence is not revealed
		}
		if err != nil {
			return err
		}
		var prevDecision string
		var prevReason *string
		err = tx.QueryRow(ctx, `SELECT decision, reject_reason FROM mock_access.decisions WHERE idempotency_key = $1`, in.Key).Scan(&prevDecision, &prevReason)
		if err == nil {
			if prevDecision != in.Decision || !sameStr(prevReason, in.RejectReason) {
				return mcpx.Err("IDEMPOTENCY_CONFLICT", "idempotency_key was used with another decision", nil)
			}
			out = map[string]any{"request": s.view(r, mentorOn), "idempotent_replay": true}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := s.applyTimer(ctx, tx, []string{r.EmployeeID}, mentorOn); err != nil {
			return err
		}
		if r, err = scanReq(tx.QueryRow(ctx, `SELECT `+reqCols+` FROM mock_access.requests WHERE request_id = $1`, in.RequestID)); err != nil {
			return err
		}
		if r.Status != "pending" {
			return mcpx.Err("ALREADY_DECIDED", "the request is already decided", map[string]any{"request": s.view(r, mentorOn)})
		}
		status := "approved"
		if in.Decision == "reject" {
			status = "rejected"
		}
		now := s.Clock.Now()
		r, err = scanReq(tx.QueryRow(ctx, `UPDATE mock_access.requests SET status = $2, decided_by = 'mentor', decided_by_user_id = $3,
			decided_at = $4, reject_reason = $5 WHERE request_id = $1 RETURNING `+reqCols, in.RequestID, status, in.Actor.UserID, now, in.RejectReason))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mock_access.decisions (idempotency_key, request_id, decision, reject_reason, actor_user_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, in.Key, in.RequestID, in.Decision, in.RejectReason, in.Actor.UserID, now); err != nil {
			return err
		}
		out = map[string]any{"request": s.view(r, mentorOn), "idempotent_replay": false}
		return nil
	})
	return out, err
}

func sameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
