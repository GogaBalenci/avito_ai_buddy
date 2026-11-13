package router

import (
	"context"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/uuid"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
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
