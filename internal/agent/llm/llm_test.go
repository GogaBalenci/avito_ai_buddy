package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

// fakeModel is an OpenAI-compatible stub: answer(req) returns status and content.
type fakeModel struct {
	mu     sync.Mutex
	reqs   []map[string]any
	answer func(req map[string]any) (int, string)
}

func (f *fakeModel) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(b, &req)
		req["_auth"] = r.Header.Get("Authorization")
		req["_x"] = r.Header.Get("X-Extra")
		req["_path"] = r.URL.Path
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		f.mu.Unlock()
		code, content := f.answer(req)
		w.WriteHeader(code)
		if code == http.StatusOK && content != "<raw>" {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
		} else {
			_, _ = w.Write([]byte("not json"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeModel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeModel) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func client(url string) *Client {
	return &Client{BaseURL: url, APIKey: "k", Model: "m", Headers: map[string]string{"X-Extra": "v"}, Timeout: time.Second}
}

var ctx0 = IntentContext{Today: "2026-11-10", Weekday: "вторник", Role: "intern", Track: "backend",
	PeriodTitle: "Неделя 1", Steps: []Step{{Ref: "1.13", Title: "Настрой окружение"}},
	Resources: []Resource{{ID: "gitlab", Title: "GitLab"}}, LastBotQuestion: "Отгул / Мой путь", MaskedText: "хочу отгул завтра"}

func TestIntentSuccessAndRequest(t *testing.T) {
	f := &fakeModel{answer: func(map[string]any) (int, string) {
		return 200, "```json\n{\"intent\":\"time_off\",\"confidence\":0.9,\"params\":{\"date_from\":\"2026-11-11\"}}\n```"
	}}
	srv := f.start(t)
	r, err := client(srv.URL).Intent(context.Background(), ctx0)
	if err != nil || r.Intent != "time_off" || r.Params.DateFrom != "2026-11-11" || r.Confidence != 0.9 {
		t.Fatalf("%+v %v", r, err)
	}
	req := f.last()
	if req["_auth"] != "Bearer k" || req["_x"] != "v" || req["_path"] != "/chat/completions" || req["model"] != "m" {
		t.Fatalf("headers %+v", req)
	}
	if req["temperature"].(float64) != 0 || req["max_tokens"].(float64) != 200 {
		t.Fatalf("params %+v", req)
	}
	rf := req["response_format"].(map[string]any)
	if rf["type"] != "json_schema" {
		t.Fatalf("format %+v", rf)
	}
	user := req["messages"].([]any)[1].(map[string]any)["content"].(string)
	for _, want := range []string{"2026-11-10", "вторник", "backend", "1.13", "gitlab", "хочу отгул завтра", "Отгул / Мой путь"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt lacks %q:\n%s", want, user)
		}
	}
}

func TestIntentSchemaFallbackToJSONObject(t *testing.T) {
	f := &fakeModel{}
	f.answer = func(req map[string]any) (int, string) {
		if req["response_format"].(map[string]any)["type"] == "json_schema" {
			return 400, ""
		}
		return 200, `{"intent":"my_path","confidence":0.8,"params":{}}`
	}
	srv := f.start(t)
	c := client(srv.URL)
	if r, err := c.Intent(context.Background(), ctx0); err != nil || r.Intent != "my_path" {
		t.Fatalf("%+v %v", r, err)
	}
	sys := f.last()["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "JSON Schema") {
		t.Fatal("schema not put into the prompt")
	}
	n := f.count()
	if _, err := c.Intent(context.Background(), ctx0); err != nil || f.count() != n+1 {
		t.Fatal("json_object must be remembered")
	}
}

func TestIntentErrors(t *testing.T) {
	cases := []struct {
		code    int
		content string
		reason  string
	}{
		{401, "", ReasonAuth},
		{403, "", ReasonAuth},
		{500, "", ReasonHTTP},
		{200, "<raw>", ReasonInvalidJSON},
		{200, "not json", ReasonInvalidJSON},
		{200, `{"intent":"dance","confidence":0.9,"params":{}}`, ReasonInvalidJSON},
		{200, `{"intent":"help","confidence":2,"params":{}}`, ReasonInvalidJSON},
		{200, `{"intent":"help","confidence":0.9,"params":{"x":1}}`, ReasonInvalidJSON},
	}
	for _, tc := range cases {
		f := &fakeModel{answer: func(map[string]any) (int, string) { return tc.code, tc.content }}
		srv := f.start(t)
		_, err := client(srv.URL).Intent(context.Background(), ctx0)
		if ReasonOf(err) != tc.reason {
			t.Errorf("%d %q: %v, want %s", tc.code, tc.content, err, tc.reason)
		}
	}
	// unreachable base URL (§14.1-2)
	if _, err := client("http://127.0.0.1:1").Intent(context.Background(), ctx0); ReasonOf(err) != ReasonHTTP {
		t.Fatalf("unreachable: %v", err)
	}
	if ReasonOf(errors.New("x")) != ReasonHTTP || (&Error{Reason: "a", Status: 1}).Error() == "" {
		t.Fatal("ReasonOf")
	}
}

func TestIntentTimeout(t *testing.T) {
	f := &fakeModel{answer: func(map[string]any) (int, string) {
		time.Sleep(300 * time.Millisecond)
		return 200, "{}"
	}}
	srv := f.start(t)
	c := client(srv.URL)
	c.Timeout = 50 * time.Millisecond
	if _, err := c.Intent(context.Background(), ctx0); ReasonOf(err) != ReasonTimeout {
		t.Fatalf("timeout: %v", err)
	}
}

func TestRephrase(t *testing.T) {
	var content atomic.Value
	content.Store(`{"text":"Готово [1]"}`)
	f := &fakeModel{answer: func(map[string]any) (int, string) { return 200, content.Load().(string) }}
	srv := f.start(t)
	c := client(srv.URL)
	in := RephraseInput{Intent: "kb_question", DraftText: "Ответ [1]", Sources: []Source{{N: 1, Title: "Док"}}, MaxChars: 400}
	if s, err := c.Rephrase(context.Background(), in); err != nil || s != "Готово [1]" {
		t.Fatalf("%q %v", s, err)
	}
	if req := f.last(); req["temperature"].(float64) != 0.3 || req["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("%+v", req)
	}
	for _, bad := range []string{`{"text":1}`, `{"text":"a","x":1}`, `nope`} {
		content.Store(bad)
		if _, err := c.Rephrase(context.Background(), in); ReasonOf(err) != ReasonInvalidJSON {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if _, err := client("http://127.0.0.1:1").Rephrase(context.Background(), in); err == nil {
		t.Fatal("unreachable")
	}
}

func TestBreaker(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	b := &Breaker{Now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		b.Failure(ReasonTimeout)
	}
	if !b.Allow() {
		t.Fatal("2 failures must not open")
	}
	b.Success()
	for i := 0; i < 3; i++ {
		b.Failure(ReasonTimeout)
	}
	if open, auth, last := b.State(); b.Allow() || !open || auth || last != ReasonTimeout {
		t.Fatal("3 failures open for 60 s")
	}
	now = now.Add(61 * time.Second)
	if !b.Allow() {
		t.Fatal("half-open after 60 s")
	}
	b.Failure(ReasonAuth)
	now = now.Add(9 * time.Minute)
	if _, auth, _ := b.State(); b.Allow() || !auth {
		t.Fatal("auth opens for 10 min")
	}
	now = now.Add(2 * time.Minute)
	if !b.Allow() {
		t.Fatal("closed after 10 min")
	}
	var real Breaker
	if !real.Allow() {
		t.Fatal("zero breaker allows")
	}
}

func TestGuard(t *testing.T) {
	base := RephraseInput{DraftText: "Шаг 1.13 до 12.11, заявка ACC-000101 [1]", Facts: []string{"7 дней"},
		Sources: []Source{{N: 1, Title: "Док"}}, MustKeep: []string{"ACC-000101", "[1]"}, MaxChars: 80}
	cases := map[string]int{
		"Шаг 1.13: заявка ACC-000101 ждёт решения до 12.11 [1]": 0,
		"":                      RuleLength,
		strings.Repeat("а", 81): RuleLength,
		"Заявка ждёт решения [1]":                     RuleMustKeep,
		"Заявка ACC-000101 ждёт [1] [2]":              RuleMarkers,
		"Заявка ACC-000101 ждёт [1], осталось 9 дней": RuleNumbers,
		"Заявка ACC-000101 [1] см. https://x.ru":      RuleMarkup,
		"Заявка ACC-000101 <b>ждёт</b> [1]":           RuleMarkup,
		"# Заявка ACC-000101 [1]":                     RuleMarkup,
		"Ты обязана ждать ACC-000101 [1]":             RuleForbidden,
		"Это Официально: ACC-000101 [1]":              RuleForbidden,
		"Так по правилам компании ACC-000101 [1]":     RuleForbidden,
		"Я гарантирую ACC-000101 [1]":                 RuleForbidden,
		"Твоя заявка уже создана ACC-000101 [1]":      RuleCreated,
		"Отправлена заявка ACC-000101 [1]":            RuleCreated,
	}
	for text, want := range cases {
		if got := Check(text, base); got != want {
			t.Errorf("%q: rule %d, want %d", text, got, want)
		}
	}
	noSrc := RephraseInput{DraftText: "Заявка создана", ActionState: "created"}
	if Check("Заявка создана, жди", noSrc) != 0 {
		t.Fatal("created state allows the phrase; default max 400")
	}
	if Check("Без маркера", RephraseInput{Sources: []Source{{N: 1}}}) != RuleMarkers {
		t.Fatal("sources require a marker")
	}
}

func TestEngine(t *testing.T) {
	clk := testkit.NewClock(testkit.Today)
	var disabled *Engine
	if disabled.Requested() || disabled.Misconfigured() != "" {
		t.Fatal("nil engine is demo")
	}
	demo := New(config.LLMConfig{Requested: "demo"}, nil, clk)
	if st, _ := demo.Status(context.Background()); st != StatusDisabled || demo.Enabled(context.Background()) {
		t.Fatal("demo disabled")
	}
	mis := New(config.LLMConfig{Requested: ProviderOpenAI, Misconfigured: "LLM_API_KEY"}, nil, clk)
	if st, code := mis.Status(context.Background()); st != StatusMisconfigured || code != "missing_LLM_API_KEY" || mis.Misconfigured() != "LLM_API_KEY" {
		t.Fatal(st, code)
	}

	var code atomic.Int32
	code.Store(200)
	f := &fakeModel{answer: func(map[string]any) (int, string) {
		return int(code.Load()), `{"intent":"help","confidence":0.9,"params":{}}`
	}}
	srv := f.start(t)
	fl := flags.Static{"llm_enabled": "on", "llm_rephrase": "off"}
	e := New(config.LLMConfig{Requested: ProviderOpenAI, Provider: ProviderOpenAI, BaseURL: srv.URL, APIKey: "k", Model: "m", Timeout: time.Second}, fl, clk)
	ctx := context.Background()
	if !e.Enabled(ctx) || e.RephraseOn(ctx) {
		t.Fatal("flags")
	}
	if st, _ := e.Status(ctx); st != StatusOK {
		t.Fatal("ok", st)
	}
	n := f.count()
	if st, _ := e.Status(ctx); st != StatusOK || f.count() != n {
		t.Fatal("probe is cached for 30 s")
	}
	if _, err := e.Intent(ctx, ctx0); err != nil {
		t.Fatal(err)
	}
	code.Store(500)
	clk.Advance(31 * time.Second)
	if st, c := e.Status(ctx); st != StatusDegraded || c != ReasonHTTP {
		t.Fatal("degraded", st, c)
	}
	for i := 0; i < 3; i++ {
		_, _ = e.Intent(ctx, ctx0)
	}
	if st, _ := e.Status(ctx); st != StatusCircuitOpen {
		t.Fatal("circuit", st)
	}
	if _, err := e.Intent(ctx, ctx0); ReasonOf(err) != ReasonCircuitOpen {
		t.Fatal("open breaker skips the call")
	}
	if _, err := e.Rephrase(ctx, RephraseInput{}); ReasonOf(err) != ReasonCircuitOpen {
		t.Fatal("open breaker skips rephrase")
	}
	clk.Advance(61 * time.Second)
	code.Store(401)
	if _, err := e.Rephrase(ctx, RephraseInput{}); ReasonOf(err) != ReasonAuth || !e.AuthBlocked() {
		t.Fatal("auth")
	}
	if st, c := e.Status(ctx); st != StatusAuthError || c != ReasonAuth {
		t.Fatal(st, c)
	}
	// a probe that hits 401 reports auth_error too
	clk.Advance(11 * time.Minute)
	e2 := New(e.Cfg, fl, clk)
	if st, _ := e2.Status(ctx); st != StatusAuthError {
		t.Fatal("probe auth", st)
	}
	fl["llm_enabled"] = "off"
	if st, _ := e.Status(ctx); st != StatusDisabled || e.Enabled(ctx) {
		t.Fatal("kill switch")
	}
}

func TestPromptsLoaded(t *testing.T) {
	if len(IntentSchemaJSON()) == 0 || IntentVersion == "" || RephraseVersion == "" {
		t.Fatal("prompts")
	}
}
