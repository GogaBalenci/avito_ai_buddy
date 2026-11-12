package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

func confirm(t *testing.T, c *client, r api.AgentReply) api.ConfirmResponse {
	t.Helper()
	if r.PendingAction == nil {
		t.Fatalf("no pending action: %+v", r)
	}
	rec := c.do("POST", "/api/actions/"+r.PendingAction.ActionId.String()+"/confirm", nil)
	expect(t, rec, http.StatusOK)
	return decode[api.ConfirmResponse](t, rec)
}

func decideReq(c *client, id, decision string, reason string, key string) *httptest.ResponseRecorder {
	body := map[string]any{"decision": decision, "idempotency_key": key}
	if reason != "" {
		body["reject_reason"] = reason
	}
	return c.do("POST", "/api/mentor/access-requests/"+id+"/decision", body)
}

// XA-04, XA-09, XA-24: the step button cm-1.7 → draft → confirm → approved, step done.
func TestAccessFromStep(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	id := newID()
	rec := sendRaw(be, id, map[string]any{"payload": "step:cm-1.7"})
	expect(t, rec, http.StatusOK)
	r := decode[api.AgentReply](t, rec)
	sm := r.PendingAction.Summary
	if r.Intent != "access_request" || r.PendingAction.Kind != "access" || *sm.ResourceId != "team-channels" || *sm.StepId != "cm-1.7" ||
		*sm.EffectivePolicy != "auto" || *sm.AccessLevel != "write" || sm.Approver == nil || !strings.Contains(*sm.Justification, "Шаг 1.7") ||
		!strings.Contains(r.Text, "Одобряется сразу") || len(r.Sources) != 1 || r.Sources[0].Slug != "access-test-order" {
		t.Fatalf("draft %+v %+v", r, sm)
	}
	if st := stepStatus(t, be, "cm-1.7"); st != "in_progress" {
		t.Fatalf("user_start: %s", st)
	}
	again := decode[api.AgentReply](t, sendRaw(be, id, map[string]any{"payload": "step:cm-1.7"})) // XA-24
	if again.PendingAction.ActionId != r.PendingAction.ActionId || len(h.events.Events("action_drafted")) != 1 {
		t.Fatal("XA-24: a repeat creates no second draft")
	}
	c := confirm(t, be, r)
	if c.Result.Status != "approved" || c.Result.RequestId != "ACC-000102" || !strings.Contains(c.Reply.Text, "одобрена") ||
		!strings.Contains(c.Reply.Text, "Дальше — «Изучи принципы разработки Авито»") {
		t.Fatalf("confirm %+v / %s", c.Result, c.Reply.Text)
	}
	c2 := confirm(t, be, r) // XA-09
	if c2.Result.RequestId != c.Result.RequestId {
		t.Fatal("XA-09: one request")
	}
	if st := stepStatus(t, be, "cm-1.7"); st != "done" {
		t.Fatalf("step %s", st)
	}
	p := decode[api.PathView](t, be.do("GET", "/api/path", nil))
	if p.NextStep.StepId != "be-1.11" {
		t.Fatalf("next %+v", p.NextStep)
	}
	for _, name := range []string{"action_completed", "access_request_created", "access_request_decided"} {
		if n := len(h.events.Events(name)); n != 1 {
			t.Fatalf("%s: %d", name, n)
		}
	}
	if ev := h.events.Events("access_request_created")[0]; ev.Payload["from"] != "step" {
		t.Fatalf("from %+v", ev.Payload)
	}
	// X-A5: approved already
	r = say(t, be, "добавь меня в ключевые каналы")
	if r.PendingAction != nil || !strings.Contains(r.Text, "Доступ уже одобрен (заявка №ACC-000102)") {
		t.Fatalf("X-A5 %+v", r)
	}
}

// XA-05, XA-06: T-09 → timer draft; +2 min → be-1.15 done without reload.
func TestAccessTimer(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	r := say(t, be, "добавь меня в tech-dep")
	if r.Intent != "access_request" || !strings.Contains(r.Text, "Одобрится автоматически примерно через 2 мин") || *r.PendingAction.Summary.StepId != "be-1.15" {
		t.Fatalf("XA-05 %+v", r)
	}
	c := confirm(t, be, r)
	if c.Result.Status != "pending" || !strings.Contains(c.Reply.Text, "одобрится автоматически около 12:02") {
		t.Fatalf("timer reply %+v %s", c.Result, c.Reply.Text)
	}
	if st := stepStatus(t, be, "be-1.15"); st != "waiting" {
		t.Fatalf("waiting: %s", st)
	}
	r = say(t, be, "дай доступ в tech-dep") // X-A4
	if r.PendingAction != nil || !strings.Contains(r.Text, "уже ждёт автоматического одобрения около 12:02") {
		t.Fatalf("X-A4 %+v", r)
	}
	h.clock.Advance(2 * time.Minute)
	p := decode[api.PathView](t, be.do("GET", "/api/path", nil))
	for _, s := range p.Steps {
		if s.StepId == "be-1.15" && s.Status != "done" {
			t.Fatalf("XA-06: %s", s.Status)
		}
	}
	if ev := h.events.Events("access_request_decided"); len(ev) != 1 || ev[0].Payload["decided_by"] != "timer" {
		t.Fatalf("decided %+v", ev)
	}
}

// XA-01, XA-02, XA-03, XA-19, T-11
func TestAccessDenied(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	r := say(t, be, "дай доступ на запись в продовую базу данных")
	if r.PendingAction != nil || r.MascotState != "ask_mentor" || r.Escalation == nil || r.Escalation.Reason != "permission_denied" ||
		!strings.Contains(r.Text, "роли стажёра не положен") {
		t.Fatalf("XA-01 %+v", r)
	}
	if ev := h.events.Events("permission_denied"); len(ev) != 1 || ev[0].Payload["object"] != "prod-db-write" {
		t.Fatalf("permission_denied %+v", ev)
	}
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "access", "params": map[string]any{"resource_id": "prod-db-write"}}), http.StatusForbidden)
	r = say(t, be, "нужен доступ") // T-10
	if r.PendingAction != nil || !hasPayload(r, "resource:team-channels") || hasPayload(r, "resource:prod-db-write") || len(payloads(r)) != 6 {
		t.Fatalf("XA-03 %+v", payloads(r))
	}
	expect(t, sendRaw(be, newID(), map[string]any{"payload": "resource:prod-db-write"}), http.StatusForbidden)
	expect(t, sendRaw(be, newID(), map[string]any{"payload": "resource:nope"}), http.StatusNotFound)
	expect(t, sendRaw(be, newID(), map[string]any{"payload": "resource:"}), http.StatusBadRequest)
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "access", "params": map[string]any{"resource_id": "nope"}}), http.StatusNotFound)
	r = press(t, be, "intent:access_request")
	if !hasPayload(r, "resource:channel-tech-dep") {
		t.Fatalf("menu %+v", r)
	}
	r = press(t, be, "resource:channel-tech-dep")
	if r.PendingAction == nil {
		t.Fatalf("quick reply %+v", r)
	}
}

// XA-07, XA-10, XA-11, XA-12, XA-13, XA-26: the mentor decides.
func TestAccessMentorDecides(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	m := h.login("u-mentor")
	r := say(t, be, "дай доступ в пространство кластера") // XA-10
	if r.PendingAction != nil || !strings.Contains(r.Text, "ACC-000101 уже ждёт наставника") {
		t.Fatalf("XA-10 %+v", r)
	}
	r = say(t, m, "что с заявками?") // XA-26
	if !strings.Contains(r.Text, "Входящих заявок: 1") {
		t.Fatalf("XA-26 %+v", r)
	}
	inbox := decode[api.RequestList](t, m.do("GET", "/api/mentor/access-requests", nil))
	if inbox.Total != 1 || inbox.Items[0].RequestId != "ACC-000101" || inbox.Items[0].Employee == nil || inbox.Items[0].StepTitle == nil {
		t.Fatalf("inbox %+v", inbox)
	}
	m.do("GET", "/api/mentor/access-requests", nil)
	if n := len(h.events.Events("mentor_inbox_viewed")); n != 1 {
		t.Fatalf("inbox viewed deduplicated: %d", n)
	}
	expect(t, decideReq(be, "ACC-000101", "approve", "", newID()), http.StatusForbidden) // XA-13
	expect(t, decideReq(m, "ACC-000101", "reject", "", newID()), http.StatusBadRequest)
	expect(t, decideReq(m, "ACC-000101", "approve", "not_needed", newID()), http.StatusBadRequest)
	expect(t, decideReq(m, "ACC-000999", "approve", "", newID()), http.StatusNotFound)
	key := newID()
	rec := decideReq(m, "ACC-000101", "approve", "", key)
	expect(t, rec, http.StatusOK)
	v := decode[api.RequestView](t, rec)
	if v.Status != "approved" || *v.DecidedBy != "mentor" || v.Employee == nil {
		t.Fatalf("decision %+v", v)
	}
	expect(t, decideReq(m, "ACC-000101", "approve", "", key), http.StatusOK) // repeat with the same key
	if st := stepStatus(t, be, "cm-1.10"); st != "done" {                    // XA-11
		t.Fatalf("XA-11 %s", st)
	}
	expect(t, decideReq(m, "ACC-000101", "reject", "not_needed", newID()), http.StatusConflict, "ALREADY_DECIDED")
	if n := len(h.events.Events("access_request_decided")); n != 1 {
		t.Fatalf("decided once: %d", n)
	}
	// XA-12, XA-17: figma (not on the Backend path) → rejected → «Запросить снова»
	r = say(t, be, "дай доступ к фигме")
	if r.PendingAction == nil || r.PendingAction.Summary.StepId != nil || !strings.Contains(*r.PendingAction.Summary.Justification, "Запрос из чата бадди") {
		t.Fatalf("XA-17 %+v", r)
	}
	c := confirm(t, be, r)
	if c.Result.Status != "pending" || c.Result.StepId != nil || hasAction(c.Reply, api.Copy) == nil {
		t.Fatalf("mentor policy %+v", c.Result)
	}
	expect(t, decideReq(m, c.Result.RequestId, "reject", "discuss_1on1", newID()), http.StatusOK)
	list := decode[api.RequestList](t, be.do("GET", "/api/requests?kind=access", nil))
	found := false
	for _, it := range list.Items {
		if it.RequestId == c.Result.RequestId && it.Status == "rejected" && *it.RejectReason == "discuss_1on1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rejected copy %+v", list.Items)
	}
	r = press(t, be, "resource:figma-library")
	if r.PendingAction == nil {
		t.Fatalf("again after rejected %+v", r)
	}
	// the mentor's other views
	if mi := decode[api.MentorInternList](t, m.do("GET", "/api/mentor/interns", nil)); mi.Items[0].PendingAccessRequests != 0 {
		t.Fatalf("pending %+v", mi.Items)
	}
	expect(t, be.do("GET", "/api/mentor/access-requests", nil), http.StatusForbidden)
	all := decode[api.RequestList](t, m.do("GET", "/api/mentor/access-requests?status=approved", nil))
	if all.Total != 1 {
		t.Fatalf("approved %d", all.Total)
	}
}

// XA-13 (foreign mentor), XA-18, XA-27
func TestAccessMentorBoundaries(t *testing.T) {
	h := newIntegration(t)
	m := h.login("u-mentor")
	other := h.login("u-mentor-other")
	expect(t, decideReq(other, "ACC-000101", "approve", "", newID()), http.StatusNotFound)
	for _, in := range []map[string]any{{"text": "добавь меня в tech-dep"}, {"payload": "intent:access_request"}, {"payload": "resource:channel-tech-dep"}} {
		rec := sendRaw(m, newID(), in)
		expect(t, rec, http.StatusOK)
		r := decode[api.AgentReply](t, rec)
		if r.PendingAction != nil || !strings.Contains(r.Text, "Входящие") {
			t.Fatalf("XA-18 %+v", r)
		}
	}
	pd := h.events.Events("permission_denied")
	if len(pd) < 3 || pd[len(pd)-1].Payload["tool"] != "route:access_request" {
		t.Fatalf("permission_denied %+v", pd)
	}
	expect(t, m.doWith("POST", "/api/mentor/access-requests/ACC-000101/decision",
		map[string]any{"decision": "approve", "idempotency_key": newID()}, map[string]string{"Origin": "https://evil.example"}), http.StatusForbidden)
	for i := 0; i < 20; i++ {
		m.do("POST", "/api/mentor/access-requests/ACC-000999/decision", map[string]any{"decision": "approve", "idempotency_key": newID()})
	}
	expect(t, decideReq(m, "ACC-000101", "approve", "", newID()), http.StatusTooManyRequests)
}

// XA-20, XA-21, XA-22, A-26: no false drafts; replaced drafts.
func TestAccessNoFalseDrafts(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	r := say(t, be, "что такое tech-dep")
	if r.PendingAction != nil || !hasPayload(r, "resource:channel-tech-dep") {
		t.Fatalf("XA-20 %+v", r)
	}
	if r = say(t, be, "в каком канале спрашивать про деплой"); r.PendingAction != nil || len(payloadsWith(r, "resource:")) != 0 {
		t.Fatalf("XA-20b %+v", r)
	}
	if r = say(t, be, "мне не нужен доступ в tech-dep"); r.Intent == "access_request" || r.PendingAction != nil {
		t.Fatalf("XA-21 %+v", r)
	}
	r = say(t, be, "хочу отгул и доступ в tech-dep") // A-26
	if hasAction(r, api.OpenForm) == nil || !hasPayload(r, "resource:channel-tech-dep") {
		t.Fatalf("A-26 %+v", r)
	}
	a := say(t, be, "добавь меня в tech-dep") // XA-22
	b := say(t, be, "дай доступ в tech-dep")
	other := press(t, be, "step:cm-1.7")
	expect(t, be.do("POST", "/api/actions/"+a.PendingAction.ActionId.String()+"/confirm", nil), http.StatusGone, "ACTION_REPLACED")
	if g := decode[api.ActionView](t, be.do("GET", "/api/actions/"+other.PendingAction.ActionId.String(), nil)); g.State != "active" {
		t.Fatalf("another resource is untouched: %s", g.State)
	}
	confirm(t, be, b)
	// XA-23: cancel → confirm 410; expired → 410; after reset → 404
	c := say(t, be, "дай доступ к фигме")
	expect(t, be.do("POST", "/api/actions/"+c.PendingAction.ActionId.String()+"/cancel", nil), http.StatusOK)
	expect(t, be.do("POST", "/api/actions/"+c.PendingAction.ActionId.String()+"/confirm", nil), http.StatusGone)
	d := say(t, be, "дай доступ к фигме")
	h.clock.Advance(16 * time.Minute)
	expect(t, be.do("POST", "/api/actions/"+d.PendingAction.ActionId.String()+"/confirm", nil), http.StatusGone, "ACTION_EXPIRED")
	e := say(t, be, "дай доступ к фигме")
	expect(t, be.do("POST", "/api/demo/reset", nil), http.StatusNoContent)
	expect(t, be.do("POST", "/api/actions/"+e.PendingAction.ActionId.String()+"/confirm", nil), http.StatusNotFound)
}

func payloadsWith(r api.AgentReply, prefix string) []string {
	var out []string
	for _, p := range payloads(r) {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// XA-14, XA-15, XA-25: the access service fails.
func TestAccessServiceFailures(t *testing.T) {
	h := newHarness(t)
	be := h.login("u-intern-be")
	res := map[string]any{"resource_id": "team-channels", "title": "Ключевые каналы и чаты команды", "kind": "channel",
		"description": "Тестовое описание", "policy": "auto", "effective_policy": "auto", "timer_minutes": nil, "access_level": "write",
		"approver": "Администратор", "allowed": true, "deny_reason_code": nil, "deny_explanation": nil, "aliases": []string{"ключевые каналы"}}
	h.tools.On("access.list_resources", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"items": []any{res}, "total": 1}, nil
	})
	created := map[string]any{"request_id": "ACC-000102", "employee_id": "u-intern-be", "resource_id": "team-channels", "status": "approved",
		"policy": "auto", "access_level": "write", "justification": nil, "created_at": "2026-11-10T09:00:00Z", "auto_approve_at": nil,
		"decided_at": "2026-11-10T09:00:00Z", "decided_by": "auto", "decided_by_user_id": nil, "reject_reason": nil}
	calls := 0
	h.tools.On("access.create_request", func(context.Context, map[string]any) (any, error) {
		calls++
		switch calls {
		case 1:
			return nil, &mcpclient.ToolError{Code: mcpclient.CodeUnavailable, Message: "down", Retryable: true}
		case 2: // XA-25: an answer without request_id violates the contract
			return map[string]any{"request": map[string]any{"status": "approved"}, "idempotent_replay": false}, nil
		}
		return map[string]any{"request": created, "idempotent_replay": false}, nil
	})
	d := decode[api.ActionCreated](t, be.do("POST", "/api/actions", map[string]any{"kind": "access", "params": map[string]any{"resource_id": "team-channels"}}))
	url := "/api/actions/" + d.PendingAction.ActionId.String() + "/confirm"
	expect(t, be.do("POST", url, nil), http.StatusServiceUnavailable, `"retryable":true`)
	expect(t, be.do("POST", url, nil), http.StatusServiceUnavailable)
	var n int
	_ = h.srv.Pool.QueryRow(context.Background(), `SELECT count(*) FROM app.access_requests WHERE request_id = 'ACC-000102'`).Scan(&n)
	if n != 0 {
		t.Fatal("XA-25: nothing in the DB")
	}
	if g := decode[api.ActionView](t, be.do("GET", "/api/actions/"+d.PendingAction.ActionId.String(), nil)); g.State != "active" {
		t.Fatalf("draft must stay active: %s", g.State)
	}
	rec := be.do("POST", url, nil) // XA-14: the retry works
	expect(t, rec, http.StatusOK)
	if len(h.events.Events("action_failed")) != 2 {
		t.Fatalf("action_failed %d", len(h.events.Events("action_failed")))
	}
	// XA-15: the service is down while polling: the path is served, stale_requests
	_, _ = h.srv.Pool.Exec(context.Background(), `UPDATE app.access_requests SET status = 'pending' WHERE request_id = 'ACC-000102'`)
	h.srv.Sync = h.srv.AccessSync.Run
	h.tools.On("checklist.get_path", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: mcpclient.CodeUnavailable, Message: "down"}
	})
	list := decode[api.RequestList](t, be.do("GET", "/api/requests?kind=access", nil))
	if list.Stale == nil || !*list.Stale {
		t.Fatalf("stale %+v", list)
	}
	// the catalog is down
	h.tools.On("access.list_resources", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: mcpclient.CodeUnavailable, Message: "down"}
	})
	expect(t, be.do("GET", "/api/access/resources", nil), http.StatusServiceUnavailable)
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "access", "params": map[string]any{"resource_id": "team-channels"}}), http.StatusServiceUnavailable)
	r := press(t, be, "resource:team-channels")
	if r.MascotState != "oops" {
		t.Fatalf("catalog down %+v", r)
	}
	if r = press(t, be, "intent:access_request"); r.MascotState != "oops" {
		t.Fatalf("catalog down menu %+v", r)
	}
}

func TestAccessResourcesList(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	out := decode[struct {
		Items []api.AccessResourceView `json:"items"`
	}](t, be.do("GET", "/api/access/resources", nil))
	if len(out.Items) != 6 {
		t.Fatalf("allowed resources %d", len(out.Items))
	}
	for _, x := range out.Items {
		if x.ResourceId == "prod-db-write" {
			t.Fatal("denied resources are not listed")
		}
	}
	// flag off → the seeded mentor request is approved at the first read (XA-08)
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "access_mentor_approval", "value": "off"}), http.StatusOK)
	p := decode[api.PathView](t, be.do("GET", "/api/path", nil))
	for _, s := range p.Steps {
		if s.StepId == "cm-1.10" && s.Status != "done" {
			t.Fatalf("XA-08: %s", s.Status)
		}
	}
}
