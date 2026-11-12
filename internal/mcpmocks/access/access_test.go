package access_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/access"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

var (
	intern = map[string]any{"user_id": "u-intern-be", "role": "intern"}
	fe     = map[string]any{"user_id": "u-intern-fe", "role": "intern"}
	mentor = map[string]any{"user_id": "u-mentor", "role": "mentor"}
	other  = map[string]any{"user_id": "u-mentor-other", "role": "mentor"}
	system = map[string]any{"user_id": "svc-buddy", "role": "system"}
)

type request struct {
	RequestID     string     `json:"request_id"`
	EmployeeID    string     `json:"employee_id"`
	ResourceID    string     `json:"resource_id"`
	Status        string     `json:"status"`
	Policy        string     `json:"policy"`
	CreatedAt     time.Time  `json:"created_at"`
	AutoApproveAt *time.Time `json:"auto_approve_at"`
	DecidedAt     *time.Time `json:"decided_at"`
	DecidedBy     *string    `json:"decided_by"`
	RejectReason  *string    `json:"reject_reason"`
}

type one struct {
	Request request `json:"request"`
	Replay  bool    `json:"idempotent_replay"`
}

type list struct {
	Items    []request `json:"items"`
	Total    int       `json:"total"`
	Approved []string  `json:"auto_approved_now"`
}

type env struct {
	c   *mcpclient.MCP
	e   *mockstest.Env
	clk *testkit.Clock
}

func setup(t *testing.T) *env {
	clk := testkit.NewClock(testkit.Today)
	e := mockstest.Start(t, clk)
	return &env{c: &mcpclient.MCP{URLs: e.URLs(), Timeout: 5 * time.Second}, e: e, clk: clk}
}

func (e *env) create(actor map[string]any, key, resource string) (one, error) {
	var o one
	err := e.c.Call(context.Background(), "access", "create_request", map[string]any{"actor": actor, "idempotency_key": key,
		"params_hash": access.ParamsHash(resource), "resource_id": resource, "justification": "Запрос из чата бадди"}, &o)
	return o, err
}

func (e *env) list(actor map[string]any, args map[string]any) (list, error) {
	var l list
	in := map[string]any{"actor": actor}
	for k, v := range args {
		in[k] = v
	}
	err := e.c.Call(context.Background(), "access", "list_requests", in, &l)
	return l, err
}

func (e *env) decide(actor map[string]any, key, id, decision string, reason *string) (one, error) {
	var o one
	in := map[string]any{"actor": actor, "idempotency_key": key, "request_id": id, "decision": decision}
	if reason != nil {
		in["reject_reason"] = *reason
	}
	err := e.c.Call(context.Background(), "access", "decide_request", in, &o)
	return o, err
}

func ptr(s string) *string { return &s }

func schemaErr(err error) bool {
	return err != nil && (mcpclient.IsCode(err, "VALIDATION_FAILED") || strings.Contains(err.Error(), "violates contract"))
}

// XM-01
func TestListResources(t *testing.T) {
	e := setup(t)
	var out struct {
		Items []access.Resource `json:"items"`
		Total int               `json:"total"`
	}
	if err := e.c.Call(context.Background(), "access", "list_resources", map[string]any{"actor": intern}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 7 {
		t.Fatalf("total %d", out.Total)
	}
	for _, r := range out.Items {
		if r.ResourceID == "prod-db-write" {
			if r.Allowed || r.DenyExplanation == nil || r.DenyReasonCode == nil || *r.DenyReasonCode != "ROLE_NOT_ALLOWED" {
				t.Fatalf("prod-db-write: %+v", r)
			}
			continue
		}
		if !r.Allowed || r.Approver == nil {
			t.Fatalf("%s: %+v", r.ResourceID, r)
		}
		if r.ResourceID == "channel-tech-dep" && (r.TimerMinutes == nil || *r.TimerMinutes != 2) {
			t.Fatalf("timer minutes: %+v", r)
		}
	}
	if err := e.c.Call(context.Background(), "access", "list_resources", map[string]any{"actor": intern, "include_denied": false}, &out); err != nil || out.Total != 6 {
		t.Fatalf("include_denied=false: %v %d", err, out.Total)
	}
	if err := e.c.Call(context.Background(), "access", "list_resources", map[string]any{"actor": mentor}, &out); err != nil {
		t.Fatal(err)
	}
	for _, r := range out.Items {
		if r.Allowed {
			t.Fatalf("a mentor may request nothing: %+v", r)
		}
	}
}

// XM-02, XM-03
func TestCreatePolicies(t *testing.T) {
	e := setup(t)
	o, err := e.create(intern, uuid.NewString(), "team-channels")
	if err != nil || o.Request.Status != "approved" || *o.Request.DecidedBy != "auto" || o.Request.RequestID != "ACC-000102" || o.Replay {
		t.Fatalf("auto: %v %+v", err, o)
	}
	o, err = e.create(intern, uuid.NewString(), "channel-tech-dep")
	if err != nil || o.Request.Status != "pending" || o.Request.AutoApproveAt == nil ||
		!o.Request.AutoApproveAt.Equal(o.Request.CreatedAt.Add(2*time.Minute)) {
		t.Fatalf("timer: %v %+v", err, o)
	}
	o, err = e.create(fe, uuid.NewString(), "figma-library")
	if err != nil || o.Request.Status != "pending" || o.Request.Policy != "mentor" || o.Request.AutoApproveAt != nil {
		t.Fatalf("mentor: %v %+v", err, o)
	}
	_, err = e.create(intern, uuid.NewString(), "prod-db-write")
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "PERMISSION_DENIED" || te.Details["reason"] != "ROLE_NOT_ALLOWED" {
		t.Fatalf("XM-03 prod: %v", err)
	}
	if _, err := e.create(mentor, uuid.NewString(), "team-channels"); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("XM-03 mentor: %v", err)
	}
	if _, err := e.create(intern, uuid.NewString(), "nope-resource"); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("XM-03 unknown: %v", err)
	}
}

// XM-04, XM-15
func TestIdempotency(t *testing.T) {
	e := setup(t)
	key := uuid.NewString()
	a, _ := e.create(intern, key, "team-channels")
	b, err := e.create(intern, key, "team-channels")
	if err != nil || !b.Replay || b.Request.RequestID != a.Request.RequestID {
		t.Fatalf("replay: %v %+v", err, b)
	}
	if _, err := e.create(intern, key, "channel-tech-dep"); !mcpclient.IsCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("conflict: %v", err)
	}
	key2 := uuid.NewString()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := e.create(fe, key2, "repo-frontend")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("parallel: %v", err)
				return
			}
			ids[o.Request.RequestID] = true
		}()
	}
	wg.Wait()
	var n int
	_ = e.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM mock_access.requests WHERE idempotency_key = $1`, key2).Scan(&n)
	if n != 1 || len(ids) != 1 {
		t.Fatalf("rows %d ids %v", n, ids)
	}
}

// XM-05, XM-06
func TestDuplicateActive(t *testing.T) {
	e := setup(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, dup := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.create(fe, uuid.NewString(), "figma-library")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case mcpclient.IsCode(err, "DUPLICATE_ACTIVE"):
				dup++
			default:
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || dup != 1 {
		t.Fatalf("ok %d dup %d", ok, dup)
	}
	a, _ := e.create(intern, uuid.NewString(), "team-channels")
	_, err := e.create(intern, uuid.NewString(), "team-channels")
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "DUPLICATE_ACTIVE" || te.Details["existing"].(map[string]any)["request_id"] != a.Request.RequestID {
		t.Fatalf("XM-06 approved: %v", err)
	}
	// rejected → a new request is allowed
	l, _ := e.list(fe, nil)
	if _, err := e.decide(mentor, uuid.NewString(), l.Items[0].RequestID, "reject", ptr("not_needed")); err != nil {
		t.Fatal(err)
	}
	if o, err := e.create(fe, uuid.NewString(), "figma-library"); err != nil || o.Request.Status != "pending" {
		t.Fatalf("after rejected: %v %+v", err, o)
	}
}

// XM-07, XM-08
func TestTimerAndFlag(t *testing.T) {
	e := setup(t)
	o, _ := e.create(intern, uuid.NewString(), "channel-tech-dep")
	e.clk.Advance(2*time.Minute - time.Second)
	l, _ := e.list(intern, map[string]any{"request_ids": []string{o.Request.RequestID}})
	if l.Items[0].Status != "pending" || len(l.Approved) != 0 {
		t.Fatalf("1:59: %+v", l)
	}
	e.clk.Advance(time.Second)
	l, _ = e.list(intern, map[string]any{"request_ids": []string{o.Request.RequestID}})
	if l.Items[0].Status != "approved" || *l.Items[0].DecidedBy != "timer" || !l.Items[0].DecidedAt.Equal(o.Request.CreatedAt.Add(2*time.Minute)) ||
		len(l.Approved) != 1 || l.Approved[0] != o.Request.RequestID {
		t.Fatalf("2:00: %+v", l)
	}
	l, _ = e.list(intern, nil)
	if len(l.Approved) != 0 {
		t.Fatalf("second read: %+v", l.Approved)
	}
	// the seeded ACC-000101 (mentor, yesterday) is approved at once when the flag is off
	if _, err := e.e.Mocks.Flags.Set(context.Background(), "access_mentor_approval", "off"); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Items []access.Resource `json:"items"`
	}
	_ = e.c.Call(context.Background(), "access", "list_resources", map[string]any{"actor": intern}, &out)
	for _, r := range out.Items {
		if r.ResourceID == "space-cluster" && r.EffectivePolicy != "timer" {
			t.Fatalf("effective policy: %+v", r)
		}
	}
	l, _ = e.list(intern, map[string]any{"request_ids": []string{"ACC-000101"}})
	if l.Items[0].Status != "approved" || *l.Items[0].DecidedBy != "timer" || l.Items[0].Policy != "timer" {
		t.Fatalf("XM-08: %+v", l.Items[0])
	}
}

// XM-09, XM-10, XM-11
func TestDecide(t *testing.T) {
	e := setup(t)
	key := uuid.NewString()
	o, err := e.decide(mentor, key, "ACC-000101", "approve", nil)
	if err != nil || o.Request.Status != "approved" || *o.Request.DecidedBy != "mentor" || o.Replay {
		t.Fatalf("approve: %v %+v", err, o)
	}
	if o, err = e.decide(mentor, key, "ACC-000101", "approve", nil); err != nil || !o.Replay {
		t.Fatalf("replay: %v %+v", err, o)
	}
	if _, err := e.decide(mentor, key, "ACC-000101", "reject", ptr("not_needed")); !mcpclient.IsCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("key conflict: %v", err)
	}
	if _, err := e.decide(mentor, uuid.NewString(), "ACC-000101", "reject", ptr("not_needed")); !mcpclient.IsCode(err, "ALREADY_DECIDED") {
		t.Fatalf("XM-10: %v", err)
	}
	// schema: reject without reason, approve with a reason
	// the schema is checked by the client before the call (ToolClient) and by the mock
	if _, err := e.decide(mentor, uuid.NewString(), "ACC-000101", "reject", nil); !schemaErr(err) {
		t.Fatalf("XM-09 reject without reason: %v", err)
	}
	if _, err := e.decide(mentor, uuid.NewString(), "ACC-000101", "approve", ptr("not_needed")); !schemaErr(err) {
		t.Fatalf("XM-09 approve with reason: %v", err)
	}
	// reject
	f, _ := e.create(fe, uuid.NewString(), "figma-library")
	if o, err = e.decide(mentor, uuid.NewString(), f.Request.RequestID, "reject", ptr("discuss_1on1")); err != nil ||
		o.Request.Status != "rejected" || *o.Request.RejectReason != "discuss_1on1" {
		t.Fatalf("reject: %v %+v", err, o)
	}
	// the timer was faster than the mentor
	tm, _ := e.create(intern, uuid.NewString(), "channel-tech-dep")
	e.clk.Advance(3 * time.Minute)
	_, err = e.decide(mentor, uuid.NewString(), tm.Request.RequestID, "approve", nil)
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "ALREADY_DECIDED" || te.Details["request"].(map[string]any)["decided_by"] != "timer" {
		t.Fatalf("timer first: %v", err)
	}
	// XM-11
	g, _ := e.create(fe, uuid.NewString(), "figma-library")
	if _, err := e.decide(other, uuid.NewString(), g.Request.RequestID, "approve", nil); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("foreign mentor: %v", err)
	}
	if _, err := e.decide(intern, uuid.NewString(), g.Request.RequestID, "approve", nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("intern: %v", err)
	}
	if _, err := e.decide(mentor, uuid.NewString(), "ACC-999999", "approve", nil); !mcpclient.IsCode(err, "NOT_FOUND") {
		t.Fatalf("missing: %v", err)
	}
}

// XM-10: approve and reject racing → one decision.
func TestDecideRace(t *testing.T) {
	e := setup(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, already := 0, 0
	for _, d := range []string{"approve", "reject", "approve", "reject"} {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			var reason *string
			if d == "reject" {
				reason = ptr("other_resource")
			}
			_, err := e.decide(mentor, uuid.NewString(), "ACC-000101", d, reason)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case mcpclient.IsCode(err, "ALREADY_DECIDED"):
				already++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(d)
	}
	wg.Wait()
	if ok != 1 || already != 3 {
		t.Fatalf("ok %d already %d", ok, already)
	}
}

// XM-12
func TestListScopes(t *testing.T) {
	e := setup(t)
	_, _ = e.create(fe, uuid.NewString(), "repo-frontend")
	if l, err := e.list(intern, nil); err != nil || l.Total != 1 || l.Items[0].RequestID != "ACC-000101" {
		t.Fatalf("intern: %v %+v", err, l)
	}
	if _, err := e.list(intern, map[string]any{"employee_id": "u-intern-fe"}); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("foreign: %v", err)
	}
	if l, err := e.list(mentor, nil); err != nil || l.Total != 2 {
		t.Fatalf("mentor: %v %+v", err, l)
	}
	if l, err := e.list(mentor, map[string]any{"employee_id": "u-intern-fe", "statuses": []string{"approved"}}); err != nil || l.Total != 1 {
		t.Fatalf("mentor filtered: %v %+v", err, l)
	}
	if _, err := e.list(other, map[string]any{"employee_id": "u-intern-be"}); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("foreign mentor: %v", err)
	}
	if _, err := e.list(system, nil); !mcpclient.IsCode(err, "VALIDATION_FAILED") {
		t.Fatalf("system without employee: %v", err)
	}
	if l, err := e.list(system, map[string]any{"employee_id": "u-intern-be"}); err != nil || l.Total != 1 {
		t.Fatalf("system: %v %+v", err, l)
	}
	if _, err := e.list(map[string]any{"user_id": "u-x", "role": "guest"}, nil); err == nil {
		t.Fatal("unknown role")
	}
}

func TestParamsHashStable(t *testing.T) {
	h1, h2 := access.ParamsHash("team-channels"), access.ParamsHash("team-channels")
	if h1 != h2 || access.ParamsHash("a") == access.ParamsHash("b") ||
		len(access.ParamsHash("x")) != 64 {
		t.Fatal("params hash")
	}
}
