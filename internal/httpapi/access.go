package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	accesssync "github.com/GogaBalenci/avito_ai_buddy/internal/access/sync"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// ListAccessResources: resources the role may request; denied ones are not
// listed so as not to prompt (11 §3.7, p. 2).
func (s *Server) ListAccessResources(w http.ResponseWriter, r *http.Request) {
	a, _ := reqctx.Actor(r.Context())
	rs, err := s.Catalog.List(r.Context(), a)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис доступов сейчас не отвечает", map[string]any{"retryable": true})
		return
	}
	items := []api.AccessResourceView{}
	for _, x := range rs {
		if !x.Allowed {
			continue
		}
		items = append(items, api.AccessResourceView{ResourceId: x.ResourceID, Title: x.Title, Kind: api.AccessResourceViewKind(x.Kind),
			Description: x.Description, AccessLevel: api.AccessLevel(x.AccessLevel), Approver: x.Approver,
			EffectivePolicy: api.Policy(x.EffectivePolicy), TimerMinutes: x.TimerMinutes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// inboxEntry is a local copy with its employee.
type inboxEntry struct {
	view   api.RequestView
	userID string
}

// mentorRequests reads local copies of the mentor's interns after syncing them.
func (s *Server) mentorRequests(ctx context.Context, a actor.Actor, status string) ([]api.RequestView, bool, error) {
	interns, err := s.Employees.InternsOf(ctx, a.UserID)
	if err != nil {
		return nil, false, err
	}
	stale := false
	items := []api.RequestView{}
	for _, e := range interns {
		if s.Sync != nil && s.Sync(ctx, e.UserID) != nil {
			stale = true
		}
		kind := api.RequestKindAccess
		list, err := s.requests(ctx, e.UserID, &kind, false)
		if err != nil {
			return nil, false, err
		}
		titles := map[string]string{}
		if l, err := s.Path.Load(ctx, a, e.UserID); err == nil {
			for _, st := range l.Steps {
				titles[st.ID] = st.Title
			}
		}
		for _, v := range list {
			if string(v.Status) != status {
				continue
			}
			if v.StepId != nil {
				if t, ok := titles[*v.StepId]; ok {
					v.StepTitle = &t
				}
			}
			v.Employee = &struct {
				DisplayName string     `json:"display_name"`
				UserId      api.UserId `json:"user_id"`
			}{DisplayName: e.DisplayName, UserId: e.UserID}
			items = append(items, v)
		}
	}
	sortByCreated(items)
	return items, stale, nil
}

// PendingInbox counts pending requests of the mentor's interns (chat X-A28).
func (s *Server) PendingInbox(ctx context.Context, mentorID string) (int, error) {
	items, _, err := s.mentorRequests(ctx, actor.Actor{UserID: mentorID, Role: actor.Mentor}, "pending")
	return len(items), err
}

// ListMentorAccessRequests is the mentor's inbox (11 §3.7, p. 7).
func (s *Server) ListMentorAccessRequests(w http.ResponseWriter, r *http.Request, params api.ListMentorAccessRequestsParams) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	if a.Role != actor.Mentor {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Экран доступен наставникам", nil)
		return
	}
	status := "pending"
	if params.Status != nil {
		status = string(*params.Status)
	}
	items, stale, err := s.mentorRequests(ctx, a, status)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	if status == "pending" {
		if ok, _ := s.Redis.SetNX(ctx, "ev:inbox_viewed:"+a.UserID, "1", 10*time.Minute).Result(); ok {
			_ = s.Events.Publish(ctx, events.Event{Name: "mentor_inbox_viewed", Payload: map[string]any{"pending_count": len(items)}})
		}
	}
	list := api.RequestList{Items: items, Total: len(items)}
	if stale {
		list.Stale = &stale
	}
	writeJSON(w, http.StatusOK, list)
}

// localCopy finds a local copy with its owner.
func (s *Server) localCopy(ctx context.Context, id string) (inboxEntry, bool) {
	var e inboxEntry
	kind := api.RequestKindAccess
	var userID string
	if err := s.Pool.QueryRow(ctx, `SELECT user_id FROM app.access_requests WHERE request_id = $1`, id).Scan(&userID); err != nil {
		return e, false
	}
	list, err := s.requests(ctx, userID, &kind, false)
	if err != nil {
		return e, false
	}
	for _, v := range list {
		if v.RequestId == id {
			return inboxEntry{view: v, userID: userID}, true
		}
	}
	return e, false
}

// DecideAccessRequest: the mentor approves or rejects (11 §3.7, p. 7).
func (s *Server) DecideAccessRequest(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	deny := func(object string) {
		_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "access.decide_request",
			"object": object, "role": a.Role}})
	}
	if a.Role != actor.Mentor {
		deny(id)
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Решение по заявке принимает наставник", nil)
		return
	}
	if !s.limit(w, r, "act") {
		return
	}
	var body api.DecisionRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	if (body.Decision == api.Reject) != (body.RejectReason != nil) {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Причина отказа обязательна при отклонении и не нужна при одобрении", nil)
		return
	}
	e, ok := s.localCopy(ctx, id)
	if !ok || s.Guard.Authorize(ctx, a, guard.AccessList, guard.Object{EmployeeID: e.userID}) != nil {
		deny(id)
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена", nil)
		return
	}
	if s.Sync != nil {
		_ = s.Sync(ctx, e.userID)
		if e2, ok := s.localCopy(ctx, id); ok {
			e = e2
		}
	}
	in := map[string]any{"actor": a, "idempotency_key": body.IdempotencyKey.String(), "request_id": id, "decision": string(body.Decision)}
	if body.RejectReason != nil {
		in["reject_reason"] = string(*body.RejectReason)
	}
	var out struct {
		Request accesssync.Request `json:"request"`
	}
	err := s.Tools.Call(ctx, "access", "decide_request", in, &out)
	if err != nil {
		te, _ := mcpclient.AsToolError(err)
		code := mcpclient.CodeUnavailable
		if te != nil {
			code = te.Code
		}
		switch code {
		case "ALREADY_DECIDED":
			var cur accesssync.Request
			if raw, e2 := json.Marshal(te.Details["request"]); e2 == nil && json.Unmarshal(raw, &cur) == nil && s.AccessSync != nil {
				_ = s.AccessSync.Apply(ctx, e.userID, e.view.StepId, cur)
			}
			if e2, ok := s.localCopy(ctx, id); ok {
				e = e2
			}
			writeError(w, r, http.StatusConflict, "ALREADY_DECIDED", alreadyDecided(e.view), map[string]any{"request": s.withEmployee(ctx, e)})
		case "NOT_FOUND":
			deny(id)
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Заявка не найдена", nil)
		case "PERMISSION_DENIED":
			deny(id)
			writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Недостаточно прав", nil)
		case "IDEMPOTENCY_CONFLICT":
			writeError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Это решение уже принято с другими параметрами", nil)
		default:
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис доступов сейчас не отвечает — попробуй ещё раз", map[string]any{"retryable": true})
		}
		return
	}
	if s.AccessSync != nil {
		if err := s.AccessSync.Apply(ctx, e.userID, e.view.StepId, out.Request); err != nil {
			s.Log.Warn("access sync after decision", "err", err, "request_id", reqctx.RequestID(ctx))
		}
	}
	v := actions.AccessView(out.Request, str(e.view.ResourceTitle), e.view.Approver, e.view.StepId)
	writeJSON(w, http.StatusOK, s.withEmployee(ctx, inboxEntry{view: v, userID: e.userID}))
}

func alreadyDecided(v api.RequestView) string {
	switch {
	case v.Status == "rejected":
		return "Заявку уже отклонили"
	case v.DecidedBy != nil && *v.DecidedBy == "timer":
		return "Уже одобрено автоматически"
	}
	return "Заявку уже одобрили"
}

func (s *Server) withEmployee(ctx context.Context, e inboxEntry) api.RequestView {
	v := e.view
	if emp, err := s.Employees.Get(ctx, e.userID); err == nil {
		v.Employee = &struct {
			DisplayName string     `json:"display_name"`
			UserId      api.UserId `json:"user_id"`
		}{DisplayName: emp.DisplayName, UserId: emp.UserID}
	}
	return v
}
