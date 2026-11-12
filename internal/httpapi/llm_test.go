package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/llm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
)

// stubModel is an OpenAI-compatible stub for the agent: intent answers step 1
// (max_tokens 200), rephrase answers step 2 and gets the draft text.
type stubModel struct {
	mu        sync.Mutex
	calls     int
	code      int
	intent    string
	rephrase  func(draft string) string
	rephrased int
	lastUser  string
}

func (m *stubModel) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var req struct {
		MaxTokens int `json:"max_tokens"`
		Messages  []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(b, &req)
	m.mu.Lock()
	m.calls++
	code, content := m.code, m.intent
	if len(req.Messages) == 2 && req.MaxTokens == 200 {
		m.lastUser = req.Messages[1].Content
	}
	if req.MaxTokens == 300 {
		m.rephrased++
		var in llm.RephraseInput
		_ = json.Unmarshal([]byte(req.Messages[1].Content), &in)
		out, _ := json.Marshal(map[string]string{"text": m.rephrase(in.DraftText)})
		content = string(out)
	}
	m.mu.Unlock()
	if code == 0 {
		code = 200
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
}

func (m *stubModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// withModel switches the harness agent to openai_compatible against the stub.
func withModel(t *testing.T, h *harness, m *stubModel, fl flags.Static) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(srv.Close)
	if m.rephrase == nil {
		m.rephrase = func(d string) string { return d }
	}
	if fl == nil {
		fl = flags.Static{"llm_enabled": "on", "llm_rephrase": "on"}
	}
	cfg := config.LLMConfig{Requested: llm.ProviderOpenAI, Provider: llm.ProviderOpenAI, BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: 2 * time.Second}
	h.srv.Cfg.LLM = cfg
	h.srv.Agent.LLM = llm.New(cfg, fl, h.clock)
}

func lastPayload(h *harness, name, key string) any {
	ev := h.events.Events(name)
	if len(ev) == 0 {
		return nil
	}
	return ev[len(ev)-1].Payload[key]
}

// Model answers step 1: dates go to the form prefill, mode=llm.
func TestLLMIntentPrefill(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"time_off","confidence":0.92,"params":{"date_from":"2026-11-12","date_to":"2026-11-13"}}`}
	withModel(t, h, m, nil)
	be := h.login("u-intern-be")
	r := say(t, be, "хочу взять отгул в четверг и пятницу")
	f := hasAction(r, api.OpenForm)
	if r.Mode != api.AgentReplyModeLlm || r.Intent != "time_off" || f == nil || f.Prefill == nil ||
		f.Prefill.DateFrom.String() != "2026-11-12" || f.Prefill.DateTo.String() != "2026-11-13" {
		t.Fatalf("%+v", r)
	}
	if lastPayload(h, "intent_recognized", "source") != "llm" {
		t.Fatal("source llm")
	}
	if lastPayload(h, "time_off_form_opened", "prefilled") != true {
		t.Fatal("prefilled event")
	}
	if m.rephrased != 1 || !strings.Contains(r.Text, "[1]") {
		t.Fatalf("the form text is rephrased, the marker kept: %q", r.Text)
	}
}

// T-36: the guard topic of the rules wins over the model.
func TestLLMGuardTopicWins(t *testing.T) {
	h := newIntegration(t)
	withModel(t, h, &stubModel{intent: `{"intent":"kb_question","confidence":0.95,"params":{"query":"зарплата"}}`}, nil)
	be := h.login("u-intern-be")
	r := say(t, be, "когда зарплата")
	if r.Intent != "out_of_scope" || r.Mode != api.AgentReplyModeLlm {
		t.Fatalf("%+v", r)
	}
	if lastPayload(h, "intent_recognized", "override") != "llm" {
		t.Fatal("override llm")
	}
}

// Low confidence keeps the rules; invalid JSON (§14.1-3) too, with an event.
func TestLLMLowConfidenceAndInvalid(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"kb_question","confidence":0.3,"params":{}}`}
	withModel(t, h, m, flags.Static{"llm_enabled": "on", "llm_rephrase": "off"})
	be := h.login("u-intern-be")
	r := say(t, be, "хочу отгул")
	if r.Intent != "time_off" || r.Mode != api.AgentReplyModeLlm || lastPayload(h, "llm_fallback", "reason") != "low_confidence" {
		t.Fatalf("%+v", r)
	}
	m.mu.Lock()
	m.intent = `{"intent":"dance"}`
	m.mu.Unlock()
	r = say(t, be, "хочу отгул")
	if r.Intent != "time_off" || r.Mode != api.AgentReplyModeFallback || !hasNotice(r, "simplified_mode") ||
		lastPayload(h, "llm_fallback", "reason") != "invalid_json" || hasAction(r, api.OpenForm) == nil {
		t.Fatalf("%+v", r)
	}
}

// Valid model params beyond the rules: step_ref and query.
func TestLLMStepRefAndOutOfScope(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"step_explain","confidence":0.9,"params":{"step_ref":"1.13"}}`}
	withModel(t, h, m, flags.Static{"llm_enabled": "on", "llm_rephrase": "off"})
	be := h.login("u-intern-be")
	r := say(t, be, "расскажи про настройку рабочего окружения")
	if r.Intent != "step_explain" || r.Mode != api.AgentReplyModeLlm {
		t.Fatalf("%+v", r)
	}
	m.mu.Lock()
	m.intent = `{"intent":"out_of_scope","confidence":0.9,"params":{"topic":"business_trip"}}`
	m.mu.Unlock()
	r = say(t, be, "поеду ли я в другой город по работе")
	if r.Intent != "out_of_scope" {
		t.Fatalf("%+v", r)
	}
	m.mu.Lock()
	m.intent = `{"intent":"access_request","confidence":0.9,"params":{"resource_hint":"гитлаб"}}`
	m.mu.Unlock()
	r = say(t, be, "мне нужен этот репозиторный сервис")
	if r.Intent != "access_request" {
		t.Fatalf("%+v", r)
	}
}

// T-37: a rejected key → rules, llm_auth_error; the next message skips the model.
func TestLLMAuthError(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{code: 401}
	withModel(t, h, m, nil)
	be := h.login("u-intern-be")
	r := say(t, be, "хочу отгул")
	if r.Mode != api.AgentReplyModeFallback || !hasNotice(r, "llm_auth_error") || hasAction(r, api.OpenForm) == nil {
		t.Fatalf("%+v", r)
	}
	n := m.count()
	r = say(t, be, "что дальше?")
	if m.count() != n || !hasNotice(r, "llm_auth_error") || r.Intent != "my_path" {
		t.Fatalf("breaker must skip the model: %d→%d %+v", n, m.count(), r)
	}
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"status":"auth_error"`)
}

// T-38: LLM_PROVIDER set without a key → misconfigured, rules answer.
func TestLLMMisconfigured(t *testing.T) {
	h := newIntegration(t)
	cfg := config.LLMConfig{Requested: llm.ProviderOpenAI, Provider: "demo", Misconfigured: "LLM_API_KEY"}
	h.srv.Cfg.LLM = cfg
	h.srv.Agent.LLM = llm.New(cfg, flags.Static{"llm_enabled": "on"}, h.clock)
	be := h.login("u-intern-be")
	r := say(t, be, "что дальше?")
	if r.Intent != "my_path" || r.Mode != api.AgentReplyModeFallback || !hasNotice(r, "llm_misconfigured") {
		t.Fatalf("%+v", r)
	}
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"status":"misconfigured"`, "missing_LLM_API_KEY")
}

// §14.1-2: an unreachable base URL → simplified mode, the scenario still works.
func TestLLMUnreachable(t *testing.T) {
	h := newIntegration(t)
	cfg := config.LLMConfig{Requested: llm.ProviderOpenAI, Provider: llm.ProviderOpenAI, BaseURL: "http://127.0.0.1:1", APIKey: "k", Model: "m", Timeout: time.Second}
	h.srv.Cfg.LLM = cfg
	h.srv.Agent.LLM = llm.New(cfg, flags.Static{"llm_enabled": "on", "llm_rephrase": "on"}, h.clock)
	be := h.login("u-intern-be")
	r := say(t, be, "хочу отгул")
	if r.Mode != api.AgentReplyModeFallback || !hasNotice(r, "simplified_mode") || hasAction(r, api.OpenForm) == nil {
		t.Fatalf("%+v", r)
	}
	if lastPayload(h, "llm_fallback", "reason") != "http_error" {
		t.Fatal("event")
	}
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"status":"degraded"`)
}

// Kill switch llm_enabled=off: like demo, no model call.
func TestLLMKillSwitch(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"help","confidence":0.9,"params":{}}`}
	withModel(t, h, m, flags.Static{"llm_enabled": "off"})
	be := h.login("u-intern-be")
	r := say(t, be, "что дальше?")
	if m.count() != 0 || r.Mode != api.AgentReplyModeDemo {
		t.Fatalf("%d %+v", m.count(), r)
	}
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"status":"disabled"`)
}

// Step 2: an accepted rephrase replaces the text; a rejected one keeps the
// template (§14.1-10); a failed one switches to fallback.
func TestLLMRephrase(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"my_path","confidence":0.9,"params":{}}`}
	m.rephrase = func(d string) string { return "Коротко: " + d }
	withModel(t, h, m, nil)
	be := h.login("u-intern-be")
	tmpl := say(t, be, "что дальше?") // short enough for 400 chars?
	if tmpl.Mode != api.AgentReplyModeLlm {
		t.Fatalf("%+v", tmpl)
	}
	accepted := strings.HasPrefix(tmpl.Text, "Коротко: ")
	if accepted == (lastPayload(h, "llm_rephrase_rejected", "rule") != nil) {
		t.Fatalf("either accepted or rejected: %q", tmpl.Text)
	}
	if accepted && lastPayload(h, "reply_sent", "rephrased") != true {
		t.Fatal("reply_sent rephrased")
	}

	m.mu.Lock()
	m.intent = `{"intent":"help","confidence":0.9,"params":{}}`
	m.rephrase = func(string) string { return "Твоя заявка создана" }
	m.mu.Unlock()
	r := say(t, be, "что ты умеешь")
	if strings.Contains(r.Text, "Твоя заявка создана") || lastPayload(h, "llm_rephrase_rejected", "rule") != llm.RuleCreated {
		t.Fatalf("guard must reject: %q", r.Text)
	}

	m.mu.Lock()
	m.rephrase = func(string) string { return "" }
	m.mu.Unlock()
	r = say(t, be, "что ты умеешь")
	if r.Mode != api.AgentReplyModeLlm || lastPayload(h, "llm_rephrase_rejected", "rule") != llm.RuleLength {
		t.Fatalf("%+v", r)
	}
}

func TestLLMHealthOK(t *testing.T) {
	h := newIntegration(t)
	withModel(t, h, &stubModel{intent: "ok"}, nil)
	rec := h.anon().do("GET", "/api/llm/health", nil)
	expect(t, rec, http.StatusOK, `"status":"ok"`, `"provider":"openai_compatible"`, `"model":"m"`)
	if strings.Contains(rec.Body.String(), `"k"`) {
		t.Fatal("key leaked")
	}
}

// §14.1-17: the guard topic wins; §14.1-11: the model gets masked text.
func TestLLMSickLeaveAndPII(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"time_off","confidence":0.95,"params":{"date_from":"2026-11-11"}}`}
	withModel(t, h, m, nil)
	be := h.login("u-intern-be")
	r := say(t, be, "у меня температура, хочу больничный")
	if r.Intent != "out_of_scope" || hasAction(r, api.OpenForm) != nil {
		t.Fatalf("%+v", r)
	}
	r = say(t, be, "хочу отгул, карта 4111 1111 1111 1111")
	m.mu.Lock()
	user := m.lastUser
	m.mu.Unlock()
	if strings.Contains(user, "4111") || !strings.Contains(user, "[скрыто]") || !hasNotice(r, "pii_masked") {
		t.Fatalf("model prompt: %s / %+v", user, r.Notices)
	}
}

// §14.1-10: a new date or «регламент Авито» → the template; §14.1-14: 10 min.
func TestLLMGuardRejectsAndAuthWindow(t *testing.T) {
	h := newIntegration(t)
	m := &stubModel{intent: `{"intent":"help","confidence":0.9,"params":{}}`}
	m.rephrase = func(string) string { return "По регламенту Авито всё просто" }
	withModel(t, h, m, nil)
	be := h.login("u-intern-be")
	tmpl := say(t, be, "что ты умеешь")
	if strings.Contains(tmpl.Text, "регламенту") || lastPayload(h, "llm_rephrase_rejected", "rule") != llm.RuleForbidden {
		t.Fatalf("%q", tmpl.Text)
	}
	m.mu.Lock()
	m.rephrase = func(string) string { return "Жду тебя 25.12" }
	m.mu.Unlock()
	say(t, be, "что ты умеешь")
	if lastPayload(h, "llm_rephrase_rejected", "rule") != llm.RuleNumbers {
		t.Fatal("new date")
	}
	m.mu.Lock()
	m.code = 401
	m.mu.Unlock()
	say(t, be, "хочу отгул")
	n := m.count()
	h.clock.Advance(9 * time.Minute)
	say(t, be, "хочу отгул")
	if m.count() != n {
		t.Fatal("no call within 10 minutes")
	}
	h.clock.Advance(2 * time.Minute)
	say(t, be, "хочу отгул")
	if m.count() != n+1 {
		t.Fatal("the model is retried after 10 minutes")
	}
}
