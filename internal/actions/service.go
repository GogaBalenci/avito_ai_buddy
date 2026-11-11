package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/timeoff/rules"
)

// Service creates and confirms drafts.
type Service struct {
	Store  *Store
	Tools  mcpclient.Client
	Guard  *guard.Guard
	Pool   *pgxpool.Pool
	Events events.Publisher
	Clock  clock.Clock
	// ConfirmWait is how long a parallel confirm waits for the first one (10 §8.1).
	ConfirmWait time.Duration
}

// Error is a failure with an HTTP meaning.
type Error struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
	Fields    []api.FieldError
	Draft     *Draft // for 410: the dates to refill the form
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// FieldErrors maps rule violations to API field errors.
func FieldErrors(vs []rules.Violation) []api.FieldError {
	out := make([]api.FieldError, 0, len(vs))
	for _, v := range vs {
		fe := api.FieldError{Field: api.FieldErrorField(v.Field), Code: v.Code, Message: rules.Message(v)}
		if len(v.Params) > 0 {
			p := v.Params
			fe.Params = &p
		}
		out = append(out, fe)
	}
	return out
}

// existing reads upcoming registered requests of the intern (for the
// overlap hint and the check); nil, false when the service is down (A23).
func (s *Service) existing(ctx context.Context, a actor.Actor) ([]rules.Existing, []api.TimeOffBrief, bool) {
	var out struct {
		Items []struct {
			RequestID string `json:"request_id"`
			DateFrom  string `json:"date_from"`
			DateTo    string `json:"date_to"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	err := s.Tools.Call(ctx, "absences", "list_requests", map[string]any{"actor": a, "date_to_gte": clock.ISO(clock.Today(s.Clock))}, &out)
	if err != nil {
		return nil, nil, false
	}
	var ex []rules.Existing
	briefs := []api.TimeOffBrief{}
	for _, it := range out.Items {
		f, _ := clock.ParseISO(it.DateFrom)
		t, _ := clock.ParseISO(it.DateTo)
		ex = append(ex, rules.Existing{RequestID: it.RequestID, DateFrom: f, DateTo: t, Status: it.Status})
		briefs = append(briefs, api.TimeOffBrief{RequestId: it.RequestID, DateFrom: openapi_types.Date{Time: f}, DateTo: openapi_types.Date{Time: t}})
	}
	return ex, briefs, true
}

// Upcoming returns up to 3 upcoming requests for the form hint.
func (s *Service) Upcoming(ctx context.Context, a actor.Actor) ([]api.TimeOffBrief, bool) {
	_, b, ok := s.existing(ctx, a)
	if len(b) > 3 {
		b = b[:3]
	}
	return b, ok
}

// CreateTimeOff validates dates and stores a draft (10 §5, step 5).
func (s *Service) CreateTimeOff(ctx context.Context, a actor.Actor, from, to string) (Draft, error) {
	if err := s.Guard.Authorize(ctx, a, guard.AbsencesCreate, guard.Object{}); err != nil {
		_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "absences.create_request"}})
		return Draft{}, fail(403, "PERMISSION_DENIED", "Оформлять отгул может только стажёр")
	}
	ex, _, listed := s.existing(ctx, a)
	res, vs := rules.Validate(from, to, s.Clock.Now(), ex)
	if len(vs) > 0 {
		codes := make([]string, 0, len(vs))
		for _, v := range vs {
			codes = append(codes, v.Code)
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "time_off_validation_failed", Payload: map[string]any{"codes": codes, "stage": "draft"}})
		return Draft{}, &Error{Status: 422, Code: "VALIDATION_FAILED", Message: "Проверь даты", Fields: FieldErrors(vs)}
	}
	now := s.Clock.Now()
	d := Draft{ActionID: uuid.NewString(), UserID: a.UserID, Kind: "time_off",
		Params: map[string]string{"date_from": from, "date_to": to}, ParamsHash: rules.ParamsHash(from, to),
		Summary:   map[string]any{"date_from": from, "date_to": to, "working_days": res.WorkingDays},
		CreatedAt: now, ExpiresAt: now.Add(DraftLife)}
	if !listed {
		d.Notices = []string{"overlap_unchecked"}
	}
	replaced, err := s.Store.Save(ctx, d)
	if err != nil {
		return Draft{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	d.State = StateActive
	if replaced != "" {
		_ = s.Events.Publish(ctx, events.Event{Name: "action_cancelled", Payload: map[string]any{"kind": "time_off", "action_id": replaced, "reason": "replaced"}})
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "action_drafted", Payload: map[string]any{"kind": "time_off", "action_id": d.ActionID,
		"working_days": res.WorkingDays, "lead_days": clock.DaysBetween(now, res.From), "overlap_unchecked": !listed}})
	return d, nil
}

// Get returns a draft of the user (404 otherwise).
func (s *Service) Get(ctx context.Context, a actor.Actor, id string) (Draft, error) {
	d, err := s.Store.Get(ctx, id, a.UserID)
	if errors.Is(err, ErrNotFound) {
		return d, fail(404, "NOT_FOUND", "Черновик не найден — давай заполним заново")
	}
	if err != nil {
		return d, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	return d, nil
}

// Cancel cancels an active draft (A19).
func (s *Service) Cancel(ctx context.Context, a actor.Actor, id string) (Draft, error) {
	st, err := s.Store.Cancel(ctx, id, a.UserID)
	if err != nil {
		return Draft{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	if e := stateError(st); e != nil {
		return Draft{}, e
	}
	d, err := s.Get(ctx, a, id)
	if err == nil {
		_ = s.Events.Publish(ctx, events.Event{Name: "action_cancelled", Payload: map[string]any{"kind": d.Kind, "action_id": id, "reason": "user"}})
	}
	return d, err
}

// stateError maps a non-ok state of Begin/Cancel to the HTTP error.
func stateError(st string) *Error {
	switch st {
	case "ok":
		return nil
	case "not_found", "":
		return fail(404, "NOT_FOUND", "Черновик не найден — давай заполним заново")
	case StateExpired:
		return fail(410, "ACTION_EXPIRED", "Черновик устарел — давай заполним заново")
	case StateCancelled:
		return fail(410, "ACTION_CANCELLED", "Этот черновик отменён — заполни форму заново")
	case StateReplaced:
		return fail(410, "ACTION_REPLACED", "Этот черновик заменён новым — подтверди его ниже")
	case StateConfirmed:
		return fail(409, "STATUS_CONFLICT", "Заявка уже создана")
	}
	return fail(409, "STATUS_CONFLICT", "Черновик сейчас подтверждается")
}

// Confirmed is the result of a confirm.
type Confirmed struct {
	Result api.RequestView
	Reply  *api.AgentReply
}

// Confirm is the only path that writes to a corporate service (AG-05).
func (s *Service) Confirm(ctx context.Context, a actor.Actor, id string) (Confirmed, error) {
	st, err := s.Store.Begin(ctx, id, a.UserID)
	if err != nil {
		return Confirmed{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	if st == StateConfirming { // a parallel confirm is running: wait for it (10 §8.1)
		deadline := time.Now().Add(s.wait())
		for time.Now().Before(deadline) && s.Store.State(ctx, id) == StateConfirming {
			time.Sleep(50 * time.Millisecond)
		}
		st = s.Store.State(ctx, id)
		switch st {
		case StateActive:
			if st, err = s.Store.Begin(ctx, id, a.UserID); err != nil {
				return Confirmed{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
			}
		case StateConfirming:
			st = "ok" // still confirming: repeat the idempotent call with the same key
		}
	}
	d, err := s.Store.Get(ctx, id, a.UserID)
	if err != nil {
		return Confirmed{}, stateError("not_found")
	}
	if st == StateConfirmed {
		return s.saved(d)
	}
	if st == StateExpired {
		_ = s.Events.Publish(ctx, events.Event{Name: "action_expired", Payload: map[string]any{"kind": d.Kind}})
		e := stateError(st)
		e.Draft = &d
		return Confirmed{}, e
	}
	if e := stateError(st); e != nil {
		e.Draft = &d
		return Confirmed{}, e
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "action_confirmed", Payload: map[string]any{"kind": d.Kind, "action_id": id}})
	var res Confirmed
	switch d.Kind {
	case "time_off":
		res, err = s.confirmTimeOff(ctx, a, d)
	default:
		err = fail(400, "BAD_REQUEST", "Неизвестный вид черновика")
	}
	if err != nil {
		_ = s.Store.Revert(ctx, id)
		return Confirmed{}, err
	}
	if err := s.Store.Finish(ctx, id, res); err != nil {
		return Confirmed{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	return res, nil
}

func (s *Service) wait() time.Duration {
	if s.ConfirmWait > 0 {
		return s.ConfirmWait
	}
	return 3 * time.Second
}

func (s *Service) saved(d Draft) (Confirmed, error) {
	var c Confirmed
	if err := json.Unmarshal(d.Result, &c); err != nil {
		return c, fail(503, "INTERNAL", "Не получилось прочитать результат")
	}
	return c, nil
}

func (s *Service) failed(ctx context.Context, a actor.Actor, d Draft, code string) {
	_ = s.Events.Publish(ctx, events.Event{Name: "action_failed", Payload: map[string]any{"kind": d.Kind, "action_id": d.ActionID, "error_code": code}})
	_, _ = s.Pool.Exec(ctx, `INSERT INTO app.action_log (action_id, user_id, kind, result, error_code, request_id, ts)
		VALUES ($1, $2, $3, 'failed', $4, $5, $6)`, d.ActionID, a.UserID, d.Kind, code, reqctx.RequestID(ctx), s.Clock.Now())
}

// toolFailure maps a ToolClient error on confirm (10 §11.5).
func (s *Service) toolFailure(ctx context.Context, a actor.Actor, d Draft, err error, service string) error {
	te, _ := mcpclient.AsToolError(err)
	code := mcpclient.CodeUnavailable
	if te != nil {
		code = te.Code
	}
	s.failed(ctx, a, d, code)
	switch code {
	case "VALIDATION_FAILED", "OVERLAP":
		var vs []rules.Violation
		if te != nil {
			if raw, err := json.Marshal(te.Details["violations"]); err == nil {
				_ = json.Unmarshal(raw, &vs)
			}
			if code == "OVERLAP" {
				if cs, ok := te.Details["conflicts"].([]any); ok && len(cs) > 0 {
					p, _ := cs[0].(map[string]any)
					vs = []rules.Violation{{Field: "range", Code: rules.Overlap, Params: p}}
				}
			}
		}
		if len(vs) == 0 {
			vs = []rules.Violation{{Field: "range", Code: "UNKNOWN"}}
		}
		return &Error{Status: 422, Code: "VALIDATION_FAILED", Message: "Проверь даты", Fields: FieldErrors(vs), Draft: &d}
	case "PERMISSION_DENIED":
		_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": service}})
		return fail(403, "PERMISSION_DENIED", "Недостаточно прав")
	case "IDEMPOTENCY_CONFLICT":
		return fail(409, "IDEMPOTENCY_CONFLICT", "Что-то пошло не так — заполни форму заново")
	case "NOT_FOUND":
		return fail(404, "NOT_FOUND", "Не найдено")
	case "DUPLICATE_ACTIVE":
		return fail(409, "DUPLICATE_ACTIVE", "Такая заявка уже есть")
	case mcpclient.CodeTimeout:
		return &Error{Status: 503, Code: "TIMEOUT", Retryable: true,
			Message: "Ой, сервис " + serviceName(service) + " сейчас не отвечает, заявка не создана. Попробуй ещё раз — дубликата не будет."}
	}
	c := "UNAVAILABLE"
	if code == mcpclient.CodeInternal {
		c = "INTERNAL"
	}
	return &Error{Status: 503, Code: c, Retryable: true,
		Message: "Ой, сервис " + serviceName(service) + " сейчас не отвечает, заявка не создана. Попробуй ещё раз — дубликата не будет."}
}

func serviceName(tool string) string {
	if tool == "absences.create_request" {
		return "отсутствий"
	}
	return "доступов"
}

func (s *Service) confirmTimeOff(ctx context.Context, a actor.Actor, d Draft) (Confirmed, error) {
	if err := s.Guard.Authorize(ctx, a, guard.AbsencesCreate, guard.Object{}); err != nil {
		return Confirmed{}, fail(403, "PERMISSION_DENIED", "Оформлять отгул может только стажёр")
	}
	from, to := d.Params["date_from"], d.Params["date_to"]
	ex, _, _ := s.existing(ctx, a)
	if _, vs := rules.Validate(from, to, s.Clock.Now(), ex); len(vs) > 0 { // A8, A9: re-check
		codes := []string{}
		for _, v := range vs {
			codes = append(codes, v.Code)
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "time_off_validation_failed", Payload: map[string]any{"codes": codes, "stage": "confirm"}})
		s.failed(ctx, a, d, vs[0].Code)
		return Confirmed{}, &Error{Status: 422, Code: "VALIDATION_FAILED", Message: "Проверь даты", Fields: FieldErrors(vs), Draft: &d}
	}
	start := time.Now()
	var out struct {
		Request struct {
			RequestID   string    `json:"request_id"`
			Status      string    `json:"status"`
			DateFrom    string    `json:"date_from"`
			DateTo      string    `json:"date_to"`
			WorkingDays int       `json:"working_days"`
			CreatedAt   time.Time `json:"created_at"`
		} `json:"request"`
		Replay bool `json:"idempotent_replay"`
	}
	err := s.Tools.Call(ctx, "absences", "create_request", map[string]any{"actor": a, "idempotency_key": d.ActionID,
		"params_hash": d.ParamsHash, "type": "time_off", "date_from": from, "date_to": to}, &out)
	if err != nil {
		return Confirmed{}, s.toolFailure(ctx, a, d, err, "absences.create_request")
	}
	r := out.Request
	f, _ := clock.ParseISO(r.DateFrom)
	t, _ := clock.ParseISO(r.DateTo)
	now := s.Clock.Now()
	inserted := false
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO app.time_off_requests (external_id, user_id, date_from, date_to, working_days, status, action_id, created_at, synced_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (external_id) DO NOTHING`,
			r.RequestID, a.UserID, f, t, r.WorkingDays, r.Status, d.ActionID, r.CreatedAt, now)
		if err != nil {
			return err
		}
		inserted = tag.RowsAffected() > 0
		if _, err := tx.Exec(ctx, `INSERT INTO app.action_log (action_id, user_id, kind, result, external_id, request_id, ts)
			VALUES ($1,$2,'time_off','completed',$3,$4,$5) ON CONFLICT DO NOTHING`, d.ActionID, a.UserID, r.RequestID, reqctx.RequestID(ctx), now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Confirmed{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	if inserted && !out.Replay { // exactly once: the local copy is inserted once
		_ = s.Events.Publish(ctx, events.Event{Name: "action_completed", Payload: map[string]any{"kind": "time_off",
			"action_id": d.ActionID, "request_id": r.RequestID, "latency_ms": time.Since(start).Milliseconds()}})
	}
	wd := r.WorkingDays
	view := api.RequestView{Kind: api.RequestKindTimeOff, RequestId: r.RequestID, Status: api.RequestViewStatus(r.Status), CreatedAt: r.CreatedAt,
		DateFrom: &openapi_types.Date{Time: f}, DateTo: &openapi_types.Date{Time: t}, WorkingDays: &wd}
	rep := reply.New(uuid.MustParse(d.ActionID), "time_off")
	rep.MascotState = api.MascotStateDone
	rep.Text = fmt.Sprintf("Готово: заявка №%s, статус «Оформлена». Она уже в «Моём пути» и в истории.", r.RequestID)
	reply.Copy(rep, "Скопировать сообщение наставнику", fmt.Sprintf("Привет! Оформил(а) отгул на %s–%s (заявка %s). Если до этого нужно что-то передать — скажи.",
		f.Format("02.01"), t.Format("02.01"), r.RequestID), "")
	reply.Link(rep, "Мои заявки", "/requests")
	return Confirmed{Result: view, Reply: rep}, nil
}
