// Package router is the orchestrator of the agent (09 §2, §5): it takes a
// masked message or a button payload, recognizes the intent, checks rights in
// code, reads services through ToolClient and builds a deterministic reply.
// Writes to services happen only through confirm endpoints (09 §8.1).
package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/access"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
)

// Context TTL of last_bot_question and the "not understood" counter (09 §2.2).
const contextTTL = 10 * time.Minute

// Agent handles chat messages.
type Agent struct {
	Rules   *rules.Dict
	Path    *path.Service
	Actions *actions.Service
	Guard   *guard.Guard
	Events  events.Publisher
	Docs    DocSource
	Redis   *redis.Client
	Clock   clock.Clock
	// Catalog is the access catalog (S4): aliases, resources, roles.
	Catalog *access.Catalog
	// Tools reaches the knowledge service (search).
	Tools mcpclient.Client
	// RHigh, RLow are the search thresholds (09 §6.5).
	RHigh, RLow float64
	// Inbox counts pending requests of the mentor's interns (X-A28).
	Inbox func(ctx context.Context, mentorID string) (int, error)
	// Requests returns the user's requests, newest first (S3 + S4).
	Requests func(ctx context.Context, userID string) ([]api.RequestView, error)
}

// Input is one chat message after the pre-checks.
type Input struct {
	ClientMessageID uuid.UUID
	Text            string // masked; empty for a payload
	Payload         string
	Masked          bool // pii.Mask replaced something
}

// Output is the reply and, for a payload, the label to show in history.
type Output struct {
	Reply       *api.AgentReply
	ButtonLabel string
}

// Error is an HTTP-level failure of a message (400/403/404/501).
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func badPayload() *Error {
	return &Error{http.StatusBadRequest, "BAD_PAYLOAD", "Не получилось обработать кнопку — обнови страницу"}
}

func noStep() *Error {
	return &Error{http.StatusNotFound, "NOT_FOUND", "Такого шага в твоём пути нет"}
}

var reResource = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)

var reStepID = regexp.MustCompile(`^[a-z]{2}-[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// buttonIntents are allowed in intent: payloads (09 §2.1).
var buttonIntents = map[string]bool{rules.Help: true, rules.MyPath: true, rules.StepExplain: true, rules.StepDone: true,
	rules.KBQuestion: true, rules.TimeOff: true, rules.AccessRequest: true, rules.MyRequests: true, rules.AskMentor: true}

// internRoutes are the stage scenarios of an intern (09 §3.1).
var internRoutes = map[string]bool{rules.MyPath: true, rules.StepExplain: true, rules.StepDone: true,
	rules.AskMentor: true, rules.TimeOff: true, rules.AccessRequest: true}

// intentLabels are quick-reply labels of intents (menu, clarification).
var intentLabels = map[string]string{
	rules.MyPath: "Что мне делать дальше?", rules.TimeOff: "Оформить отгул", rules.AccessRequest: "Запросить доступ",
	rules.AskMentor: "Спросить наставника", rules.MyRequests: "Мои заявки", rules.Help: "Что ты умеешь?",
	rules.StepExplain: "Объяснить шаг", rules.StepDone: "Отметить шаг", rules.KBQuestion: "Вопрос о процессах",
}

// recognition is what the pipeline knows about the message.
type recognition struct {
	rules.Result
	Source string // rules | button
	Query  string
}

// Handle processes one message.
func (g *Agent) Handle(ctx context.Context, a actor.Actor, in Input) (Output, error) {
	start := g.Clock.Now()
	var (
		out Output
		err error
	)
	if in.Payload != "" {
		out, err = g.handlePayload(ctx, a, in)
	} else {
		out, err = g.handleText(ctx, a, in)
	}
	if err != nil {
		return Output{}, err
	}
	if in.Masked {
		reply.Notice(out.Reply, "pii_masked")
	}
	if out.Reply.Intent != api.AgentReplyIntent(rules.Unknown) {
		g.Redis.Del(ctx, unkKey(a.UserID)) // any recognized intent resets the counter (09 §6.4)
	}
	g.publish(ctx, "reply_sent", map[string]any{"intent": string(out.Reply.Intent),
		"latency_ms_total": g.Clock.Now().Sub(start).Milliseconds(), "rephrased": false, "reply_id": out.Reply.ReplyId.String()})
	return out, nil
}

func (g *Agent) publish(ctx context.Context, name string, payload map[string]any) {
	if g.Events != nil {
		_ = g.Events.Publish(ctx, events.Event{Name: name, Mode: "demo", Payload: payload})
	}
}

// --- payloads ---------------------------------------------------------------

func (g *Agent) handlePayload(ctx context.Context, a actor.Actor, in Input) (Output, error) {
	kind, value, ok := strings.Cut(in.Payload, ":")
	if !ok || value == "" {
		return Output{}, badPayload()
	}
	switch kind {
	case "intent", "clarify":
		if !buttonIntents[value] {
			return Output{}, badPayload()
		}
		if kind == "clarify" {
			g.forgetQuestion(ctx, a.UserID) // any option is fine: unknown ones act as intent:
		}
		rec := recognition{Result: rules.Result{Intent: value, Confidence: 1}, Source: "button"}
		g.recognized(ctx, a, rec)
		r, err := g.route(ctx, a, in, rec)
		return Output{Reply: r, ButtonLabel: labelOf(value)}, err
	case "step":
		if !reStepID.MatchString(value) {
			return Output{}, badPayload()
		}
		return g.stepButton(ctx, a, in, value)
	case "resource":
		if !reResource.MatchString(value) {
			return Output{}, badPayload()
		}
		return g.resourcePayload(ctx, a, in, value)
	}
	return Output{}, badPayload()
}

func labelOf(intent string) string {
	if l, ok := intentLabels[intent]; ok {
		return l
	}
	return intent
}

// --- text --------------------------------------------------------------------

func (g *Agent) handleText(ctx context.Context, a actor.Actor, in Input) (Output, error) {
	// «Да» текстом не подтверждает черновик (09 §8.1, 10 A18).
	if g.Actions != nil && rules.IsConfirmWord(in.Text) && g.Actions.Store.ActiveDraft(ctx, a.UserID) != "" {
		r := reply.New(in.ClientMessageID, rules.TimeOff)
		r.Text = "Чтобы отправить, нажми «Подтвердить» в карточке выше — так ты точно видишь, что отправляешь."
		return Output{Reply: r}, nil
	}
	h := rules.Hints{}
	h.Aliases = g.catalogAliases(ctx, a)
	var loaded *path.Loaded
	if a.Role == actor.Intern {
		if l, err := g.Path.Load(ctx, a, ""); err == nil {
			loaded = l
			for _, s := range l.Steps {
				h.Steps = append(h.Steps, rules.StepTitle{ID: s.ID, Title: s.Title})
			}
		}
	}
	res := g.Rules.Recognize(in.Text, h)
	rec := recognition{Result: res, Source: "rules", Query: in.Text}

	// p. 7 of 09 §6.3: an answer to the previous clarification.
	if opt := g.answerToQuestion(ctx, a.UserID, in.Text); opt != "" {
		rec.Intent, rec.Confidence, rec.Ambiguous = opt, 0.9, nil
	}
	if len(rec.Ambiguous) > 1 {
		return Output{Reply: g.clarify(ctx, a, in, rec.Ambiguous)}, nil
	}
	if rec.Intent == rules.Unknown || rec.Intent == rules.KBQuestion && in.Masked {
		if in.Masked { // only personal data in the message: say so, nothing to escalate
			r := reply.New(in.ClientMessageID, rules.Unknown)
			r.Text = "Такие данные мне не нужны — я их скрыл и не сохраняю. Чем помочь?"
			menu(r, a.Role)
			return Output{Reply: r}, nil
		}
		return Output{Reply: g.unknown(ctx, a, in, loaded)}, nil
	}
	g.recognized(ctx, a, rec)
	r, err := g.route(ctx, a, in, rec)
	if err == nil && r != nil && a.Role == actor.Intern {
		switch {
		case rec.Second == rules.AccessRequest && rec.ResourceID == "":
			reply.Quick(r, intentLabels[rules.AccessRequest], "intent:access_request")
		case rec.Intent != rules.AccessRequest && rec.ResourceID != "":
			g.accessHint(ctx, a, r, rec.ResourceID) // 11 §3.5.4, p. 2; A-26
		}
	}
	return Output{Reply: r}, err
}

func (g *Agent) recognized(ctx context.Context, a actor.Actor, rec recognition) {
	g.publish(ctx, "intent_recognized", map[string]any{"intent": rec.Intent, "source": rec.Source,
		"override": nil, "confidence": rec.Confidence, "latency_ms": 0})
}

// --- routing -----------------------------------------------------------------

func (g *Agent) route(ctx context.Context, a actor.Actor, in Input, rec recognition) (*api.AgentReply, error) {
	if internRoutes[rec.Intent] && a.Role != actor.Intern {
		return g.mentorTemplate(ctx, a, in, rec.Intent), nil
	}
	switch rec.Intent {
	case rules.Help:
		return g.help(in, a), nil
	case rules.MyPath:
		return g.myPath(ctx, a, in), nil
	case rules.StepExplain, rules.StepDone:
		return g.stepText(ctx, a, in, rec)
	case rules.TimeOff:
		return g.timeOff(ctx, a, in, rec), nil
	case rules.MyRequests:
		return g.myRequests(ctx, a, in), nil
	case rules.AskMentor:
		return g.askMentor(ctx, a, in, rec.Query), nil
	case rules.OutOfScope:
		return g.outOfScope(ctx, a, in, rec), nil
	case rules.AccessRequest:
		return g.accessRequest(ctx, a, in, rec), nil
	case rules.KBQuestion:
		return g.kbQuestion(ctx, a, in, rec), nil
	}
	return g.unknown(ctx, a, in, nil), nil
}

// mentorTemplate answers a mentor on an intern scenario (09 §3.1).
func (g *Agent) mentorTemplate(ctx context.Context, a actor.Actor, in Input, intent string) *api.AgentReply {
	r := reply.New(in.ClientMessageID, intent)
	switch intent {
	case rules.TimeOff:
		r.Text = "Это сценарий для стажёров. Предстоящие отгулы стажёров — на экране «Мои стажёры»."
		reply.Link(r, "Мои стажёры", "/mentor/interns")
	case rules.AccessRequest:
		r.Text = "Это сценарий для стажёров. Входящие заявки — на экране «Входящие»."
		reply.Link(r, "Входящие", "/mentor/inbox")
	default:
		r.Text = "Это сценарий для стажёров. Прогресс твоих стажёров — на экране «Мои стажёры»."
		reply.Link(r, "Мои стажёры", "/mentor/interns")
	}
	g.publish(ctx, "permission_denied", map[string]any{"tool": "route:" + intent, "role": a.Role})
	return r
}

func menu(r *api.AgentReply, role string) {
	if role == actor.Mentor {
		reply.Link(r, "Мои стажёры", "/mentor/interns")
		reply.Link(r, "Входящие", "/mentor/inbox")
		return
	}
	for _, i := range []string{rules.MyPath, rules.TimeOff, rules.AccessRequest, rules.AskMentor} {
		reply.Quick(r, intentLabels[i], "intent:"+i)
	}
}

func (g *Agent) help(in Input, a actor.Actor) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.Help)
	if a.Role == actor.Mentor {
		r.Text = "Привет! Я Витя, бадди на стажировке. Отвечу на вопросы о процессах, а прогресс стажёров и их заявки — на экранах «Мои стажёры» и «Входящие»."
	} else {
		r.Text = "Привет! Я Витя, твой бадди на стажировке. Подскажу, что делать дальше, отвечу на вопросы о команде и инструментах, оформлю отгул или заявку на доступ."
	}
	menu(r, a.Role)
	return r
}

// --- S1: my path ---------------------------------------------------------------

func retry(r *api.AgentReply, payload string) {
	p := payload
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.Retry, Label: "Повторить", Payload: &p})
}

func markDone(r *api.AgentReply, stepID string) {
	id := stepID
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.MarkDone, Label: "Отметить пройденным", StepId: &id})
}

// stepButton is the main button of a step by help.kind (11 §1.6).
func stepButton(s path.Step) string {
	switch s.HelpKind {
	case "access_request":
		return "Запросить доступ"
	case "ask_mentor":
		return "Сформулировать вопрос"
	case "track_only":
		return "Начать"
	}
	return "Разобраться"
}

func ddmm(t time.Time) string { return t.Format("02.01") }

func (g *Agent) myPath(ctx context.Context, a actor.Actor, in Input) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.MyPath)
	l, err := g.Path.Load(ctx, a, "")
	if err != nil {
		r.MascotState = api.MascotStateOops
		r.Text = "Не могу получить твой путь, попробуй ещё раз."
		retry(r, "intent:my_path")
		return r
	}
	if l.Track.Synthetic {
		reply.Notice(r, "synthetic_track")
	}
	var next *path.Step
	if l.Next != nil {
		if s, ok := l.Step(l.Next.StepID); ok {
			next = &s
		}
	}
	waiting := waitingTitles(l)
	switch l.State {
	case path.StateFinished:
		r.MascotState = api.MascotStateDone
		r.Text = "Все шаги пройдены. Обсуди итоги с наставником на 1:1."
	case path.StateOnlyWaiting:
		r.Text = "Осталось дождаться ответов: " + strings.Join(quote(waiting), ", ") + "."
		reply.Quick(r, "Обновить", "intent:my_path")
	case path.StatePreStart:
		r.Text = "Стажировка начнётся " + ddmm(l.Start) + ". Можно заглянуть в первую неделю."
	default:
		r.Text = fmt.Sprintf("Ты на этапе «%s», готово %d из %d.", l.PeriodTitle(l.CurrentPeriod), l.Progress.PeriodDone, l.Progress.PeriodTotal)
		if next != nil {
			r.Text += " Дальше — «" + next.Title + "»"
			if l.Next.Reason == path.ReasonAhead {
				r.Text += ", его можно начать заранее"
			}
			r.Text += "."
		}
		if len(waiting) > 0 {
			r.Text += " Ждём ответа по «" + waiting[0] + "»."
		}
	}
	if next != nil && l.State != path.StateFinished {
		reply.Quick(r, stepButton(*next)+": "+next.Title, "step:"+next.ID)
	}
	return r
}

func waitingTitles(l *path.Loaded) []string {
	var out []string
	for _, id := range l.Waiting {
		if s, ok := l.Step(id); ok {
			out = append(out, s.Title)
		}
	}
	return out
}

func quote(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "«" + x + "»"
	}
	return out
}

// stepButton handles the step: payload (11 §1.6, P-A10, P-A19, P-A20).
func (g *Agent) stepButton(ctx context.Context, a actor.Actor, in Input, stepID string) (Output, error) {
	if a.Role != actor.Intern {
		return Output{}, noStep()
	}
	s, err := g.Path.GetStep(ctx, a, stepID)
	if err != nil {
		if isNotFound(err) {
			return Output{}, noStep()
		}
		r := reply.New(in.ClientMessageID, rules.StepExplain)
		r.MascotState = api.MascotStateOops
		r.Text = "Не могу получить шаг, попробуй ещё раз."
		retry(r, "step:"+stepID)
		return Output{Reply: r, ButtonLabel: "Шаг " + stepID}, nil
	}
	if s.Owner != "intern" {
		return Output{}, noStep()
	}
	label := stepButton(s) + ": " + s.Title
	intent := rules.StepExplain
	if s.HelpKind == "access_request" {
		intent = rules.AccessRequest
	}
	g.recognized(ctx, a, recognition{Result: rules.Result{Intent: intent, Confidence: 1}, Source: "button"})
	switch s.Status {
	case "done":
		return Output{Reply: alreadyDone(in, s), ButtonLabel: label}, nil
	case "waiting":
		return Output{Reply: g.waitingStep(ctx, a, in, s), ButtonLabel: label}, nil
	case "not_started":
		key := uuid.NewSHA1(uuid.NameSpaceURL, []byte("buddy:chat:"+in.ClientMessageID.String())).String() // the same for a repeat
		st, changed, _, err := g.Path.SetStatus(ctx, a, s.ID, "in_progress", "not_started", "user_start", key)
		if err == nil {
			s = st
			if changed {
				g.publish(ctx, "step_started", map[string]any{"step_id": s.ID, "period": s.Period})
			}
		}
	}
	if s.HelpKind == "access_request" {
		return Output{Reply: g.accessStep(ctx, a, in, s), ButtonLabel: label}, nil
	}
	return Output{Reply: g.explainStep(ctx, a, in, s, true), ButtonLabel: label}, nil
}

func alreadyDone(in Input, s path.Step) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.StepDone)
	r.Text = "Шаг «" + s.Title + "» уже отмечен. Вернуть его в работу можно в панели «Мой путь»."
	return r
}

func (g *Agent) waitingStep(ctx context.Context, a actor.Actor, in Input, s path.Step) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.StepExplain)
	r.Text = "По шагу «" + s.Title + "» заявка уже отправлена — ждём ответа."
	if g.Path.Pool != nil {
		if reqs, err := g.Path.Requests(ctx, a.UserID); err == nil {
			if rq, ok := reqs[s.ID]; ok && rq.AutoApproveAt != nil {
				r.Text = "По шагу «" + s.Title + "» заявка " + rq.RequestID + " одобрится около " + rq.AutoApproveAt.In(g.Clock.Now().Location()).Format("15:04") + "."
			} else if ok {
				r.Text = "По шагу «" + s.Title + "» заявка " + rq.RequestID + " ждёт наставника."
			}
		}
	}
	reply.Quick(r, "Обновить", "intent:my_path")
	return r
}

// explainStep builds the reply by help.kind; button=true marks a pressed button.
func (g *Agent) explainStep(ctx context.Context, a actor.Actor, in Input, s path.Step, button bool) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.StepExplain)
	switch s.HelpKind {
	case "explain":
		d, err := g.Docs.Doc(ctx, a, s.KBDoc)
		if err != nil {
			r.MascotState = api.MascotStateOops
			r.Text = "Объяснение сейчас не загрузилось."
			retry(r, "step:"+s.ID)
			markDone(r, s.ID)
			return r
		}
		n := reply.Source(r, d.Slug, d.Title, d.Label, d.SourceLevel, d.Dated)
		if d.SourceLevel == "none" {
			r.MascotState = api.MascotStateAskMentor
			r.Text = fmt.Sprintf("В открытых источниках про «%s» ничего нет, поэтому вот тестовое описание [%d]: %s Уточни у наставника — помогу сформулировать вопрос.", termOf(d.Title), n, d.Summary)
			g.escalate(ctx, a, r, "no_source_term", "Что значит шаг «"+s.Title+"»?", s.Period, s.Title, []string{d.Title})
		} else {
			r.Text = fmt.Sprintf("%s [%d]", d.Summary, n)
		}
		markDone(r, s.ID)
	case "access_request":
		r.Text = "Шаг «" + s.Title + "» — это заявка на доступ."
		reply.Quick(r, "Запросить доступ: "+s.Title, "step:"+s.ID)
	case "ask_mentor":
		r.MascotState = api.MascotStateAskMentor
		r.Text = "Вот черновик вопроса наставнику по шагу «" + s.Title + "» — поправь и отправь сам."
		g.escalate(ctx, a, r, "explicit", s.Title, s.Period, s.Title, nil)
		markDone(r, s.ID)
	default:
		if button {
			r.Text = "Шаг «" + s.Title + "» в работе. Когда закончишь — отметь шаг."
		} else {
			r.Text = "Шаг «" + s.Title + "»: когда закончишь — отметь его."
		}
		markDone(r, s.ID)
	}
	return r
}

// termOf strips a "Термин: " style prefix from a document title.
func termOf(title string) string {
	if _, after, ok := strings.Cut(title, ": "); ok {
		return after
	}
	return title
}

// stepText handles step_explain / step_done typed as text (09 §5.1).
func (g *Agent) stepText(ctx context.Context, a actor.Actor, in Input, rec recognition) (*api.AgentReply, error) {
	l, err := g.Path.Load(ctx, a, "")
	if err != nil {
		r := reply.New(in.ClientMessageID, rec.Intent)
		r.MascotState = api.MascotStateOops
		r.Text = "Не могу получить твой путь, попробуй ещё раз."
		retry(r, "intent:my_path")
		return r, nil
	}
	var s path.Step
	ok := false
	if rec.StepRef != "" {
		s, ok = l.StepByRef(rec.StepRef)
	}
	if !ok && rec.StepID != "" {
		s, ok = l.Step(rec.StepID)
	}
	if !ok { // P-A11: the parameter is dropped, ask with the current period's steps
		r := reply.New(in.ClientMessageID, rec.Intent)
		r.Text = "Не нашёл такой шаг в твоём пути. Какой из шагов этапа «" + l.PeriodTitle(l.CurrentPeriod) + "» ты имеешь в виду?"
		n := 0
		for _, st := range l.Steps {
			if st.Period == l.CurrentPeriod && st.Status != "done" && n < 4 {
				reply.Quick(r, st.Title, "step:"+st.ID)
				n++
			}
		}
		return r, nil
	}
	if rec.Intent == rules.StepExplain {
		return g.explainStep(ctx, a, in, s, false), nil
	}
	r := reply.New(in.ClientMessageID, rules.StepDone)
	switch {
	case s.Status == "done":
		return alreadyDone(in, s), nil
	case s.HelpKind == "access_request":
		r.Text = "Этот шаг закроется сам, когда заявку одобрят."
	default:
		r.Text = "Отметить «" + s.Title + "» пройденным? Нажми кнопку — так ты точно видишь, что меняешь."
		markDone(r, s.ID)
	}
	return r, nil
}

// --- S3: time off ---------------------------------------------------------------

func (g *Agent) timeOff(ctx context.Context, a actor.Actor, in Input, rec recognition) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.TimeOff)
	if d, err := g.Docs.Doc(ctx, a, "time-off-test-order"); err == nil {
		n := reply.Source(r, d.Slug, d.Title, d.Label, d.SourceLevel, d.Dated)
		r.Text = fmt.Sprintf("Отгул оформляется по тестовому порядку [%d]: выбираешь даты, я создаю заявку в тестовом сервисе отсутствий, а наставника лучше предупредить самому. Выбери даты.", n)
	} else {
		r.Text = "Отгул оформляется по тестовому порядку: выбираешь даты, я создаю заявку в тестовом сервисе отсутствий, а наставника лучше предупредить самому. Выбери даты."
	}
	existing, _ := g.Actions.Upcoming(ctx, a)
	form := api.ReplyActionFormTimeOff
	act := api.ReplyAction{Type: api.OpenForm, Label: "Выбрать даты", Form: &form}
	if len(existing) > 0 {
		act.Existing = &existing
	}
	r.Actions = append(r.Actions, act)
	entry := "chat"
	if rec.Source == "button" {
		entry = "help_menu"
	}
	g.publish(ctx, "time_off_form_opened", map[string]any{"entry": entry, "prefilled": false, "has_existing": len(existing) > 0})
	return r
}

// --- requests ------------------------------------------------------------------------

var statusLabels = map[string]string{"registered": "Оформлена", "pending": "Ждёт наставника", "approved": "Одобрена",
	"rejected": "Отклонена", "cancelled": "Отменена"}

func (g *Agent) myRequests(ctx context.Context, a actor.Actor, in Input) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.MyRequests)
	if a.Role == actor.Mentor {
		r.Text = "Входящие заявки стажёров — на экране «Входящие»."
		if g.Inbox != nil {
			if n, err := g.Inbox(ctx, a.UserID); err == nil {
				r.Text = "Входящих заявок: " + strconv.Itoa(n) + "."
			}
		}
		reply.Link(r, "Входящие", "/mentor/inbox")
		return r
	}
	list, err := g.Requests(ctx, a.UserID)
	if err != nil {
		r.MascotState = api.MascotStateOops
		r.Text = "Не могу получить твои заявки, попробуй ещё раз."
		retry(r, "intent:my_requests")
		return r
	}
	if len(list) == 0 {
		r.Text = "Заявок пока нет. Оформить отгул?"
		reply.Quick(r, "Оформить отгул", "intent:time_off")
		return r
	}
	var parts []string
	for i, v := range list {
		if i == 5 {
			break
		}
		parts = append(parts, requestLine(v))
	}
	r.Text = "Твои заявки: " + strings.Join(parts, "; ") + "."
	reply.Link(r, "Все заявки", "/requests")
	return r
}

func requestLine(v api.RequestView) string {
	st := statusLabels[string(v.Status)]
	if st == "" {
		st = string(v.Status)
	}
	what := "доступ"
	if v.Kind == api.RequestKindTimeOff && v.DateFrom != nil && v.DateTo != nil {
		what = "отгул " + ddmm(v.DateFrom.Time)
		if !v.DateTo.Equal(v.DateFrom.Time) {
			what += "–" + ddmm(v.DateTo.Time)
		}
	} else if v.ResourceTitle != nil {
		what = "доступ «" + *v.ResourceTitle + "»"
	}
	return v.RequestId + " — " + what + ", «" + st + "»"
}

// --- escalation, out of scope, unknown ----------------------------------------------------

// escalate attaches a draft question to the mentor (09 §8.2).
func (g *Agent) escalate(ctx context.Context, a actor.Actor, r *api.AgentReply, reason, query, period, stepTitle string, seen []string) {
	var b strings.Builder
	b.WriteString("Привет!")
	if period != "" || stepTitle != "" {
		b.WriteString(" Я")
		if period != "" {
			b.WriteString(" на этапе «" + period + "»")
			if stepTitle != "" {
				b.WriteString(",")
			}
		}
		if stepTitle != "" {
			b.WriteString(" шаг «" + stepTitle + "»")
		}
		b.WriteString(".")
	}
	if query != "" {
		b.WriteString("\nВопрос: " + query)
	}
	if len(seen) > 0 {
		b.WriteString("\nЧто уже посмотрел: " + strings.Join(seen, ", "))
	} else if reason == "no_answer" {
		b.WriteString("\nЧто уже посмотрел: в базе знаний бадди ответа не нашлось")
	}
	r.Escalation = &api.Escalation{Draft: b.String(), Reason: api.EscalationReason(reason)}
	reply.Copy(r, "Скопировать", b.String(), "escalation_copied")
	g.publish(ctx, "escalated", map[string]any{"reason": reason})
}

// context of the intern for an escalation draft: period title and next step.
func (g *Agent) contextOf(ctx context.Context, a actor.Actor, l *path.Loaded) (string, string) {
	if a.Role != actor.Intern {
		return "", ""
	}
	if l == nil {
		var err error
		if l, err = g.Path.Load(ctx, a, ""); err != nil {
			return "", ""
		}
	}
	period := l.PeriodTitle(l.CurrentPeriod)
	if l.Next != nil {
		if s, ok := l.Step(l.Next.StepID); ok {
			return period, s.Title
		}
	}
	return period, ""
}

func (g *Agent) askMentor(ctx context.Context, a actor.Actor, in Input, query string) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.AskMentor)
	r.MascotState = api.MascotStateAskMentor
	r.Text = "Вот черновик вопроса наставнику — поправь и отправь сам."
	period, step := g.contextOf(ctx, a, nil)
	g.escalate(ctx, a, r, "explicit", query, period, step, nil)
	return r
}

func (g *Agent) whoToAsk(ctx context.Context, a actor.Actor, r *api.AgentReply, withMarker, without string) {
	if d, err := g.Docs.Doc(ctx, a, "who-to-ask"); err == nil {
		n := reply.Source(r, d.Slug, d.Title, d.Label, d.SourceLevel, d.Dated)
		r.Text = strings.ReplaceAll(withMarker, "[1]", "["+strconv.Itoa(n)+"]")
		return
	}
	r.Text = without
}

// outOfScope answers by topic (09 §11.4).
func (g *Agent) outOfScope(ctx context.Context, a actor.Actor, in Input, rec recognition) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.OutOfScope)
	mentorTopic := func(text string) {
		r.MascotState = api.MascotStateAskMentor
		r.Text = text
		period, step := g.contextOf(ctx, a, nil)
		g.escalate(ctx, a, r, "mentor_topic", "", period, step, nil)
	}
	switch rec.Topic {
	case "salary", "benefits":
		g.whoToAsk(ctx, a, r, "С вопросами про выплаты и бенефиты не помогу. Обычно путь такой: руководитель, затем HR BP [1].",
			"С вопросами про выплаты и бенефиты не помогу — это к руководителю или HR BP.")
	case "vacation":
		r.Text = "Отпуск я не оформляю, только отгул в тестовом сервисе. Про отпуск лучше спросить руководителя. Оформить отгул?"
		if a.Role == actor.Intern {
			reply.Quick(r, "Оформить отгул", "intent:time_off")
		}
	case "sick_leave":
		mentorTopic("Если заболел — сначала предупреди наставника и руководителя. Подробности о здоровье мне писать не нужно.")
	case "business_trip", "hiring", "dismissal":
		g.whoToAsk(ctx, a, r, "Это вне того, с чем я помогаю стажёрам. Обычно путь такой: наставник, затем руководитель, затем HR BP [1].",
			"Это вне того, с чем я помогаю стажёрам. Лучше спросить руководителя или HR BP.")
	case "evaluation":
		mentorTopic("Оценку и обратную связь о твоей работе даёт наставник — хорошая тема для 1:1. Помочь сформулировать вопрос?")
	case "wellbeing":
		mentorTopic("Звучит непросто, и это нормально в начале. Об этом стоит поговорить с наставником или руководителем на 1:1 — помочь сформулировать, с чего начать?")
	case "personal_data":
		r.Text = "Контакты коллег не даю — найди человека в корпоративном справочнике или спроси наставника."
	default:
		return g.help(in, a)
	}
	return r
}

func unkKey(userID string) string { return "ctx:unk:" + userID }
func lbqKey(userID string) string { return "ctx:lbq:" + userID }

// unknown: first time — the menu; second in a row — the menu and a draft
// question to the mentor; no third clarification (09 §6.4, AG-14).
func (g *Agent) unknown(ctx context.Context, a actor.Actor, in Input, l *path.Loaded) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.Unknown)
	n, _ := g.Redis.Incr(ctx, unkKey(a.UserID)).Result()
	g.Redis.Expire(ctx, unkKey(a.UserID), contextTTL)
	g.publish(ctx, "intent_recognized", map[string]any{"intent": rules.Unknown, "source": "rules", "override": nil, "confidence": 0, "latency_ms": 0})
	if n >= 2 {
		g.Redis.Del(ctx, unkKey(a.UserID))
		r.MascotState = api.MascotStateAskMentor
		r.Text = "Кажется, я не понимаю. Вот что я умею — или давай спросим наставника, вот черновик."
		menu(r, a.Role)
		period, step := g.contextOf(ctx, a, l)
		g.escalate(ctx, a, r, "not_understood", in.Text, period, step, nil)
		return r
	}
	r.Text = "Не понял тебя. Я умею вот это:"
	menu(r, a.Role)
	opts := []string{rules.MyPath, rules.TimeOff, rules.AccessRequest, rules.AskMentor}
	if a.Role == actor.Mentor {
		opts = nil
	}
	g.rememberQuestion(ctx, a.UserID, opts)
	return r
}

// clarify asks once which of the close intents was meant (09 §6.4).
func (g *Agent) clarify(ctx context.Context, a actor.Actor, in Input, options []string) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.Unknown)
	var labels []string
	for _, o := range options {
		labels = append(labels, strings.ToLower(strings.TrimSuffix(intentLabels[o], "?")))
	}
	r.Text = "Я правильно понял, ты хочешь: " + strings.Join(labels, " или ") + "?"
	for _, o := range options {
		reply.Quick(r, intentLabels[o], "clarify:"+o)
	}
	reply.Quick(r, "Другое", "intent:help")
	g.rememberQuestion(ctx, a.UserID, options)
	return r
}

func (g *Agent) rememberQuestion(ctx context.Context, userID string, options []string) {
	if len(options) == 0 {
		return
	}
	b, _ := json.Marshal(options)
	g.Redis.Set(ctx, lbqKey(userID), b, contextTTL)
}

func (g *Agent) forgetQuestion(ctx context.Context, userID string) { g.Redis.Del(ctx, lbqKey(userID)) }

// answerToQuestion maps "1", "2" or the text of an option to its intent.
func (g *Agent) answerToQuestion(ctx context.Context, userID, text string) string {
	b, err := g.Redis.Get(ctx, lbqKey(userID)).Bytes()
	if err != nil {
		return ""
	}
	var opts []string
	if json.Unmarshal(b, &opts) != nil {
		return ""
	}
	t := strings.TrimSpace(strings.Trim(strings.TrimSpace(text), ".!?"))
	if n, err := strconv.Atoi(t); err == nil && n >= 1 && n <= len(opts) {
		g.forgetQuestion(ctx, userID)
		return opts[n-1]
	}
	norm := strings.Join(rules.Tokens(t), " ")
	if i := slices.IndexFunc(opts, func(o string) bool { return strings.Join(rules.Tokens(intentLabels[o]), " ") == norm }); i >= 0 {
		g.forgetQuestion(ctx, userID)
		return opts[i]
	}
	return ""
}

func isNotFound(err error) bool {
	var te *mcpclient.ToolError
	if errors.As(err, &te) {
		return te.Code == "NOT_FOUND"
	}
	return errors.Is(err, guard.ErrNotFound)
}
