package accesssync_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	accesssync "github.com/GogaBalenci/avito_ai_buddy/internal/access/sync"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/access"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

// faulty wraps the client: checklist calls can fail and are counted.
type faulty struct {
	mcpclient.Client
	checklistDown atomic.Bool
	checklist     atomic.Int32
}

func (f *faulty) Call(ctx context.Context, service, tool string, in, out any) error {
	if service == "checklist" {
		f.checklist.Add(1)
		if f.checklistDown.Load() {
			return &mcpclient.ToolError{Code: mcpclient.CodeUnavailable, Message: "down"}
		}
	}
	return f.Client.Call(ctx, service, tool, in, out)
}

type env struct {
	s     *accesssync.Syncer
	tools *faulty
	rec   *events.Recorder
	clk   *testkit.Clock
	mcp   *mcpclient.MCP
	m     *mockstest.Env
}

func setup(t *testing.T) *env {
	clk := testkit.NewClock(testkit.Today)
	m := mockstest.Start(t, clk)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO app.employees (user_id, display_name, role) VALUES ('u-mentor', 'Наставник', 'mentor')`,
		`INSERT INTO app.employees (user_id, display_name, role, track_id, start_date, mentor_id) VALUES
			('u-intern-be', 'Стажёр Backend', 'intern', 'backend', '2026-11-07', 'u-mentor'),
			('u-intern-fe', 'Стажёр Frontend', 'intern', 'frontend', '2026-10-31', 'u-mentor')`,
	} {
		if _, err := m.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	c := &mcpclient.MCP{URLs: m.URLs(), Timeout: 5 * time.Second}
	f := &faulty{Client: c}
	rec := &events.Recorder{}
	return &env{s: &accesssync.Syncer{Tools: f, Pool: m.Pool, Events: rec, Clock: clk}, tools: f, rec: rec, clk: clk, mcp: c, m: m}
}

func (e *env) create(t *testing.T, user, resource string) accesssync.Request {
	t.Helper()
	var out struct {
		Request accesssync.Request `json:"request"`
	}
	if err := e.mcp.Call(context.Background(), "access", "create_request", map[string]any{
		"actor": map[string]any{"user_id": user, "role": "intern"}, "idempotency_key": uuid.NewString(),
		"params_hash": access.ParamsHash(resource), "resource_id": resource}, &out); err != nil {
		t.Fatal(err)
	}
	return out.Request
}

func (e *env) step(t *testing.T, user, id string) string {
	t.Helper()
	var out struct {
		Step struct {
			Status string `json:"status"`
		} `json:"step"`
	}
	if err := e.mcp.Call(context.Background(), "checklist", "get_step", map[string]any{
		"actor": map[string]any{"user_id": user, "role": "intern"}, "step_id": id}, &out); err != nil {
		t.Fatal(err)
	}
	return out.Step.Status
}

func (e *env) local(t *testing.T, id string) (status string, reason *string) {
	t.Helper()
	if err := e.m.Pool.QueryRow(context.Background(), `SELECT status, reject_reason FROM app.access_requests WHERE request_id = $1`, id).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	return
}

func str(s string) *string { return &s }

// XS-01: pending → approved (timer): step done, copy updated, one event.
func TestApprovedByTimer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.create(t, "u-intern-be", "channel-tech-dep")
	ins, err := e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "Канал Tech-dep", StepID: str("be-1.15")}, r)
	if err != nil || !ins {
		t.Fatalf("created: %v %v", ins, err)
	}
	if st := e.step(t, "u-intern-be", "be-1.15"); st != "waiting" {
		t.Fatalf("step %s", st)
	}
	if ins, _ := e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "x", StepID: str("be-1.15")}, r); ins {
		t.Fatal("the copy is inserted once")
	}
	if err := e.s.Run(ctx, "u-intern-be"); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.local(t, r.RequestID); st != "pending" || len(e.rec.Events("access_request_decided")) != 0 {
		t.Fatal("too early")
	}
	e.clk.Advance(2 * time.Minute)
	if err := e.s.Run(ctx, "u-intern-be"); err != nil {
		t.Fatal(err)
	}
	if st := e.step(t, "u-intern-be", "be-1.15"); st != "done" {
		t.Fatalf("step %s", st)
	}
	ev := e.rec.Events("access_request_decided")
	if st, _ := e.local(t, r.RequestID); st != "approved" || len(ev) != 1 || ev[0].Payload["decided_by"] != "timer" ||
		ev[0].Payload["wait_ms"] != int64(120000) {
		t.Fatalf("copy %s events %+v", st, ev)
	}
	if err := e.s.Run(ctx, "u-intern-be"); err != nil || len(e.rec.Events("access_request_decided")) != 1 {
		t.Fatal("nothing pending: no new events")
	}
	if err := e.s.Run(ctx, "u-nobody"); err != nil {
		t.Fatal(err)
	}
}

// XS-02: 5 parallel synchronizations → one event.
func TestParallelOneEvent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.create(t, "u-intern-be", "channel-tech-dep")
	_, _ = e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "Канал Tech-dep", StepID: str("be-1.15")}, r)
	e.clk.Advance(3 * time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.s.Run(ctx, "u-intern-be"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := len(e.rec.Events("access_request_decided")); n != 1 {
		t.Fatalf("events %d", n)
	}
}

// XS-03: the checklist is down → the copy is unchanged; the next call syncs.
// XS-04: rejected → step in_progress, reason in the copy.
func TestChecklistDownThenRejected(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// the seeded ACC-000101 with its step cm-1.10 (waiting)
	var out struct {
		Items []accesssync.Request `json:"items"`
	}
	if err := e.mcp.Call(ctx, "access", "list_requests", map[string]any{"actor": map[string]any{"user_id": "u-intern-be", "role": "intern"}}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "Пространство", StepID: str("cm-1.10")}, out.Items[0]); err != nil {
		t.Fatal(err)
	}
	if err := e.mcp.Call(ctx, "access", "decide_request", map[string]any{"actor": map[string]any{"user_id": "u-mentor", "role": "mentor"},
		"idempotency_key": uuid.NewString(), "request_id": "ACC-000101", "decision": "reject", "reject_reason": "not_needed"}, nil); err != nil {
		t.Fatal(err)
	}
	e.tools.checklistDown.Store(true)
	if err := e.s.Run(ctx, "u-intern-be"); err == nil {
		t.Fatal("XS-03: an error is expected")
	}
	if st, _ := e.local(t, "ACC-000101"); st != "pending" || len(e.rec.Events("access_request_decided")) != 0 {
		t.Fatalf("XS-03: copy %s", st)
	}
	e.tools.checklistDown.Store(false)
	if err := e.s.Run(ctx, "u-intern-be"); err != nil {
		t.Fatal(err)
	}
	st, reason := e.local(t, "ACC-000101")
	if st != "rejected" || reason == nil || *reason != "not_needed" || e.step(t, "u-intern-be", "cm-1.10") != "in_progress" {
		t.Fatalf("XS-04: %s %v", st, reason)
	}
	ev := e.rec.Events("access_request_decided")
	if len(ev) != 1 || ev[0].Payload["decision"] != "reject" || ev[0].Payload["reject_reason"] != "not_needed" {
		t.Fatalf("event %+v", ev)
	}
}

// XS-05: a request without a step → only the copy and the event.
func TestNoStep(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.create(t, "u-intern-be", "repo-frontend") // auto, not on the Backend path
	ins, err := e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "Репозиторий", ActionID: uuid.NewString()}, r)
	if err != nil || !ins || e.tools.checklist.Load() != 0 {
		t.Fatalf("%v %v calls %d", ins, err, e.tools.checklist.Load())
	}
	ev := e.rec.Events("access_request_decided")
	if len(ev) != 1 || ev[0].Payload["decided_by"] != "auto" || ev[0].Payload["wait_ms"] != int64(0) {
		t.Fatalf("auto event %+v", ev)
	}
}

func TestServiceDown(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.create(t, "u-intern-be", "channel-tech-dep")
	_, _ = e.s.Created(ctx, accesssync.Copy{UserID: "u-intern-be", ResourceTitle: "x", StepID: str("be-1.15")}, r)
	e.s.Tools = mcpclient.NewFake() // nothing registered: the access service is unavailable
	if err := e.s.Run(ctx, "u-intern-be"); err == nil {
		t.Fatal("expected an error")
	}
	e.m.Pool.Close()
	if err := e.s.Run(ctx, "u-intern-be"); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("db down")
	}
}
