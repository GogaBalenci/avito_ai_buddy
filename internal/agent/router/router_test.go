package router

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/kb"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

type step struct{ id, period, kind, status, title string }

func rawStep(s step, order int) map[string]any {
	ref := strings.TrimPrefix(s.id, "be-")
	return map[string]any{"step_id": s.id, "track_id": "backend", "checklist_ref": ref, "period": s.period, "order": order,
		"title": s.title, "type": "learn", "owner": "intern", "help": map[string]any{"kind": s.kind, "kb_doc": "dev-principles"},
		"synthetic": false, "status": s.status, "request_ref": nil}
}

func pathOut(synthetic bool, steps ...step) map[string]any {
	var raw []any
	for i, s := range steps {
		raw = append(raw, rawStep(s, (i+1)*10))
	}
	return map[string]any{"employee_id": "u-intern-be",
		"track":   map[string]any{"id": "backend", "title": "Backend", "synthetic": synthetic, "disclaimer": nil},
		"periods": []any{map[string]any{"id": "week1", "title": "Неделя 1", "from_day": 1, "to_day": 7}, map[string]any{"id": "week2", "title": "Неделя 2", "from_day": 8, "to_day": 14}},
		"steps":   raw}
}

type env struct {
	agent *Agent
	tools *mcpclient.Fake
	clock *testkit.Clock
	rec   *events.Recorder
}

func newEnv(t *testing.T) *env {
	t.Helper()
	rc, _ := testkit.Redis(t)
	clk := testkit.NewClock(testkit.Today)
	tools := mcpclient.NewFake()
	emp := testkit.EmployeeProvider()
	g := &guard.Guard{Employees: emp}
	rec := &events.Recorder{}
	dir, _ := fs.Sub(content.FS(), "kb")
	docs, err := kb.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Rules: rules.MustLoad(), Guard: g, Events: rec, Docs: NewStaticDocs(docs), Redis: rc, Clock: clk,
		Path:    &path.Service{Tools: tools, Employees: emp, Clock: clk, Guard: g},
		Actions: &actions.Service{Store: &actions.Store{Redis: rc, Clock: clk}, Tools: tools, Guard: g, Events: rec, Clock: clk},
		Requests: func(context.Context, string) ([]api.RequestView, error) {
			return nil, errors.New("db down")
		},
	}
	return &env{agent: a, tools: tools, clock: clk, rec: rec}
}

var intern = actor.Actor{UserID: "u-intern-be", Role: actor.Intern}

func (e *env) say(t *testing.T, text string) *api.AgentReply {
	t.Helper()
	out, err := e.agent.Handle(context.Background(), intern, Input{ClientMessageID: uuid.New(), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return out.Reply
}

func (e *env) path(p map[string]any) {
	e.tools.On("checklist.get_path", func(context.Context, map[string]any) (any, error) { return p, nil })
}

func TestMyPathStates(t *testing.T) {
	e := newEnv(t)
	e.path(pathOut(true, step{"be-1.1", "week1", "explain", "done", "Первый"}, step{"be-2.1", "week2", "explain", "not_started", "Второй"}))
	r := e.say(t, "что дальше")
	if !strings.Contains(r.Text, "Дальше — «Второй», его можно начать заранее.") || len(r.Notices) != 1 || r.Notices[0] != "synthetic_track" {
		t.Fatalf("ahead + synthetic: %+v", r)
	}
	e.path(pathOut(false, step{"be-1.1", "week1", "explain", "done", "Первый"}, step{"be-1.2", "week1", "access_request", "done", "Второй"}))
	if r = e.say(t, "что дальше"); r.MascotState != api.MascotStateDone || !strings.Contains(r.Text, "Все шаги пройдены") || len(r.Actions) != 0 {
		t.Fatalf("finished: %+v", r)
	}
	e.path(pathOut(false, step{"be-1.1", "week1", "explain", "done", "Первый"}, step{"be-1.2", "week1", "access_request", "waiting", "Доступ"}))
	if r = e.say(t, "что дальше"); !strings.Contains(r.Text, "Осталось дождаться ответов: «Доступ»") {
		t.Fatalf("only_waiting: %+v", r)
	}
	e.clock.Set(testkit.Today.AddDate(0, 0, -10)) // before the start of the intern
	e.path(pathOut(false, step{"be-1.1", "week1", "track_only", "not_started", "Первый"}))
	if r = e.say(t, "что дальше"); !strings.Contains(r.Text, "Стажировка начнётся 07.11") || len(r.Actions) != 1 || *r.Actions[0].Payload != "step:be-1.1" ||
		r.Actions[0].Label != "Начать: Первый" {
		t.Fatalf("pre_start: %+v", r)
	}
}

func TestStepButtonLabels(t *testing.T) {
	for kind, want := range map[string]string{"explain": "Разобраться", "access_request": "Запросить доступ", "ask_mentor": "Сформулировать вопрос", "track_only": "Начать"} {
		if got := stepButton(path.Step{HelpKind: kind}); got != want {
			t.Errorf("%s: %s", kind, got)
		}
	}
}

func TestRequestsDownAndLines(t *testing.T) {
	e := newEnv(t)
	if r := e.say(t, "мои заявки"); r.MascotState != api.MascotStateOops || *r.Actions[0].Payload != "intent:my_requests" {
		t.Fatalf("requests down: %+v", r)
	}
	e.agent.Requests = func(context.Context, string) ([]api.RequestView, error) { return nil, nil }
	if r := e.say(t, "мои заявки"); !strings.Contains(r.Text, "Заявок пока нет") {
		t.Fatalf("empty: %+v", r)
	}
	title := "Канал Tech-dep"
	from := api.IsoDate{Time: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}
	to := api.IsoDate{Time: time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)}
	var list []api.RequestView
	for i := 0; i < 6; i++ {
		list = append(list, api.RequestView{RequestId: "ABS-00010" + string(rune('0'+i)), Kind: api.RequestKindTimeOff, Status: "registered", DateFrom: &from, DateTo: &from})
	}
	list[1].DateTo = &to
	list[2] = api.RequestView{RequestId: "ACC-000101", Kind: "access", Status: "pending", ResourceTitle: &title}
	list[3] = api.RequestView{RequestId: "ACC-000102", Kind: "access", Status: "weird"}
	e.agent.Requests = func(context.Context, string) ([]api.RequestView, error) { return list, nil }
	r := e.say(t, "мои заявки")
	for _, want := range []string{"ABS-000100 — отгул 01.12, «Оформлена»", "отгул 01.12–02.12", "доступ «Канал Tech-dep», «Ждёт наставника»", "ACC-000102 — доступ, «weird»"} {
		if !strings.Contains(r.Text, want) {
			t.Fatalf("%q not in %s", want, r.Text)
		}
	}
	if strings.Contains(r.Text, "ABS-000105") {
		t.Fatal("at most 5 requests")
	}
}

func TestPayloadErrors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, p := range []string{"intent", "intent:", "zzz:1", "step:BAD"} {
		_, err := e.agent.Handle(ctx, intern, Input{ClientMessageID: uuid.New(), Payload: p})
		var re *Error
		if !errors.As(err, &re) || re.Status != 400 || re.Error() == "" {
			t.Fatalf("%q: %v", p, err)
		}
	}
	if labelOf("x") != "x" {
		t.Fatal("labelOf")
	}
}

func TestOutOfScopeOther(t *testing.T) {
	e := newEnv(t)
	r := e.agent.outOfScope(context.Background(), intern, Input{ClientMessageID: uuid.New()}, recognition{Result: rules.Result{Intent: rules.OutOfScope, Topic: "other"}})
	if r.Intent != api.AgentReplyIntent(rules.Help) {
		t.Fatalf("other → help: %+v", r)
	}
	// without who-to-ask the text has no marker
	delete(e.agent.Docs.(StaticDocs), "who-to-ask")
	if r = e.say(t, "когда зарплата"); strings.Contains(r.Text, "[1]") || len(r.Sources) != 0 {
		t.Fatalf("no who-to-ask: %+v", r)
	}
	if r = e.say(t, "как уволиться"); strings.Contains(r.Text, "[1]") {
		t.Fatalf("no who-to-ask: %+v", r)
	}
	delete(e.agent.Docs.(StaticDocs), "time-off-test-order")
	if r = e.say(t, "хочу отгул"); len(r.Sources) != 0 || !strings.Contains(r.Text, "тестовому порядку:") {
		t.Fatalf("no time-off doc: %+v", r)
	}
}

func TestStaticDocsRoles(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.agent.Docs.Doc(ctx, intern, "mentor-guide"); !errors.Is(err, ErrDocNotFound) {
		t.Fatalf("intern must not read the mentor guide: %v", err)
	}
	d, err := e.agent.Docs.Doc(ctx, actor.Actor{UserID: "u-mentor", Role: actor.Mentor}, "mentor-guide")
	if err != nil || d.Summary == "" {
		t.Fatalf("mentor: %v %+v", err, d)
	}
	if termOf("Термин: avito-cli") != "avito-cli" || termOf("Без префикса") != "Без префикса" {
		t.Fatal("termOf")
	}
}

func TestAnswerToQuestionEdgeCases(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.agent.Redis.Set(ctx, lbqKey("u"), "not json", contextTTL)
	if e.agent.answerToQuestion(ctx, "u", "1") != "" {
		t.Fatal("broken context must be ignored")
	}
	e.agent.rememberQuestion(ctx, "u", []string{rules.MyPath})
	if e.agent.answerToQuestion(ctx, "u", "5") != "" || e.agent.answerToQuestion(ctx, "u", "1") != rules.MyPath {
		t.Fatal("numbers")
	}
	e.agent.rememberQuestion(ctx, "u", nil)
	if e.agent.answerToQuestion(ctx, "u", "1") != "" {
		t.Fatal("no options — nothing to remember")
	}
}
