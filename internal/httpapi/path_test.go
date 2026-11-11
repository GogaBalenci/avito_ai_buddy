package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

// PA-01: the first screen of the Backend demo (with the temporary cm-1.7 seed).
func TestGetPathDemoBackend(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	start := time.Now()
	rec := be.do("GET", "/api/path", nil)
	expect(t, rec, http.StatusOK)
	if time.Since(start) > 2*time.Second {
		t.Fatal("too slow")
	}
	v := decode[api.PathView](t, rec)
	if v.Day != 4 || v.CurrentPeriod != "week1" || v.State != "ready" || v.ReadOnly {
		t.Fatalf("day %d period %s state %s", v.Day, v.CurrentPeriod, v.State)
	}
	if v.NextStep == nil || v.NextStep.StepId != "be-1.11" {
		t.Fatalf("next = %+v (cm-1.7 is temporarily seeded done until S4)", v.NextStep)
	}
	if len(v.Waiting) != 1 || v.Waiting[0] != "cm-1.10" || v.Progress.PeriodDone != 3 || v.Progress.PeriodTotal != 12 {
		t.Fatalf("waiting %v progress %+v", v.Waiting, v.Progress)
	}
	if v.Employee.Track.Id != "backend" || v.Today.String() != "2026-11-10" || v.StartDate.String() != "2026-11-07" {
		t.Fatalf("employee %+v today %s", v.Employee, v.Today)
	}
	expect(t, be.do("GET", "/api/path", nil), http.StatusOK)
	if n := len(h.events.Events("path_viewed")); n != 1 {
		t.Fatalf("path_viewed deduplicated: %d", n)
	}
	if n := len(h.events.Events("period_started")); n != 1 {
		t.Fatalf("period_started: %d", n)
	}
	var stats int
	_ = h.srv.Pool.QueryRow(context.Background(), `SELECT total FROM app.path_period_stats WHERE user_id='u-intern-be' AND period='week1'`).Scan(&stats)
	if stats != 12 {
		t.Fatalf("path_period_stats = %d", stats)
	}
}

// PA-04, PA-05, PA-09, AC-P13, AC-P14, AC-P15
func TestMarkStep(t *testing.T) {
	h := newIntegration(t)
	be := h.login("u-intern-be")
	key := newID()
	body := map[string]any{"status": "done", "expected_status": "in_progress", "idempotency_key": key}
	rec := be.do("POST", "/api/path/steps/be-1.11/status", body)
	expect(t, rec, http.StatusOK)
	res := decode[api.StepStatusResult](t, rec)
	if !res.Changed || res.Step.Status != "done" || res.PathSummary.Progress.PeriodDone != 4 || res.PathSummary.NextStep.StepId != "be-1.12" {
		t.Fatalf("result %+v", res)
	}
	expect(t, be.do("POST", "/api/path/steps/be-1.11/status", body), http.StatusOK)
	if n := len(h.events.Events("step_done")); n != 1 {
		t.Fatalf("step_done published %d times", n)
	}
	// AC-P14: another tab already changed the status
	expect(t, be.do("POST", "/api/path/steps/be-1.11/status", map[string]any{"status": "done", "expected_status": "in_progress", "idempotency_key": newID()}),
		http.StatusConflict, "STATUS_CONFLICT", `"current_status":"done"`)
	// AC-P15: reopen
	expect(t, be.do("POST", "/api/path/steps/be-1.11/status", map[string]any{"status": "in_progress", "expected_status": "done", "idempotency_key": newID()}),
		http.StatusOK, `"status":"in_progress"`)
	if len(h.events.Events("step_reopened")) != 1 {
		t.Fatal("step_reopened")
	}
	// start a not_started step
	expect(t, be.do("POST", "/api/path/steps/be-1.12/status", map[string]any{"status": "in_progress", "expected_status": "not_started", "idempotency_key": newID()}), http.StatusOK)
	if len(h.events.Events("step_started")) != 1 {
		t.Fatal("step_started")
	}
	// PA-05 / AC-P07: access step cannot be closed by hand
	expect(t, be.do("POST", "/api/path/steps/cm-1.10/status", map[string]any{"status": "done", "expected_status": "waiting", "idempotency_key": newID()}),
		http.StatusConflict, "TRANSITION_NOT_ALLOWED")
	// PA-09: identity fields are refused by the schema
	expect(t, be.do("POST", "/api/path/steps/be-1.12/status", map[string]any{"status": "done", "expected_status": "in_progress", "idempotency_key": newID(), "employee_id": "u-intern-fe"}),
		http.StatusBadRequest)
	// PA-11: not in the path
	expect(t, be.do("POST", "/api/path/steps/cm-1.1/status", map[string]any{"status": "done", "expected_status": "not_started", "idempotency_key": newID()}), http.StatusNotFound)
	expect(t, be.do("GET", "/api/path/steps/fe-w1-10", nil), http.StatusNotFound, "Такого шага")
	expect(t, be.do("GET", "/api/path/steps/be-1.13", nil), http.StatusOK, `"kb_doc":"term-avito-cli"`)
	expect(t, be.do("POST", "/api/path/steps/be-1.13/status", map[string]any{"status": "done", "expected_status": "done", "idempotency_key": newID()}),
		http.StatusConflict)
}

// PA-06, AC-P20, AC-P21, P-A12, P-A13
func TestMentorScreens(t *testing.T) {
	h := newIntegration(t)
	m := h.login("u-mentor")
	expect(t, m.do("GET", "/api/path", nil), http.StatusForbidden)
	rec := m.do("GET", "/api/mentor/interns", nil)
	expect(t, rec, http.StatusOK)
	list := decode[api.MentorInternList](t, rec)
	if len(list.Items) != 2 || list.Items[0].UserId != "u-intern-be" || list.Items[1].CurrentPeriod != "week2" || list.Items[1].Day != 11 {
		t.Fatalf("interns %+v", list.Items)
	}
	rec = m.do("GET", "/api/mentor/interns/u-intern-fe/path", nil)
	expect(t, rec, http.StatusOK, `"read_only":true`, `"synthetic":true`)
	v := decode[api.PathView](t, rec)
	if v.NextStep == nil || v.NextStep.StepId != "fe-w1-50" || v.NextStep.Reason != "overdue" {
		t.Fatalf("AC-P10: %+v", v.NextStep)
	}
	expect(t, m.do("GET", "/api/mentor/interns/u-intern-other/path", nil), http.StatusNotFound)
	expect(t, m.do("POST", "/api/path/steps/be-1.12/status", map[string]any{"status": "done", "expected_status": "not_started", "idempotency_key": newID()}),
		http.StatusForbidden, "только для чтения")
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/mentor/interns", nil), http.StatusForbidden)
	expect(t, be.do("GET", "/api/mentor/interns/u-intern-fe/path", nil), http.StatusForbidden)
	expect(t, m.do("GET", "/api/path/steps/be-1.12", nil), http.StatusNotFound)
}

// PA-07 / AC-P22: the checklist service is down → 503 with retry.
func TestPathServiceDown(t *testing.T) {
	h := newHarness(t)
	h.tools.On("checklist.get_path", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: mcpclient.CodeTimeout, Retryable: true}
	})
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/path", nil), http.StatusServiceUnavailable, `"retryable":true`, "TIMEOUT")
	m := h.login("u-mentor")
	expect(t, m.do("GET", "/api/mentor/interns", nil), http.StatusServiceUnavailable)
	h.tools.On("checklist.get_step", func(context.Context, map[string]any) (any, error) {
		return nil, &mcpclient.ToolError{Code: "UNAVAILABLE", Retryable: true}
	})
	expect(t, be.do("POST", "/api/path/steps/be-1.12/status", map[string]any{"status": "done", "expected_status": "in_progress", "idempotency_key": newID()}), http.StatusServiceUnavailable)
}
