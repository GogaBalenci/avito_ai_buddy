package absences_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/absences"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
	"github.com/GogaBalenci/avito_ai_buddy/internal/timeoff/rules"
)

var (
	intern = map[string]any{"user_id": "u-intern-be", "role": "intern"}
	fe     = map[string]any{"user_id": "u-intern-fe", "role": "intern"}
	mentor = map[string]any{"user_id": "u-mentor", "role": "mentor"}
)

type out struct {
	Request struct {
		RequestID   string `json:"request_id"`
		EmployeeID  string `json:"employee_id"`
		Status      string `json:"status"`
		WorkingDays int    `json:"working_days"`
	} `json:"request"`
	Replay bool `json:"idempotent_replay"`
}

func setup(t *testing.T) (*mcpclient.MCP, *mockstest.Env) {
	env := mockstest.Start(t, testkit.NewClock(testkit.Today))
	return &mcpclient.MCP{URLs: env.URLs(), Timeout: 5 * time.Second}, env
}

func create(c *mcpclient.MCP, actor map[string]any, key, from, to string, o *out) error {
	return c.Call(context.Background(), "absences", "create_request", map[string]any{"actor": actor, "idempotency_key": key,
		"params_hash": rules.ParamsHash(from, to), "type": "time_off", "date_from": from, "date_to": to}, o)
}

// M-01, M-02, M-03
func TestCreateIdempotent(t *testing.T) {
	c, env := setup(t)
	key := uuid.NewString()
	var o out
	if err := create(c, intern, key, "2026-11-13", "2026-11-13", &o); err != nil || o.Request.RequestID != "ABS-000102" || o.Request.Status != "registered" || o.Replay {
		t.Fatalf("M-01: %v %+v", err, o)
	}
	if err := create(c, intern, key, "2026-11-13", "2026-11-13", &o); err != nil || !o.Replay || o.Request.RequestID != "ABS-000102" {
		t.Fatalf("M-02: %v %+v", err, o)
	}
	if err := create(c, intern, key, "2026-11-16", "2026-11-16", nil); !mcpclient.IsCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("M-03: %v", err)
	}
	var n int
	_ = env.Pool.QueryRow(context.Background(), `SELECT count(*) FROM mock_absences.requests WHERE idempotency_key = $1`, key).Scan(&n)
	if n != 1 {
		t.Fatal("rows", n)
	}
}

// M-04: 10 parallel calls with one key → one row.
func TestParallelSameKey(t *testing.T) {
	c, env := setup(t)
	key := uuid.NewString()
	var wg sync.WaitGroup
	var mu sync.Mutex
	replays, errs := 0, 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var o out
			err := create(c, intern, key, "2026-11-13", "2026-11-13", &o)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
				t.Errorf("err %v", err)
			} else if o.Replay {
				replays++
			}
		}()
	}
	wg.Wait()
	var n int
	_ = env.Pool.QueryRow(context.Background(), `SELECT count(*) FROM mock_absences.requests WHERE idempotency_key = $1`, key).Scan(&n)
	if n != 1 || replays != 9 || errs != 0 {
		t.Fatalf("rows %d replays %d errs %d", n, replays, errs)
	}
}

// M-05: two different drafts with overlapping dates in parallel → one request + one OVERLAP.
func TestParallelOverlap(t *testing.T) {
	c, _ := setup(t)
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = create(c, intern, uuid.NewString(), "2026-11-17", "2026-11-18", nil)
		}(i)
	}
	wg.Wait()
	ok, overlap := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case mcpclient.IsCode(err, "OVERLAP"):
			overlap++
		}
	}
	if ok != 1 || overlap != 1 {
		t.Fatalf("%v", results)
	}
}

// M-06, M-07, M-08
func TestCreateRejected(t *testing.T) {
	c, _ := setup(t)
	if err := create(c, mentor, uuid.NewString(), "2026-11-13", "2026-11-13", nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("mentor: %v", err)
	}
	if err := create(c, map[string]any{"user_id": "svc-buddy", "role": "system"}, uuid.NewString(), "2026-11-13", "2026-11-13", nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("system: %v", err)
	}
	for _, d := range [][2]string{{"2026-11-09", "2026-11-09"}, {"2026-11-14", "2026-11-14"}, {"2026-11-16", "2026-11-23"}, {"2026-11-13", "2026-11-12"}} {
		err := create(c, intern, uuid.NewString(), d[0], d[1], nil)
		te, _ := mcpclient.AsToolError(err)
		if te == nil || te.Code != "VALIDATION_FAILED" || te.Details["violations"] == nil {
			t.Fatalf("M-08 %v: %v", d, err)
		}
	}
	// seed ABS-000101 is Mon–Tue three weeks ahead: overlap
	from, to := rules.SeedDates(testkit.Today)
	err := create(c, intern, uuid.NewString(), from.Format("2006-01-02"), to.Format("2006-01-02"), nil)
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "OVERLAP" {
		t.Fatalf("overlap with seed: %v", err)
	}
}

// M-09, M-10 and the flag only affecting writes.
func TestFault(t *testing.T) {
	c, env := setup(t)
	ctx := context.Background()
	if _, err := env.Mocks.Flags.Set(ctx, "mock_absences_fault", "unavailable"); err != nil {
		t.Fatal(err)
	}
	err := create(c, intern, uuid.NewString(), "2026-11-13", "2026-11-13", nil)
	te, _ := mcpclient.AsToolError(err)
	if te == nil || te.Code != "UNAVAILABLE" || !te.Retryable {
		t.Fatalf("M-09: %v", err)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern}, nil); err != nil {
		t.Fatalf("reads still work: %v", err)
	}
	old := absences.SlowDelay
	absences.SlowDelay = 400 * time.Millisecond
	t.Cleanup(func() { absences.SlowDelay = old })
	_, _ = env.Mocks.Flags.Set(ctx, "mock_absences_fault", "slow")
	c.Timeout = 200 * time.Millisecond
	if err := create(c, intern, uuid.NewString(), "2026-11-13", "2026-11-13", nil); !mcpclient.IsCode(err, mcpclient.CodeTimeout) {
		t.Fatalf("M-10: %v", err)
	}
}

// M-11, M-12, M-13
func TestList(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	_ = create(c, intern, uuid.NewString(), "2026-11-13", "2026-11-13", nil)
	var l struct {
		Items []struct {
			RequestID string `json:"request_id"`
			DateFrom  string `json:"date_from"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern}, &l); err != nil || l.Total != 2 || l.Items[0].RequestID != "ABS-000102" {
		t.Fatalf("M-11: %v %+v", err, l)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern, "employee_id": "u-intern-fe"}, nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
		t.Fatalf("foreign: %v", err)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": mentor, "employee_id": "u-intern-be"}, &l); err != nil || l.Total != 2 {
		t.Fatalf("M-12 mentor: %v", err)
	}
	for _, in := range []map[string]any{{"actor": mentor}, {"actor": mentor, "employee_id": "u-intern-other"}, {"actor": map[string]any{"user_id": "svc-x", "role": "system"}}} {
		if err := c.Call(ctx, "absences", "list_requests", in, nil); !mcpclient.IsCode(err, "PERMISSION_DENIED") {
			t.Fatalf("M-12 %v: %v", in, err)
		}
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern, "date_to_gte": "2026-11-20"}, &l); err != nil || l.Total != 1 {
		t.Fatalf("M-13 date filter: %v %+v", err, l)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern, "statuses": []string{"cancelled"}}, &l); err != nil || l.Total != 0 {
		t.Fatalf("M-13 statuses: %v", err)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": fe}, &l); err != nil || l.Total != 0 {
		t.Fatalf("fe has none: %v", err)
	}
	if err := c.Call(ctx, "absences", "list_requests", map[string]any{"actor": intern, "limit": 51}, nil); !mcpclient.IsCode(err, mcpclient.CodeInternal) {
		t.Fatalf("M-13 limit 51 is refused by the contract before sending: %v", err)
	}
}
