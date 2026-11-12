package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/access"
	accesssync "github.com/GogaBalenci/avito_ai_buddy/internal/access/sync"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// Access sources of a draft (event access_request_created.from).
const (
	FromStep       = "step"
	FromChat       = "chat"
	FromQuickReply = "quick_reply"
)

// CreateAccess checks the resource and stores an access draft (11 §3.7,
// p. 2–4). The step of the request is defined by the server: the step of the
// user's path whose help.resource is the resource; the client never sends it.
func (s *Service) CreateAccess(ctx context.Context, a actor.Actor, resourceID, from string) (Draft, error) {
	if a.Role != actor.Intern {
		s.denied(ctx, a, resourceID)
		return Draft{}, fail(403, "PERMISSION_DENIED", "Заявки на доступ оформляет стажёр")
	}
	res, ok, err := s.Catalog.Get(ctx, a, resourceID)
	if err != nil {
		return Draft{}, &Error{Status: 503, Code: "UNAVAILABLE", Retryable: true, Message: "Сервис доступов сейчас не отвечает. Попробуй ещё раз."}
	}
	if !ok {
		return Draft{}, fail(404, "NOT_FOUND", "Такого ресурса в тестовом каталоге нет")
	}
	roles := []string{}
	if res.Allowed {
		roles = []string{a.Role}
	}
	if err := s.Guard.Authorize(ctx, a, guard.AccessCreate, guard.Object{ResourceRoles: roles}); err != nil {
		s.denied(ctx, a, resourceID)
		msg := "Этот доступ роли стажёра не положен"
		if res.DenyExplanation != nil {
			msg = *res.DenyExplanation
		}
		return Draft{}, &Error{Status: 403, Code: "PERMISSION_DENIED", Message: msg, Resource: &res}
	}
	if ex, ok := s.activeAccess(ctx, a.UserID, resourceID); ok {
		return Draft{}, &Error{Status: 409, Code: "DUPLICATE_ACTIVE", Message: "Такая заявка уже есть", Existing: &ex, Resource: &res}
	}
	var stepID *string
	justification := "Запрос из чата бадди"
	if s.Path != nil {
		if l, err := s.Path.Load(ctx, a, ""); err == nil {
			for _, st := range l.Steps {
				if st.Resource == resourceID && st.Owner == "intern" {
					id := st.ID
					stepID = &id
					justification = fmt.Sprintf("Шаг %s чек-листа адаптации: %s", st.ChecklistRef, st.Title)
					break
				}
			}
		}
	}
	now := s.Clock.Now()
	params := map[string]string{"resource_id": resourceID, "from": from}
	if stepID != nil {
		params["step_id"] = *stepID
	}
	d := Draft{ActionID: uuid.NewString(), UserID: a.UserID, Kind: "access", Params: params, ParamsHash: access.ParamsHash(resourceID),
		Summary: map[string]any{"resource_id": resourceID, "title": res.Title, "access_level": res.AccessLevel, "approver": res.Approver,
			"effective_policy": res.EffectivePolicy, "timer_minutes": res.TimerMinutes, "step_id": stepID, "justification": justification},
		CreatedAt: now, ExpiresAt: now.Add(DraftLife)}
	replaced, err := s.Store.Save(ctx, d)
	if err != nil {
		return Draft{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	d.State = StateActive
	if replaced != "" {
		_ = s.Events.Publish(ctx, events.Event{Name: "action_cancelled", Payload: map[string]any{"kind": "access", "action_id": replaced, "reason": "replaced"}})
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "action_drafted", Payload: map[string]any{"kind": "access", "action_id": d.ActionID,
		"resource_id": resourceID, "effective_policy": res.EffectivePolicy}})
	return d, nil
}

func (s *Service) denied(ctx context.Context, a actor.Actor, resourceID string) {
	_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "access.create_request",
		"object": resourceID, "role": a.Role}})
}

// activeAccess finds a pending or approved local copy for (user, resource).
func (s *Service) activeAccess(ctx context.Context, userID, resourceID string) (api.RequestView, bool) {
	var v api.RequestView
	var title, level, policy string
	err := s.Pool.QueryRow(ctx, `SELECT request_id, status, created_at, resource_title, access_level, policy, step_id, auto_approve_at, approver
		FROM app.access_requests WHERE user_id = $1 AND resource_id = $2 AND status IN ('pending', 'approved')
		ORDER BY created_at DESC LIMIT 1`, userID, resourceID).
		Scan(&v.RequestId, &v.Status, &v.CreatedAt, &title, &level, &policy, &v.StepId, &v.AutoApproveAt, &v.Approver)
	if err != nil {
		return v, false
	}
	al, pol := api.AccessLevel(level), api.Policy(policy)
	v.Kind, v.ResourceId, v.ResourceTitle, v.AccessLevel, v.Policy = api.RequestKindAccess, &resourceID, &title, &al, &pol
	return v, true
}

// AccessView maps a request of the access service to the API view.
func AccessView(r accesssync.Request, title string, approver, stepID *string) api.RequestView {
	al, pol := api.AccessLevel(r.AccessLevel), api.Policy(r.Policy)
	v := api.RequestView{Kind: api.RequestKindAccess, RequestId: r.RequestID, Status: api.RequestViewStatus(r.Status), CreatedAt: r.CreatedAt,
		ResourceId: &r.ResourceID, ResourceTitle: &title, AccessLevel: &al, Policy: &pol, Approver: approver, StepId: stepID,
		Justification: r.Justification, AutoApproveAt: r.AutoApproveAt}
	if r.DecidedBy != nil {
		d := api.RequestViewDecidedBy(*r.DecidedBy)
		v.DecidedBy = &d
	}
	if r.RejectReason != nil {
		rr := api.RequestViewRejectReason(*r.RejectReason)
		v.RejectReason = &rr
	}
	return v
}

func (s *Service) confirmAccess(ctx context.Context, a actor.Actor, d Draft) (Confirmed, error) {
	resourceID := d.Params["resource_id"]
	res, ok, err := s.Catalog.Get(ctx, a, resourceID)
	if err != nil {
		return Confirmed{}, s.toolFailure(ctx, a, d, err, "access.create_request")
	}
	if !ok {
		return Confirmed{}, fail(404, "NOT_FOUND", "Такого ресурса в тестовом каталоге нет")
	}
	roles := []string{}
	if res.Allowed {
		roles = []string{a.Role}
	}
	if err := s.Guard.Authorize(ctx, a, guard.AccessCreate, guard.Object{ResourceRoles: roles}); err != nil { // guard again (11 §3.7)
		s.denied(ctx, a, resourceID)
		return Confirmed{}, fail(403, "PERMISSION_DENIED", "Этот доступ роли стажёра не положен")
	}
	var stepID *string
	if id, ok := d.Params["step_id"]; ok {
		stepID = &id
	}
	justification, _ := d.Summary["justification"].(string)
	start := time.Now()
	var out struct {
		Request accesssync.Request `json:"request"`
		Replay  bool               `json:"idempotent_replay"`
	}
	in := map[string]any{"actor": a, "idempotency_key": d.ActionID, "params_hash": d.ParamsHash, "resource_id": resourceID}
	if justification != "" {
		in["justification"] = justification
	}
	err = s.Tools.Call(ctx, "access", "create_request", in, &out)
	if mcpclient.IsCode(err, "DUPLICATE_ACTIVE") { // X-A4/X-A5: keep the existing one in the local copy and the step
		te, _ := mcpclient.AsToolError(err)
		var ex accesssync.Request
		if raw, e := json.Marshal(te.Details["existing"]); e == nil && json.Unmarshal(raw, &ex) == nil && ex.RequestID != "" {
			_, _ = s.Sync.Created(ctx, accesssync.Copy{UserID: a.UserID, ResourceTitle: res.Title, Approver: res.Approver, StepID: stepID}, ex)
			v := AccessView(ex, res.Title, res.Approver, stepID)
			s.failed(ctx, a, d, "DUPLICATE_ACTIVE")
			return Confirmed{}, &Error{Status: 409, Code: "DUPLICATE_ACTIVE", Message: "Такая заявка уже есть", Existing: &v, Resource: &res}
		}
	}
	if err != nil {
		return Confirmed{}, s.toolFailure(ctx, a, d, err, "access.create_request")
	}
	r := out.Request
	inserted, err := s.Sync.Created(ctx, accesssync.Copy{UserID: a.UserID, ResourceTitle: res.Title, Approver: res.Approver,
		StepID: stepID, ActionID: d.ActionID}, r)
	if err != nil && !inserted && !isStepErr(err) {
		// the request exists in the service; the copy is saved by the next confirm (same key → replay)
		return Confirmed{}, fail(503, "UNAVAILABLE", "Сервис временно недоступен")
	}
	now := s.Clock.Now()
	_, _ = s.Pool.Exec(ctx, `INSERT INTO app.action_log (action_id, user_id, kind, result, external_id, request_id, ts)
		VALUES ($1,$2,'access','completed',$3,$4,$5) ON CONFLICT DO NOTHING`, d.ActionID, a.UserID, r.RequestID, reqctx.RequestID(ctx), now)
	if inserted && !out.Replay {
		_ = s.Events.Publish(ctx, events.Event{Name: "action_completed", Payload: map[string]any{"kind": "access",
			"action_id": d.ActionID, "request_id": r.RequestID, "resource_id": resourceID, "latency_ms": time.Since(start).Milliseconds()}})
		_ = s.Events.Publish(ctx, events.Event{Name: "access_request_created", Payload: map[string]any{"request_id": r.RequestID,
			"resource_id": resourceID, "step_id": stepID, "policy": r.Policy, "status": r.Status, "from": d.Params["from"]}})
	}
	view := AccessView(r, res.Title, res.Approver, stepID)
	return Confirmed{Result: view, Reply: s.accessDoneReply(ctx, a, d, r, res, stepID)}, nil
}

// isStepErr: the copy is saved, only the step failed — the sync retries it.
func isStepErr(err error) bool {
	var te *mcpclient.ToolError
	return errors.As(err, &te)
}

// accessDoneReply is the result in the chat (11 §3.7, p. 6).
func (s *Service) accessDoneReply(ctx context.Context, a actor.Actor, d Draft, r accesssync.Request, res access.Resource, stepID *string) *api.AgentReply {
	rep := reply.New(uuid.MustParse(d.ActionID), "access_request")
	rep.MascotState = api.MascotStateDone
	switch {
	case r.Status == "approved":
		rep.Text = fmt.Sprintf("Готово: заявка №%s одобрена (тестовый сервис).", r.RequestID)
		if stepID != nil && s.Path != nil {
			if l, err := s.Path.Load(ctx, a, ""); err == nil {
				if st, ok := l.Step(*stepID); ok {
					rep.Text += " Шаг «" + st.Title + "» отмечен."
				}
				if l.Next != nil {
					if nx, ok := l.Step(l.Next.StepID); ok {
						rep.Text += " Дальше — «" + nx.Title + "»."
					}
				}
			}
		}
	case r.Policy == "timer":
		at := r.CreatedAt.Add(2 * time.Minute)
		if r.AutoApproveAt != nil {
			at = *r.AutoApproveAt
		}
		rep.Text = fmt.Sprintf("Заявка №%s создана, одобрится автоматически около %s.", r.RequestID, at.In(s.Clock.Now().Location()).Format("15:04"))
	default:
		rep.Text = fmt.Sprintf("Заявка №%s у наставника. Как только он одобрит, шаг закроется сам — обновлять страницу не нужно.", r.RequestID)
		reply.Copy(rep, "Скопировать сообщение наставнику", fmt.Sprintf("Привет! Отправил(а) заявку на доступ «%s» (%s). Посмотри, пожалуйста, во входящих бадди.",
			res.Title, r.RequestID), "")
	}
	reply.Link(rep, "Мои заявки", "/requests")
	return rep
}

// Summary builds ActionSummary from the draft summary map.
func Summary(d Draft) api.ActionSummary {
	var sm api.ActionSummary
	b, _ := json.Marshal(d.Summary)
	_ = json.Unmarshal(b, &sm)
	return sm
}

// Pending converts a draft to PendingAction.
func (st *Store) Pending(d Draft) api.PendingAction {
	return api.PendingAction{ActionId: uuid.MustParse(d.ActionID), Kind: api.RequestKind(d.Kind),
		State: api.PendingActionState(st.ViewState(d)), ExpiresAt: d.ExpiresAt, Summary: Summary(d)}
}
