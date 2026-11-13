package router

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

// hasEmoji reports a pictograph (emoji block or misc symbols).
func hasEmoji(s string) bool {
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || unicode.Is(unicode.So, r) {
			return true
		}
	}
	return false
}

func emojiCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) {
			n++
		}
	}
	return n
}

func TestPickIsDeterministic(t *testing.T) {
	if pick(uuid.New(), nil) != "" {
		t.Fatal("no variants → empty")
	}
	id := uuid.MustParse("6f1c2b8e-1d0a-4c55-9a3b-0e7d5f4a2c11")
	first := pick(id, notUnderstood)
	for i := 0; i < 5; i++ {
		if pick(id, notUnderstood) != first {
			t.Fatal("the same id must give the same variant")
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		seen[pick(uuid.New(), notUnderstood)] = true
	}
	if len(seen) != len(notUnderstood) {
		t.Fatalf("every variant is reachable: %d of %d", len(seen), len(notUnderstood))
	}
}

// AG-27: tone rules of the templates themselves.
func TestVoiceTemplatesTone(t *testing.T) {
	for _, v := range notUnderstood {
		if emojiCount(v) > 1 || strings.Count(v, ".")+strings.Count(v, "?")+strings.Count(v, "!") > 2 {
			t.Errorf("at most one emoji and two sentences: %q", v)
		}
	}
	for _, v := range notUnderstoodAgain {
		if hasEmoji(v) || strings.Contains(v, "Гав") {
			t.Errorf("no jokes in the second «not understood»: %q", v)
		}
	}
}

// AG-27: the first and second «not understood» come in Vitya's voice; a repeat
// of the same message id gives the same text.
func TestNotUnderstoodVoice(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := uuid.New()
	out, err := e.agent.Handle(ctx, intern, Input{ClientMessageID: id, Text: "ыыы"})
	if err != nil {
		t.Fatal(err)
	}
	r := out.Reply
	if r.Intent != "unknown" || !slices.Contains(notUnderstood, r.Text) || r.MascotState != api.MascotStateHello || len(r.Actions) != 4 {
		t.Fatalf("first: %+v", r)
	}
	e.agent.Redis.Del(ctx, unkKey(intern.UserID)) // as if it were the first time again
	again, _ := e.agent.Handle(ctx, intern, Input{ClientMessageID: id, Text: "ыыы"})
	if again.Reply.Text != r.Text {
		t.Fatalf("same id → same variant: %q vs %q", again.Reply.Text, r.Text)
	}
	second, _ := e.agent.Handle(ctx, intern, Input{ClientMessageID: uuid.New(), Text: "ыыы"})
	if !slices.Contains(notUnderstoodAgain, second.Reply.Text) || second.Reply.Escalation == nil || hasEmoji(second.Reply.Text) {
		t.Fatalf("second: %+v", second.Reply)
	}
}

// AG-29: chit-chat gets a template of its kind plus the intern's next step;
// a repeat of the same message gives the same variant.
func TestSmallTalkReplies(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.path(pathOut(false, step{"be-1.1", "week1", "explain", "done", "Первый"}, step{"be-1.2", "week1", "explain", "not_started", "Каналы команды"}))
	for _, text := range []string{"какую музыку ты слушаешь", "спасибо", "привет", "ты кто?", "пока", "как дела", "ты молодец", "пошути", "что ты любишь"} {
		id := uuid.New()
		out, err := e.agent.Handle(ctx, intern, Input{ClientMessageID: id, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		r := out.Reply
		if r.Intent != "help" || !strings.Contains(r.Text, "А у тебя, кстати, следующий шаг — «Каналы команды».") ||
			len(r.Actions) != 1 || *r.Actions[0].Payload != "step:be-1.2" || r.MascotState != api.MascotStateHello {
			t.Fatalf("%q: %+v", text, r)
		}
		again, _ := e.agent.Handle(ctx, intern, Input{ClientMessageID: id, Text: text})
		if again.Reply.Text != r.Text {
			t.Fatalf("%q: same id → same variant", text)
		}
	}
	for kind, vs := range smallTalkReplies {
		if len(vs) < 3 {
			t.Errorf("%s: need several variants", kind)
		}
		for _, v := range vs {
			if emojiCount(v) > 1 {
				t.Errorf("%s: at most one emoji: %q", kind, v)
			}
		}
	}
	if r := e.agent.smallTalk(ctx, intern, Input{ClientMessageID: uuid.New()}, "unknown-kind"); !strings.Contains(r.Text, "Я Витя") {
		t.Fatalf("unknown kind → help: %+v", r)
	}
}

func TestSmallTalkBackToWork(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	say := func(a actor.Actor) *api.AgentReply {
		out, err := e.agent.Handle(ctx, a, Input{ClientMessageID: uuid.New(), Text: "спасибо"})
		if err != nil {
			t.Fatal(err)
		}
		return out.Reply
	}
	e.path(pathOut(false, step{"be-1.1", "week1", "explain", "done", "Первый"}))
	if r := say(intern); !strings.Contains(r.Text, "все шаги пройдены") || len(r.Actions) != 0 {
		t.Fatalf("finished: %+v", r)
	}
	e.path(pathOut(false, step{"be-1.1", "week1", "explain", "done", "Первый"}, step{"be-1.2", "week1", "access_request", "waiting", "Доступ"}))
	if r := say(intern); !strings.Contains(r.Text, "ждём ответа по «Доступ»") || *r.Actions[0].Payload != "intent:my_path" {
		t.Fatalf("only_waiting: %+v", r)
	}
	e.clock.Set(testkit.Today.AddDate(0, 0, -10))
	e.path(pathOut(false, step{"be-1.1", "week1", "track_only", "not_started", "Первый"}))
	if r := say(intern); !strings.Contains(r.Text, "стажировка начнётся 07.11") {
		t.Fatalf("pre_start: %+v", r)
	}
	e.tools.On("checklist.get_path", func(context.Context, map[string]any) (any, error) { return nil, errors.New("down") })
	if r := say(intern); !strings.HasSuffix(r.Text, "Чем помочь?") || len(r.Actions) != 4 {
		t.Fatalf("path down → menu: %+v", r)
	}
	mentor := actor.Actor{UserID: "u-mentor", Role: actor.Mentor}
	if r := say(mentor); !strings.Contains(r.Text, "«Мои стажёры» и «Входящие»") || len(r.Actions) != 2 {
		t.Fatalf("mentor: %+v", r)
	}
}
