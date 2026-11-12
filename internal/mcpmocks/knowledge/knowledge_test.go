package knowledge_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/knowledge"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

var (
	intern = map[string]any{"user_id": "u-intern-be", "role": "intern"}
	mentor = map[string]any{"user_id": "u-mentor", "role": "mentor"}
)

type result struct {
	Items  []knowledge.Item   `json:"items"`
	Denied []knowledge.Denied `json:"denied"`
	Lex    []string           `json:"query_lexemes"`
}

type env struct {
	c *mcpclient.MCP
	m *mockstest.Env
}

func setup(t *testing.T) *env {
	m := mockstest.Start(t, testkit.NewClock(testkit.Today))
	return &env{c: &mcpclient.MCP{URLs: m.URLs(), Timeout: 5 * time.Second}, m: m}
}

func (e *env) search(t *testing.T, actor map[string]any, q string, ctx map[string]any) result {
	t.Helper()
	var r result
	in := map[string]any{"actor": actor, "query": q}
	if ctx != nil {
		in["context"] = ctx
	}
	if err := e.c.Call(context.Background(), "knowledge", "search", in, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func top(r result) string {
	if len(r.Items) == 0 {
		return ""
	}
	return r.Items[0].Slug
}

// KM-01, KM-02, KM-03, KM-09
func TestSearch(t *testing.T) {
	e := setup(t)
	r := e.search(t, intern, "код ревью", nil)
	if !strings.HasPrefix(top(r), "code-review") || len(r.Lex) == 0 {
		t.Fatalf("KM-01 %+v", r)
	}
	found := false
	for _, it := range e.search(t, intern, "сколько ждать код-ревью", nil).Items {
		if it.Slug == "neighborhood-agreement" {
			found = true
		}
	}
	if !found {
		t.Fatal("KM-02: OR of terms")
	}
	for _, q := range []string{"процесс", "процессы", "процессов", "процессами", "о процессе"} {
		if got := top(e.search(t, intern, q, nil)); got != "processes-scrum" {
			t.Errorf("KM-03 %q → %s", q, got)
		}
	}
	for _, q := range []string{"что такое stash", "что такое avito-cli"} {
		r := e.search(t, intern, q, nil)
		if r.Items[0].Label != "test_description" || r.Items[0].SourceLevel != "none" || r.Items[0].URL != "/kb/"+r.Items[0].Slug {
			t.Fatalf("KM-09 %+v", r.Items[0])
		}
	}
	if r := e.search(t, intern, "сколько ждать код-ревью", nil); r.Items[0].Label != "test_instruction" || len(r.Items[0].Snippet) == 0 ||
		strings.Contains(r.Items[0].Snippet, "<b>") || r.Items[0].Rank <= 0 || r.Items[0].Rank >= 1 {
		t.Fatalf("item %+v", r.Items[0])
	}
	if r := e.search(t, intern, "и в на", nil); len(r.Items) != 0 || len(r.Lex) != 0 {
		t.Fatalf("stop words only: %+v", r)
	}
}

// KM-04, KM-05, KM-06, KM-07
func TestRights(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.search(t, intern, "что в методичке наставника", nil)
	for _, it := range r.Items {
		if it.Slug == "mentor-guide" {
			t.Fatal("KM-04: the guide is never in items for an intern")
		}
	}
	if len(r.Denied) != 1 || r.Denied[0].Slug != "mentor-guide" || r.Denied[0].Title == "" {
		t.Fatalf("KM-04 denied %+v", r.Denied)
	}
	if r := e.search(t, intern, "кто мой наставник", nil); len(r.Denied) != 0 {
		t.Fatalf("a closed doc that covers no more of the query is not reported: %+v", r.Denied)
	}
	if r := e.search(t, mentor, "что в методичке наставника", nil); top(r) != "mentor-guide" || len(r.Denied) != 0 {
		t.Fatalf("KM-05 %+v", r)
	}
	// KM-06: a closed document without disclose_title does not exist for the role
	if _, err := e.m.Pool.Exec(ctx, `INSERT INTO mock_knowledge.docs (slug, title, kind, source_level, allowed_roles, disclose_title, summary, body_md,
		tsv, title_tsv, updated_at) VALUES ('secret-fixture', 'Секретная методичка', 'guide', 'team', ARRAY['mentor'], false, 'Тест', 'Тело',
		to_tsvector('russian', 'Секретная методичка'), to_tsvector('russian', 'Секретная методичка'), now())`); err != nil {
		t.Fatal(err)
	}
	if r := e.search(t, intern, "секретная методичка", nil); len(r.Denied) != 0 {
		for _, d := range r.Denied {
			if d.Slug == "secret-fixture" {
				t.Fatalf("KM-06 %+v", r.Denied)
			}
		}
	}
	get := func(actor map[string]any, slug string) (map[string]any, error) {
		var out struct {
			Doc map[string]any `json:"doc"`
		}
		err := e.c.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": actor, "slug": slug}, &out)
		return out.Doc, err
	}
	if _, err := get(intern, "secret-fixture"); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("KM-06 get_doc: %v", err)
	}
	_, err := get(intern, "mentor-guide")
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "PERMISSION_DENIED" || te.Details["title"] == nil || len(te.Details) != 1 {
		t.Fatalf("KM-07: %v", err)
	}
	if d, err := get(mentor, "mentor-guide"); err != nil || d["body_md"] == "" || d["label"] != "test_instruction" {
		t.Fatalf("mentor get_doc: %v %v", err, d)
	}
	if d, err := get(intern, "term-stash"); err != nil || d["label"] != "test_description" || d["url"] != "/kb/term-stash" {
		t.Fatalf("get_doc: %v %v", err, d)
	}
	if _, err := get(intern, "no-such-doc"); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("missing: %v", err)
	}
	system := map[string]any{"user_id": "svc-buddy", "role": "system"}
	if _, err := get(system, "term-stash"); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("system get_doc: %v", err)
	}
	if err := e.c.Call(ctx, "knowledge", "search", map[string]any{"actor": system, "query": "ревью"}, nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("system search: %v", err)
	}
}

// KM-08: the context boosts the order but the raw rank stays raw.
func TestBoost(t *testing.T) {
	e := setup(t)
	plain := e.search(t, intern, "что такое avito-cli", nil)
	boosted := e.search(t, intern, "что такое avito-cli", map[string]any{"track": "frontend", "period": "week1"})
	if top(plain) != "term-avito-cli" || top(boosted) != "term-avito-cli" || plain.Items[0].Rank != boosted.Items[0].Rank {
		t.Fatalf("the context is a boost, not a filter: %+v / %+v", plain.Items, boosted.Items)
	}
	g := e.search(t, intern, "код ревью", nil)
	b := e.search(t, intern, "код ревью", map[string]any{"track": "backend"})
	if len(g.Items) < 2 || len(b.Items) < 2 || g.Items[0].Rank != b.Items[0].Rank {
		t.Fatalf("ranks %+v / %+v", g.Items, b.Items)
	}
}

// KM-10: the query of closed documents never selects the body.
func TestSQLAudit(t *testing.T) {
	if strings.Contains(knowledge.DeniedSQL, "body_md") {
		t.Fatal("DeniedSQL must not select body_md")
	}
	if !strings.Contains(knowledge.AllowedSQL, "d.allowed_roles @> ARRAY[$2]") {
		t.Fatal("the rights are a WHERE condition")
	}
}

// KM-13: the thresholds R_HIGH / R_LOW (config defaults) on 20 questions.
func TestThresholds(t *testing.T) {
	e := setup(t)
	cfg, err := config.LoadAPI(func(k string) string {
		return map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://x"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	band := func(r result) string {
		if len(r.Items) == 0 || r.Items[0].Rank < cfg.KBRLow {
			return "none"
		}
		if r.Items[0].Rank >= cfg.KBRHigh {
			return "high"
		}
		return "medium"
	}
	cases := []struct{ q, slug, band string }{
		{"сколько ждать код-ревью", "", "high"},
		{"код ревью", "", "high"},
		{"какой срок код-ревью в команде", "code-review-team", "high"},
		{"как у нас оценивают задачи на груминге", "processes-scrum", "high"},
		{"что такое zero bug policy", "zero-bug-policy", "high"},
		{"как оформить отгул", "time-off-test-order", "high"},
		{"дизайн система", "design-system-akita", "high"},
		{"как попасть на встречу команды", "processes-scrum", "high"},
		{"что такое avito-cli", "term-avito-cli", "high"},
		{"что такое tech-dep", "term-tech-dep", "high"},
		{"что такое stash", "term-stash", "medium"},
		{"что такое sentry", "tool-sentry", "medium"},
		{"как пользоваться grafana", "tool-grafana", "medium"},
		{"как настроить окружение", "frontend-setup", "medium"},
		{"кто мой наставник", "who-to-ask", "medium"},
		{"в каком канале спрашивать про деплой", "avito-paas", "medium"},
		{"как устроен квантовый компьютер", "", "none"},
		{"что такое infomodel", "", "none"},
		{"как работает дежурство", "", "none"},
		{"ыыы", "", "none"},
	}
	for _, c := range cases {
		r := e.search(t, intern, c.q, nil)
		if got := band(r); got != c.band || c.slug != "" && top(r) != c.slug {
			t.Errorf("%q: band %s top %s, want %s %s (%+v)", c.q, got, top(r), c.band, c.slug, r.Items)
		}
	}
}
