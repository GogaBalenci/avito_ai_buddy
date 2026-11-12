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

func hasNotice(r api.AgentReply, n string) bool {
	for _, x := range r.Notices {
		if string(x) == n {
			return true
		}
	}
	return false
}

func feedback(r api.AgentReply) int {
	n := 0
	for _, a := range r.Actions {
		if a.Type == api.Feedback {
			n++
		}
	}
	return n
}

// KA-01, KA-02, KA-04, KA-16, KA-17, KA-21 (T-15…T-18, T-27, T-28, AC-K01, AC-K22)
func TestKnowledgeAnswers(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	start := time.Now()
	r := say(t, be, "сколько ждать код-ревью?") // T-15, AC-K01: the week-1 boost lifts the agreement
	if time.Since(start) > 300*time.Millisecond {
		t.Errorf("KA-01: %v", time.Since(start))
	}
	if r.Intent != "kb_question" || r.Sources[0].Slug != "neighborhood-agreement" || !strings.Contains(r.Text, "не позже следующего дня") ||
		!strings.Contains(r.Text, "[1]") || r.Sources[0].Dated == nil {
		t.Fatalf("T-15 %+v", r)
	}
	r = say(t, be, "какой срок код-ревью в команде") // KA-21, AC-K22
	if r.Intent != "kb_question" || len(r.Sources) != 3 || r.Sources[1].Slug != "code-review-team" || r.Sources[2].Slug != "code-review-general" ||
		!strings.Contains(r.Text, "[2] и «Код-ревью: общая") || !strings.Contains(r.Text, "Инструкции расходятся") ||
		!hasNotice(r, "doc_conflict") || r.MascotState != "ask_mentor" || feedback(r) != 2 {
		t.Fatalf("KA-21 %+v", r)
	}
	cp := hasAction(r, api.Copy)
	if cp == nil || cp.CopyEvent == nil || *cp.CopyEvent != "doc_conflict_reported" || !strings.Contains(*cp.Text, "«Код-ревью: общая") {
		t.Fatalf("KA-22 copy %+v", cp)
	}
	// KA-22: reported once per reply
	for i := 0; i < 2; i++ {
		expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "doc_conflict_reported", "reply_id": r.ReplyId.String()}), http.StatusNoContent)
	}
	if ev := h.events.Events("doc_conflict_reported"); len(ev) != 1 {
		t.Fatalf("KA-22 events %d", len(ev))
	}
	r = say(t, be, "что такое zero bug policy") // T-16
	if r.Intent != "kb_question" || r.Sources[0].Slug != "zero-bug-policy" || r.Sources[0].SourceLevel != "playbook" || r.Sources[0].Label != "test_instruction" ||
		r.MascotState != "hello" || feedback(r) != 2 || r.Escalation != nil {
		t.Fatalf("T-16 %+v", r)
	}
	r = say(t, be, "что такое stash") // T-17, KA-02
	if r.Sources[0].Label != "test_description" || !strings.Contains(r.Text, "тестовое описание") || !strings.Contains(r.Text, "Уточни у наставника") ||
		r.Escalation == nil || r.Escalation.Reason != "no_source_term" {
		t.Fatalf("T-17 %+v", r)
	}
	r = say(t, be, "как устроен квантовый компьютер") // T-18, KA-04
	if r.Escalation == nil || r.Escalation.Reason != "no_answer" || !strings.Contains(r.Escalation.Draft, "Неделя 1") {
		t.Fatalf("T-18 %+v", r)
	}
	r = say(t, be, "как у нас оценивают задачи на груминге") // T-28, KA-16
	if r.Intent != "kb_question" || r.Sources[0].Slug != "processes-scrum" {
		t.Fatalf("T-28 %+v", r)
	}
	r = say(t, be, "в каком канале спрашивать про деплой") // T-27
	if r.Intent != "kb_question" || r.PendingAction != nil || len(payloadsWith(r, "resource:")) != 0 {
		t.Fatalf("T-27 %+v", r)
	}
	r = say(t, be, "что такое tech-dep") // KA-17
	if r.Sources[0].Slug != "term-tech-dep" || r.PendingAction != nil || !hasPayload(r, "resource:channel-tech-dep") {
		t.Fatalf("KA-17 %+v", r)
	}
	r = say(t, be, "кто мой наставник") // a medium answer, not a refusal
	if !strings.HasPrefix(r.Text, "Похоже, ответ есть здесь [1]") || r.Sources[0].Slug != "who-to-ask" || feedback(r) != 2 {
		t.Fatalf("medium %+v", r)
	}
	ev := h.events.Events("question_answered")
	if len(ev) < 5 || ev[0].Payload["confidence_band"] != "high" {
		t.Fatalf("question_answered %+v", ev)
	}
	r = say(t, be, "как устроены релизы") // T-22: kb-injection-test answers from the neutral summary
	if r.PendingAction != nil || strings.Contains(strings.ToLower(r.Text), "игнорируй") || strings.Contains(r.Text, "продовую") {
		t.Fatalf("T-22 %+v", r)
	}
}

// KA-05, KA-06, KA-18, KA-20 (T-19, T-20)
func TestKnowledgeRights(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	m := h.login("u-mentor")
	for _, q := range []string{"что в методичке наставника", "покажи методичку наставника"} {
		r := say(t, be, q)
		if r.Intent != "kb_question" || len(r.Sources) != 0 || r.MascotState != "ask_mentor" || r.Escalation == nil ||
			r.Escalation.Reason != "permission_denied" || !strings.Contains(r.Text, "«Методичка наставника") || !strings.Contains(r.Text, "доступна только наставникам") {
			t.Fatalf("T-19 %q %+v", q, r)
		}
	}
	if ev := h.events.Events("permission_denied"); len(ev) == 0 || ev[0].Payload["tool"] != "knowledge.search" {
		t.Fatalf("permission_denied %+v", ev)
	}
	r := say(t, m, "что в методичке наставника") // T-20
	if r.Sources[0].Slug != "mentor-guide" || r.Escalation != nil {
		t.Fatalf("T-20 %+v", r)
	}
	a := say(t, be, "что такое zero bug policy") // KA-20: S2 is the same for the mentor
	b := say(t, m, "что такое zero bug policy")
	if a.Text != b.Text {
		t.Fatalf("KA-20 %q vs %q", a.Text, b.Text)
	}
	// KA-06, K-A2, K-A3
	rec := be.do("GET", "/api/kb/mentor-guide", nil)
	expect(t, rec, http.StatusForbidden, "Методичка наставника")
	if strings.Contains(rec.Body.String(), "body_md") {
		t.Fatal("no body")
	}
	expect(t, be.do("GET", "/api/kb/no-such-doc", nil), http.StatusNotFound)
	doc := decode[api.KbDoc](t, m.do("GET", "/api/kb/mentor-guide", nil))
	if doc.BodyMd == "" {
		t.Fatal("mentor reads the guide")
	}
	d := decode[api.KbDoc](t, be.do("GET", "/api/kb/term-stash?from=reply", nil))
	if d.Label != "test_description" || d.Url != "/kb/term-stash" || d.ConflictsWith == nil {
		t.Fatalf("doc %+v", d)
	}
	if ev := h.events.Events("kb_doc_opened"); len(ev) != 2 || ev[1].Payload["from"] != "reply" {
		t.Fatalf("kb_doc_opened %+v", ev)
	}
	expect(t, h.anon().do("GET", "/api/kb/term-stash", nil), http.StatusUnauthorized)
}

// KA-08, KA-13: feedback; two «Не то» in a row → escalation.
func TestKnowledgeFeedback(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	fe := h.login("u-intern-fe")
	a := say(t, be, "что такое zero bug policy")
	b := say(t, be, "как у нас оценивают задачи на груминге")
	send := func(c *client, id api.AgentReply, v string) *httptest.ResponseRecorder {
		return c.do("POST", "/api/feedback", map[string]any{"reply_id": id.ReplyId.String(), "value": v})
	}
	expect(t, send(fe, a, "up"), http.StatusNotFound) // KA-13
	rec := send(be, a, "down")
	expect(t, rec, http.StatusNoContent)
	if rec.Header().Get(HeaderEscalation) != "" {
		t.Fatal("one «Не то» is not an escalation")
	}
	expect(t, send(be, a, "down"), http.StatusNoContent) // the same reply again: no new count
	rec = send(be, b, "down")
	if rec.Header().Get(HeaderEscalation) != "negative_feedback" {
		t.Fatalf("KA-08: %v", rec.Header())
	}
	var ev int
	for _, e := range h.events.Events("escalated") {
		if e.Payload["reason"] == "negative_feedback" {
			ev++
		}
	}
	if ev != 1 || len(h.events.Events("feedback")) != 2 {
		t.Fatalf("events escalated %d feedback %d", ev, len(h.events.Events("feedback")))
	}
	expect(t, send(be, a, "up"), http.StatusNoContent)
	var v string
	_ = h.srv.Pool.QueryRow(context.Background(), `SELECT value FROM app.feedback WHERE reply_id = $1`, a.ReplyId).Scan(&v)
	if v != "up" {
		t.Fatalf("overwritten value %s", v)
	}
	expect(t, be.do("POST", "/api/feedback", map[string]any{"reply_id": a.ReplyId.String(), "value": "meh"}), http.StatusBadRequest)
	// client events
	s := say(t, be, "что такое stash")
	expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "escalation_copied", "reply_id": s.ReplyId.String()}), http.StatusNoContent)
	if ev := h.events.Events("escalation_copied"); len(ev) != 1 || ev[0].Payload["reason"] != "no_source_term" {
		t.Fatalf("escalation_copied %+v", ev)
	}
	expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "escalation_copied"}), http.StatusBadRequest)
	expect(t, fe.do("POST", "/api/client-events", map[string]any{"name": "escalation_copied", "reply_id": s.ReplyId.String()}), http.StatusNotFound)
	expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "next_step_clicked", "step_id": "cm-1.7"}), http.StatusNoContent)
	expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "next_step_clicked"}), http.StatusBadRequest)
	expect(t, be.do("POST", "/api/client-events", map[string]any{"name": "other"}), http.StatusBadRequest)
	if len(h.events.Events("next_step_clicked")) != 1 {
		t.Fatal("next_step_clicked")
	}
}

// KA-09, KA-12, KA-14, KA-19: rules of 11 §2.6.4 on fake search results.
func TestKnowledgeRuleOrder(t *testing.T) {
	h := newHarness(t)
	be := h.login("u-intern-be")
	item := func(slug string, rank float64, level string, conflicts ...string) map[string]any {
		label := "test_instruction"
		if level == "none" {
			label = "test_description"
		}
		if conflicts == nil {
			conflicts = []string{}
		}
		return map[string]any{"slug": slug, "title": "Документ " + slug, "kind": "process", "summary": "Кратко про " + slug + ".",
			"snippet": "…", "rank": rank, "label": label, "source_level": level, "dated": nil, "url": "/kb/" + slug, "conflicts_with": conflicts}
	}
	var result map[string]any
	h.tools.On("knowledge.search", func(context.Context, map[string]any) (any, error) { return result, nil })
	denied := []any{map[string]any{"slug": "mentor-guide", "title": "Методичка наставника"}}
	cases := []struct {
		name   string
		items  []any
		denied []any
		check  func(api.AgentReply) bool
	}{
		{"1: denied, top < high", []any{item("doc-a", 0.5, "team")}, denied, func(r api.AgentReply) bool {
			return r.Escalation != nil && r.Escalation.Reason == "permission_denied" && len(r.Sources) == 0
		}},
		{"1: denied, no items", []any{}, denied, func(r api.AgentReply) bool {
			return r.Escalation != nil && r.Escalation.Reason == "permission_denied"
		}},
		{"3 not 1: denied, top ≥ high (KA-12)", []any{item("doc-a", 0.8, "team")}, denied, func(r api.AgentReply) bool {
			return r.Escalation == nil && hasNotice(r, "restricted_doc_exists") && strings.Contains(r.Text, "доступен только наставникам") && r.Sources[0].Slug == "doc-a"
		}},
		{"2: term ≥ low", []any{item("doc-t", 0.35, "none")}, nil, func(r api.AgentReply) bool {
			return r.Escalation != nil && r.Escalation.Reason == "no_source_term" && r.Sources[0].Label == "test_description"
		}},
		{"5 not 2: term < low", []any{item("doc-t", 0.2, "none")}, nil, func(r api.AgentReply) bool {
			return r.Escalation != nil && r.Escalation.Reason == "no_answer"
		}},
		{"3: high, two sources", []any{item("doc-a", 0.9, "team"), item("doc-b", 0.7, "playbook")}, nil, func(r api.AgentReply) bool {
			return strings.HasSuffix(r.Text, "[1]") && len(r.Sources) == 2 && feedback(r) == 2 && r.MascotState == "hello"
		}},
		{"4: medium", []any{item("doc-a", 0.4, "team")}, nil, func(r api.AgentReply) bool {
			return strings.HasPrefix(r.Text, "Похоже, ответ есть здесь [1]") && len(r.Sources) == 1
		}},
		{"5: nothing, 3+ tokens", []any{}, nil, func(r api.AgentReply) bool {
			return r.Escalation != nil && r.Escalation.Reason == "no_answer"
		}},
		{"4 with a conflict to a missing doc", []any{item("doc-a", 0.4, "team", "zz-missing")}, nil, func(r api.AgentReply) bool {
			return !hasNotice(r, "doc_conflict") && len(r.Sources) == 1
		}},
	}
	for _, c := range cases {
		d := c.denied
		if d == nil {
			d = []any{}
		}
		result = map[string]any{"items": c.items, "denied": d, "query_lexemes": []string{"x"}}
		r := say(t, be, "как устроен процесс сборки проекта")
		if !c.check(r) {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
	// 6: 1–2 content tokens and nothing found → clarification, not «не нашёл»
	result = map[string]any{"items": []any{}, "denied": []any{}, "query_lexemes": []string{"абв"}}
	if r := say(t, be, "абв где"); r.Intent != "unknown" || r.Escalation != nil {
		t.Fatalf("6: %+v", r)
	}
	// KA-15: no content tokens → no search call at all
	calls := 0
	h.tools.On("knowledge.search", func(context.Context, map[string]any) (any, error) { calls++; return result, nil })
	say(t, be, "а как?")
	if calls != 0 {
		t.Fatal("KA-15: search must not be called")
	}
	// KA-19: a repeat writes no second question_answered
	result = map[string]any{"items": []any{item("doc-a", 0.9, "team")}, "denied": []any{}, "query_lexemes": []string{"x"}}
	id := newID()
	n := len(h.events.Events("question_answered"))
	sendRaw(be, id, map[string]any{"text": "как устроен процесс сборки проекта"})
	sendRaw(be, id, map[string]any{"text": "как устроен процесс сборки проекта"})
	if len(h.events.Events("question_answered")) != n+1 {
		t.Fatal("KA-19")
	}
	// KA-09: the knowledge service is down
	h.tools.On("knowledge.search", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: mcpclient.CodeUnavailable, Message: "down"}
	})
	r := say(t, be, "как устроен процесс сборки проекта")
	if r.MascotState != "oops" || hasAction(r, api.Retry) == nil || len(r.Sources) != 0 {
		t.Fatalf("KA-09 %+v", r)
	}
	expect(t, be.do("GET", "/api/kb/term-stash", nil), http.StatusServiceUnavailable)
	// KA-11: the masked text is searched
	var seen string
	h.tools.On("knowledge.search", func(_ context.Context, in map[string]any) (any, error) {
		seen, _ = in["query"].(string)
		return result, nil
	})
	r = say(t, be, "как сборка проекта, мой телефон +7 912 345-67-89")
	if strings.Contains(seen, "912") || !hasNotice(r, "pii_masked") {
		t.Fatalf("KA-11 %q %+v", seen, r)
	}
}
