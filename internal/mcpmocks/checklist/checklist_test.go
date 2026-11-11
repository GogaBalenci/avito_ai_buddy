package checklist_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

var (
	intern = map[string]any{"user_id": "u-intern-be", "role": "intern"}
	fe     = map[string]any{"user_id": "u-intern-fe", "role": "intern"}
	mentor = map[string]any{"user_id": "u-mentor", "role": "mentor"}
	sys    = map[string]any{"user_id": "svc-buddy", "role": "system"}
)

func setup(t *testing.T) *mcpclient.MCP {
	env := mockstest.Start(t, testkit.NewClock(testkit.Today))
	return &mcpclient.MCP{URLs: env.URLs(), Timeout: 5 * time.Second}
}

type step struct {
	StepID string  `json:"step_id"`
	Owner  string  `json:"owner"`
	Status string  `json:"status"`
	Source *string `json:"status_source"`
}

type path struct {
	EmployeeID string `json:"employee_id"`
	Track      struct {
		ID        string `json:"id"`
		Synthetic bool   `json:"synthetic"`
	} `json:"track"`
	Steps []step `json:"steps"`
}

// C-01
func TestGetPathIntern(t *testing.T) {
	c := setup(t)
	var p path
	if err := c.Call(context.Background(), "checklist", "get_path", map[string]any{"actor": intern}, &p); err != nil {
		t.Fatal(err)
	}
	if p.EmployeeID != "u-intern-be" || p.Track.ID != "backend" || len(p.Steps) != 41 {
		t.Fatalf("path: %s %s %d", p.EmployeeID, p.Track.ID, len(p.Steps))
	}
	status := map[string]string{}
	for _, s := range p.Steps {
		if s.Owner != "intern" {
			t.Fatalf("owner %s visible", s.Owner)
		}
		status[s.StepID] = s.Status
	}
	if status["cm-1.10"] != "waiting" || status["be-1.11"] != "in_progress" || status["be-1.12"] != "not_started" {
		t.Fatalf("seeds: %v", status)
	}
	var pf path
	if err := c.Call(context.Background(), "checklist", "get_path", map[string]any{"actor": mentor, "employee_id": "u-intern-fe"}, &pf); err != nil || !pf.Track.Synthetic {
		t.Fatalf("mentor reads mentee: %v %+v", err, pf.Track)
	}
}

// C-02
func TestGetPathDenied(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	cases := []map[string]any{
		{"actor": intern, "employee_id": "u-intern-fe"},
		{"actor": mentor},
		{"actor": mentor, "employee_id": "u-intern-other"},
		{"actor": intern, "owners": []string{"mentor"}},
		{"actor": sys, "employee_id": "u-intern-be"},
	}
	for i, in := range cases {
		if err := c.Call(ctx, "checklist", "get_path", in, nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
			t.Errorf("case %d: %v", i, err)
		}
	}
	if err := c.Call(ctx, "checklist", "get_path", map[string]any{"actor": mentor, "employee_id": "u-nobody"}, nil); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Errorf("unknown employee: %v", err)
	}
}

func set(c *mcpclient.MCP, actor map[string]any, extra map[string]any) error {
	in := map[string]any{"actor": actor, "idempotency_key": uuid.NewString()}
	for k, v := range extra {
		in[k] = v
	}
	return c.Call(context.Background(), "checklist", "set_step_status", in, nil)
}

// C-03, C-04, C-05, C-06, C-09
func TestSetStepStatus(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	key := uuid.NewString()
	in := map[string]any{"actor": intern, "step_id": "be-1.11", "status": "done", "reason": "user_mark_done", "expected_status": "in_progress", "idempotency_key": key}
	var out struct {
		Step    step `json:"step"`
		Changed bool `json:"changed"`
		Replay  bool `json:"idempotent_replay"`
	}
	if err := c.Call(ctx, "checklist", "set_step_status", in, &out); err != nil || !out.Changed || out.Step.Status != "done" || *out.Step.Source != "user" {
		t.Fatalf("C-03: %v %+v", err, out)
	}
	if err := c.Call(ctx, "checklist", "set_step_status", in, &out); err != nil || !out.Replay {
		t.Fatalf("C-04 replay: %v %+v", err, out)
	}
	in2 := map[string]any{"actor": intern, "step_id": "be-1.11", "status": "in_progress", "reason": "user_reopen", "idempotency_key": key}
	if err := c.Call(ctx, "checklist", "set_step_status", in2, nil); !mcpclient.IsCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("C-04 conflict: %v", err)
	}
	err := set(c, intern, map[string]any{"step_id": "be-1.12", "status": "done", "reason": "user_mark_done", "expected_status": "in_progress"})
	te, _ := mcpclient.AsToolError(err)
	if !mcpclient.IsCode(err, "STATUS_CONFLICT") || te.Details["current_status"] != "not_started" {
		t.Fatalf("C-05: %v", err)
	}
	if err := set(c, intern, map[string]any{"step_id": "cm-1.7", "status": "done", "reason": "user_mark_done"}); !mcpclient.IsCode(err, "TRANSITION_NOT_ALLOWED") {
		t.Fatalf("C-06: %v", err)
	}
	if err := set(c, intern, map[string]any{"step_id": "cm-1.1", "status": "in_progress", "reason": "user_start"}); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("C-09: %v", err)
	}
	if err := set(c, intern, map[string]any{"step_id": "fe-w1-10", "status": "in_progress", "reason": "user_start"}); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("other track: %v", err)
	}
	// no-op: start an already in-progress step
	var noop struct {
		Changed bool `json:"changed"`
	}
	_ = set(c, intern, map[string]any{"step_id": "be-1.12", "status": "in_progress", "reason": "user_start"})
	if err := c.Call(ctx, "checklist", "set_step_status", map[string]any{"actor": intern, "step_id": "be-1.12", "status": "in_progress", "reason": "user_start", "idempotency_key": uuid.NewString()}, &noop); err != nil || noop.Changed {
		t.Fatalf("no-op: %v %+v", err, noop)
	}
}

// C-07, C-08 and the system path
func TestSetStepStatusRoles(t *testing.T) {
	c := setup(t)
	if err := set(c, intern, map[string]any{"step_id": "cm-1.10", "status": "done", "reason": "access_approved", "request_ref": "ACC-000101"}); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("C-07 intern access_*: %v", err)
	}
	if err := set(c, sys, map[string]any{"employee_id": "u-intern-be", "step_id": "cm-1.10", "status": "done", "reason": "access_approved"}); !mcpclient.IsCode(err, "VALIDATION_FAILED") {
		t.Fatalf("C-07 system without ref: %v", err)
	}
	if err := set(c, sys, map[string]any{"employee_id": "u-intern-be", "step_id": "be-1.12", "status": "done", "reason": "user_mark_done"}); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("D11 system user_*: %v", err)
	}
	if err := set(c, mentor, map[string]any{"employee_id": "u-intern-be", "step_id": "be-1.12", "status": "in_progress", "reason": "user_start"}); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("C-08: %v", err)
	}
	if err := set(c, sys, map[string]any{"step_id": "cm-1.10", "status": "done", "reason": "access_approved", "request_ref": "ACC-000101"}); !mcpclient.IsCode(err, "VALIDATION_FAILED") {
		t.Fatalf("system without employee: %v", err)
	}
	if err := set(c, sys, map[string]any{"employee_id": "u-intern-be", "step_id": "cm-1.10", "status": "done", "reason": "access_approved", "request_ref": "ACC-000101"}); err != nil {
		t.Fatalf("system approve: %v", err)
	}
	if err := set(c, sys, map[string]any{"employee_id": "u-nobody", "step_id": "cm-1.10", "status": "done", "reason": "access_approved", "request_ref": "ACC-000101"}); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("system unknown employee: %v", err)
	}
}

// C-10: 10 parallel mark-done with different keys → one success.
func TestParallelMarkDone(t *testing.T) {
	c := setup(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, conflicts := 0, 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := set(c, intern, map[string]any{"step_id": "be-1.11", "status": "done", "reason": "user_mark_done", "expected_status": "in_progress"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okN++
			case mcpclient.IsCode(err, "STATUS_CONFLICT"):
				conflicts++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if okN != 1 || conflicts != 9 {
		t.Fatalf("ok %d conflicts %d", okN, conflicts)
	}
}

func TestGetStep(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	var out struct {
		Step step `json:"step"`
	}
	if err := c.Call(ctx, "checklist", "get_step", map[string]any{"actor": fe, "step_id": "fe-w1-50"}, &out); err != nil || out.Step.Status != "not_started" {
		t.Fatalf("%v %+v", err, out)
	}
	if err := c.Call(ctx, "checklist", "get_step", map[string]any{"actor": mentor, "employee_id": "u-intern-fe", "step_id": "fe-w1-10"}, &out); err != nil || out.Step.Status != "done" {
		t.Fatalf("mentor: %v %+v", err, out)
	}
	if err := c.Call(ctx, "checklist", "get_step", map[string]any{"actor": fe, "step_id": "be-1.11"}, nil); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("foreign track: %v", err)
	}
}
