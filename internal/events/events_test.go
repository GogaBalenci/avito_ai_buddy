package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit/pgtest"
)

func TestPublishFillsCommonFields(t *testing.T) {
	pool, _ := pgtest.New(t)
	clk := testkit.NewClock(testkit.Today)
	p := &events.PG{Pool: pool, Clock: clk}
	ctx := reqctx.WithRequestID(context.Background(), "req-1")
	ctx = reqctx.WithActor(ctx, actor.Actor{UserID: "u-intern-be", Role: "intern"})
	ctx = reqctx.WithSessionID(ctx, "s-1")
	if err := p.Publish(ctx, events.Event{Name: "path_viewed", Mode: "demo", Payload: map[string]any{"day": 4}}); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return events.PublishTx(ctx, tx, clk, events.Event{Name: "doc_conflict_reported", Payload: map[string]any{"reply_id": "r1"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	// AC-K23: the unique index keeps doc_conflict_reported once per reply
	_ = p.Publish(ctx, events.Event{Name: "doc_conflict_reported", Payload: map[string]any{"reply_id": "r1"}})
	var n int
	var rid, uid, role, sid string
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM app.events WHERE name='doc_conflict_reported'`).Scan(&n)
	_ = pool.QueryRow(ctx, `SELECT request_id, user_id, role, session_id FROM app.events WHERE name='path_viewed'`).Scan(&rid, &uid, &role, &sid)
	if n != 1 || rid != "req-1" || uid != "u-intern-be" || role != "intern" || sid != "s-1" {
		t.Fatalf("n=%d %s %s %s %s", n, rid, uid, role, sid)
	}
	if err := p.Publish(ctx, events.Event{Name: "x", Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("unmarshalable payload")
	}
	r := &events.Recorder{}
	_ = r.Publish(ctx, events.Event{Name: "a"})
	if len(r.Events("a")) != 1 || len(r.Events("")) != 1 || r.Events("a")[0].UserID != "u-intern-be" {
		t.Fatal("recorder")
	}
}

func TestFlagsPG(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `INSERT INTO app.feature_flags (name, value, allowed_values, description, updated_at) VALUES ('llm_enabled','on',ARRAY['on','off'],'d',now())`)
	p := &flags.PG{Pool: pool}
	if !flags.On(ctx, p, flags.LLMEnabled) || flags.On(ctx, p, flags.LLMRephrase) {
		t.Fatal("On")
	}
	if _, err := p.Get(ctx, "nope"); !errors.Is(err, flags.ErrUnknown) {
		t.Fatal(err)
	}
	if v, _ := (flags.Static{"a": "on"}).Get(ctx, "a"); v != "on" {
		t.Fatal("static")
	}
	if _, err := (flags.Static{}).Get(ctx, "a"); err == nil {
		t.Fatal("static unknown")
	}
}
