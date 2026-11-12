package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

func draft(t *testing.T, c *client, from, to string) api.ActionCreated {
	t.Helper()
	rec := c.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": from, "date_to": to}})
	expect(t, rec, http.StatusCreated)
	return decode[api.ActionCreated](t, rec)
}

// A-06, AC-13, A-11, A-36: draft → confirm → saved copy, one event, same number on repeat.
func TestTimeOffFullFlow(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/time-off/config", nil), http.StatusOK, `"today":"2026-11-10"`, `"max_date":"2027-02-08"`, `"weekend_days":[6,7]`)
	d := draft(t, be, "2026-11-13", "2026-11-13")
	if d.PendingAction.State != "active" || *d.PendingAction.Summary.WorkingDays != 1 || len(d.Notices) != 0 {
		t.Fatalf("draft %+v", d)
	}
	id := d.PendingAction.ActionId.String()
	expect(t, be.do("GET", "/api/actions/"+id, nil), http.StatusOK, `"state":"active"`)
	rec := be.do("POST", "/api/actions/"+id+"/confirm", nil)
	expect(t, rec, http.StatusOK, "ABS-000102", `"mascot_state":"done"`, "Скопировать сообщение наставнику")
	again := be.do("POST", "/api/actions/"+id+"/confirm", nil)
	expect(t, again, http.StatusOK, "ABS-000102")
	if n := len(h.events.Events("action_completed")); n != 1 {
		t.Fatalf("action_completed = %d", n)
	}
	expect(t, be.do("GET", "/api/actions/"+id, nil), http.StatusOK, `"state":"confirmed"`, "ABS-000102")
	var n int
	_ = h.srv.Pool.QueryRow(context.Background(), `SELECT count(*) FROM app.time_off_requests WHERE action_id = $1`, id).Scan(&n)
	if n != 1 {
		t.Fatal("local copy", n)
	}
	rec = be.do("GET", "/api/requests?kind=time_off&scope=upcoming", nil)
	expect(t, rec, http.StatusOK, "ABS-000101", "ABS-000102")
	if l := decode[api.RequestList](t, rec); l.Total != 2 {
		t.Fatalf("requests %+v", l)
	}
	// AC-11: overlap with the seed
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": "2026-11-30", "date_to": "2026-11-30"}}),
		http.StatusUnprocessableEntity, "OVERLAP", "ABS-000101")
	// A-32 / AC-43: mentor sees upcoming time off; foreign → 404
	m := h.login("u-mentor")
	expect(t, m.do("GET", "/api/mentor/interns", nil), http.StatusOK, `"upcoming_time_off":[{"date_from":"2026-11-13"`)
	expect(t, m.do("GET", "/api/requests?employee_id=u-intern-be", nil), http.StatusOK, `"display_name"`)
	expect(t, m.do("GET", "/api/requests?employee_id=u-intern-other", nil), http.StatusNotFound)
	expect(t, m.do("GET", "/api/requests", nil), http.StatusNotFound)
	expect(t, be.do("GET", "/api/requests?employee_id=u-intern-fe", nil), http.StatusNotFound)
}

// A-05: validation vectors through the API.
func TestTimeOffValidation(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	cases := map[string][2]string{"DATE_IN_PAST": {"2026-11-09", "2026-11-10"}, "WEEKEND_DATE": {"2026-11-14", "2026-11-14"},
		"TOO_LONG": {"2026-11-16", "2026-11-23"}, "RANGE_REVERSED": {"2026-11-13", "2026-11-12"}, "REQUIRED": {"", "2026-11-13"}, "INVALID_DATE": {"2026-02-30", "2026-11-13"}}
	for code, d := range cases {
		expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": d[0], "date_to": d[1]}}),
			http.StatusUnprocessableEntity, code)
	}
	if len(h.events.Events("time_off_validation_failed")) != len(cases) {
		t.Fatal("validation events")
	}
	// A-17, AC-27, AC-28: identity and reason fields are refused
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": "2026-11-13", "date_to": "2026-11-13", "reason": "болею"}}), http.StatusBadRequest)
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "time_off", "employee_id": "u-intern-fe", "params": map[string]any{"date_from": "2026-11-13"}}), http.StatusBadRequest)
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "access", "params": map[string]any{"resource_id": "team-channels", "date_from": "2026-11-13"}}), http.StatusBadRequest)
}

// A-08, A-09, A-10, A-15, A-16, A-20, A-21
func TestDraftLifecycle(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	// cancel
	d := draft(t, be, "2026-11-13", "2026-11-13")
	id := d.PendingAction.ActionId.String()
	expect(t, be.do("POST", "/api/actions/"+id+"/cancel", nil), http.StatusOK, `"state":"cancelled"`)
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusGone, "ACTION_CANCELLED")
	expect(t, be.do("POST", "/api/actions/"+id+"/cancel", nil), http.StatusGone)
	// replace
	d1 := draft(t, be, "2026-11-13", "2026-11-13")
	d2 := draft(t, be, "2026-11-16", "2026-11-16")
	expect(t, be.do("POST", "/api/actions/"+d1.PendingAction.ActionId.String()+"/confirm", nil), http.StatusGone, "ACTION_REPLACED")
	expect(t, be.do("GET", "/api/actions/"+d1.PendingAction.ActionId.String(), nil), http.StatusOK, `"state":"replaced"`)
	// foreign draft
	fe := h.login("u-intern-fe")
	expect(t, fe.do("POST", "/api/actions/"+d2.PendingAction.ActionId.String()+"/confirm", nil), http.StatusNotFound)
	expect(t, fe.do("GET", "/api/actions/"+d2.PendingAction.ActionId.String(), nil), http.StatusNotFound)
	// expired (key still there: 410, not 404)
	h.clock.Advance(16 * time.Minute)
	expect(t, be.do("GET", "/api/actions/"+d2.PendingAction.ActionId.String(), nil), http.StatusOK, `"state":"expired"`)
	expect(t, be.do("POST", "/api/actions/"+d2.PendingAction.ActionId.String()+"/confirm", nil), http.StatusGone, "ACTION_EXPIRED")
	// midnight between draft and confirm: the date went to the past
	h.clock.Set(time.Date(2026, 11, 10, 23, 55, 0, 0, h.clock.Now().Location()))
	d3 := draft(t, be, "2026-11-10", "2026-11-10")
	h.clock.Set(time.Date(2026, 11, 11, 0, 5, 0, 0, h.clock.Now().Location()))
	expect(t, be.do("POST", "/api/actions/"+d3.PendingAction.ActionId.String()+"/confirm", nil), http.StatusUnprocessableEntity, "DATE_IN_PAST")
	expect(t, be.do("GET", "/api/actions/"+d3.PendingAction.ActionId.String(), nil), http.StatusOK, `"state":"active"`)
	// mentor cannot draft
	m := h.login("u-mentor")
	expect(t, m.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": "2026-11-13", "date_to": "2026-11-13"}}), http.StatusForbidden)
	expect(t, be.do("GET", "/api/actions/00000000-0000-4000-8000-000000000001", nil), http.StatusNotFound)
}

// A-11: 10 parallel confirms → one request, all 200 with one number.
func TestParallelConfirm(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	id := draft(t, be, "2026-11-13", "2026-11-13").PendingAction.ActionId.String()
	var wg sync.WaitGroup
	codes := make([]int, 10)
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := be.do("POST", "/api/actions/"+id+"/confirm", nil)
			codes[i] = rec.Code
			if rec.Code == 200 {
				ids[i] = decode[api.ConfirmResponse](t, rec).Result.RequestId
			}
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 || ids[i] != "ABS-000102" {
			t.Fatalf("confirm %d: %d %s", i, c, ids[i])
		}
	}
	if n := len(h.events.Events("action_completed")); n != 1 {
		t.Fatalf("action_completed = %d", n)
	}
}

// A-13, AC-21: the absence service fails, the draft stays active, retry works without a duplicate.
func TestConfirmServiceDown(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/demo/flags", nil), http.StatusOK, "mock_absences_fault")
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "mock_absences_fault", "value": "unavailable"}), http.StatusOK, `"value":"unavailable"`)
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "mock_absences_fault", "value": "broken"}), http.StatusBadRequest)
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "llm_enabled", "value": "off"}), http.StatusBadRequest) // A-34
	id := draft(t, be, "2026-11-13", "2026-11-13").PendingAction.ActionId.String()
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusServiceUnavailable, `"retryable":true`, "сервис отсутствий")
	if len(h.events.Events("action_failed")) != 1 {
		t.Fatal("action_failed")
	}
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "mock_absences_fault", "value": "off"}), http.StatusOK)
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusOK, "ABS-000102")
}

// A-21, AC-31, AC-38, PA-08: reset returns the seeds; old drafts are gone.
func TestDemoReset(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	id := draft(t, be, "2026-11-13", "2026-11-13").PendingAction.ActionId.String()
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusOK)
	expect(t, be.do("POST", "/api/path/steps/be-1.12/status", map[string]any{"status": "done", "expected_status": "not_started", "idempotency_key": newID()}), http.StatusOK)
	h.clock.Advance(7 * 24 * time.Hour)
	expect(t, be.do("POST", "/api/demo/reset", nil), http.StatusNoContent)
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusNotFound)
	rec := be.do("GET", "/api/requests?kind=time_off", nil)
	if l := decode[api.RequestList](t, rec); l.Total != 1 || l.Items[0].RequestId != "ABS-000101" {
		t.Fatalf("after reset %+v", l)
	}
	v := decode[api.PathView](t, be.do("GET", "/api/path", nil))
	if v.Day != 4 || v.StartDate.String() != "2026-11-14" {
		t.Fatalf("relative dates after a week: day %d start %s", v.Day, v.StartDate)
	}
	for _, s := range v.Steps {
		if s.StepId == "be-1.12" && s.Status != "not_started" {
			t.Fatal("progress not reset")
		}
	}
	h.srv.Cfg.DemoToolsEnabled = false
	expect(t, be.do("POST", "/api/demo/reset", nil), http.StatusNotFound)
	expect(t, be.do("GET", "/api/demo/flags", nil), http.StatusNotFound)
	expect(t, be.do("POST", "/api/demo/flags", map[string]any{"name": "mock_absences_fault", "value": "off"}), http.StatusNotFound)
}

// A-12, A-19, A-31 with a fake absence service.
func TestConfirmFakeService(t *testing.T) {
	h := newHarness(t)
	h.tools.On("absences.list_requests", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: "UNAVAILABLE", Retryable: true}
	})
	calls := 0
	h.tools.On("absences.create_request", func(_ context.Context, in map[string]any) (any, error) {
		calls++
		if calls == 1 {
			return nil, &mcpclient.ToolError{Code: mcpclient.CodeTimeout, Retryable: true}
		}
		if calls == 2 {
			return map[string]any{"request": map[string]any{"employee_id": "u-intern-be"}, "idempotent_replay": false}, nil // A-31: not by contract
		}
		return map[string]any{"request": map[string]any{"request_id": "ABS-000555", "employee_id": "u-intern-be", "type": "time_off", "status": "registered",
			"date_from": in["date_from"], "date_to": in["date_to"], "working_days": 1, "created_at": "2026-11-10T12:00:00Z"}, "idempotent_replay": true}, nil
	})
	be := h.login("u-intern-be")
	d := draft(t, be, "2026-11-13", "2026-11-13")
	if len(d.Notices) != 1 || d.Notices[0] != "overlap_unchecked" {
		t.Fatalf("A-19: %+v", d.Notices)
	}
	id := d.PendingAction.ActionId.String()
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusServiceUnavailable, "TIMEOUT")
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusServiceUnavailable, "INTERNAL")
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusOK, "ABS-000555")
	if len(h.events.Events("action_completed")) != 0 {
		t.Fatal("replay must not publish action_completed")
	}
	h.tools.On("absences.create_request", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: "VALIDATION_FAILED", Details: map[string]any{"violations": []any{map[string]any{"field": "range", "code": "HOLIDAY"}}}}
	})
	id = draft(t, be, "2026-11-16", "2026-11-16").PendingAction.ActionId.String()
	expect(t, be.do("POST", "/api/actions/"+id+"/confirm", nil), http.StatusUnprocessableEntity, "Сервис отклонил даты")
	for _, code := range []string{"PERMISSION_DENIED", "IDEMPOTENCY_CONFLICT", "NOT_FOUND", "DUPLICATE_ACTIVE"} {
		code := code
		h.tools.On("absences.create_request", func(context.Context, map[string]any) (any, error) { return nil, &mcpclient.ToolError{Code: code} })
		id = draft(t, be, "2026-11-17", "2026-11-17").PendingAction.ActionId.String()
		if rec := be.do("POST", "/api/actions/"+id+"/confirm", nil); rec.Code < 400 || rec.Code == 503 {
			t.Fatalf("%s → %d", code, rec.Code)
		}
	}
}

// A-33: the 21st action within a minute is refused.
func TestActionRateLimit(t *testing.T) {
	h := newHarness(t)
	be := h.login("u-intern-be")
	for i := 0; i < 20; i++ {
		be.do("POST", "/api/actions/00000000-0000-4000-8000-000000000001/cancel", nil)
	}
	expect(t, be.do("POST", "/api/actions/00000000-0000-4000-8000-000000000001/cancel", nil), http.StatusTooManyRequests, "RATE_LIMITED")
}
