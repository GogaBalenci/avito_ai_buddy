package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

func TestAuthFlow(t *testing.T) {
	h := newHarness(t)
	h.tools.On("checklist.get_path", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"employee_id": "u-intern-fe", "track": map[string]any{"id": "frontend", "title": "Frontend (тестовый трек)", "synthetic": true, "disclaimer": "Тестовый"},
			"periods": []any{}, "steps": []any{}}, nil
	})
	expect(t, h.anon().do("GET", "/api/me", nil), http.StatusUnauthorized, "UNAUTHORIZED")
	expect(t, h.anon().do("POST", "/api/auth/login", map[string]any{"user_id": "u-nobody"}), http.StatusNotFound)
	expect(t, h.anon().do("POST", "/api/auth/login", map[string]any{"user_id": "u-intern-be", "role": "mentor"}), http.StatusBadRequest)

	fe := h.login("u-intern-fe")
	expect(t, fe.do("GET", "/api/me", nil), http.StatusOK, `"role":"intern"`, `"synthetic":true`)
	m := h.login("u-mentor")
	me := m.do("GET", "/api/me", nil)
	expect(t, me, http.StatusOK, `"role":"mentor"`)
	expect(t, fe.do("POST", "/api/auth/logout", nil), http.StatusNoContent)
	expect(t, fe.do("GET", "/api/me", nil), http.StatusUnauthorized)
}

func TestAccountsHideFixtures(t *testing.T) {
	h := newHarness(t)
	rec := h.anon().do("GET", "/api/auth/accounts", nil)
	expect(t, rec, http.StatusOK, "u-intern-be", "u-mentor")
	if body := rec.Body.String(); contains(body, "u-intern-other") {
		t.Fatal("fixture account listed")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// D15 / A-35: POST without Origin or with a foreign Origin → 403.
func TestOriginCheck(t *testing.T) {
	h := newHarness(t)
	c := h.anon()
	expect(t, c.doWith("POST", "/api/auth/login", map[string]any{"user_id": "u-intern-be"}, nil), http.StatusForbidden, "BAD_ORIGIN")
	expect(t, c.doWith("POST", "/api/auth/login", map[string]any{"user_id": "u-intern-be"}, map[string]string{"Origin": "http://evil.test"}), http.StatusForbidden, "BAD_ORIGIN")
}

func TestValidatorAndRouting(t *testing.T) {
	h := newHarness(t)
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/nope", nil), http.StatusNotFound)
	if rec := be.do("DELETE", "/api/me", nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/me = %d", rec.Code)
	}
	// D3: extra field employee_id
	expect(t, be.do("POST", "/api/actions", map[string]any{"kind": "time_off", "params": map[string]any{"date_from": "2026-11-13"}, "employee_id": "u-x"}), http.StatusBadRequest, "BAD_REQUEST")
	expect(t, be.do("GET", "/api/chat/history?limit=1000", nil), http.StatusBadRequest)
}

func TestConfigAndLLMHealth(t *testing.T) {
	h := newHarness(t)
	expect(t, h.anon().do("GET", "/api/config", nil), http.StatusOK, `"poll_interval_ms":10000`, `"timezone":"Europe/Moscow"`, `"llm_provider":"demo"`)
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"status":"disabled"`)
	h.srv.Cfg.LLM = config.LLMConfig{Requested: "openai_compatible", Provider: "demo", Misconfigured: "LLM_MODEL", APIKey: "secret-key"}
	rec := h.anon().do("GET", "/api/llm/health", nil)
	expect(t, rec, http.StatusOK, `"status":"misconfigured"`, "missing_LLM_MODEL")
	if contains(rec.Body.String(), "secret-key") {
		t.Fatal("key leaked")
	}
	h.srv.Cfg.LLM = config.LLMConfig{Requested: "openai_compatible", Provider: "openai_compatible", Model: "m"}
	expect(t, h.anon().do("GET", "/api/llm/health", nil), http.StatusOK, `"model":"m"`)
	expect(t, h.anon().do("GET", "/healthz", nil), http.StatusOK)
}

func TestToolErrorMapping(t *testing.T) {
	cases := map[string]int{"VALIDATION_FAILED": 422, "OVERLAP": 422, "PERMISSION_DENIED": 403, "NOT_FOUND": 404,
		"DUPLICATE_ACTIVE": 409, "STATUS_CONFLICT": 409, "TIMEOUT": 503, "INTERNAL": 503, "UNAVAILABLE": 503}
	for code, want := range cases {
		got, _, _ := toolHTTP(&mcpclient.ToolError{Code: code})
		if got != want {
			t.Errorf("%s → %d, want %d", code, got, want)
		}
		rec := httptest.NewRecorder()
		writeToolError(rec, nil, &mcpclient.ToolError{Code: code}, "")
		if rec.Code != want {
			t.Errorf("writeToolError %s → %d", code, rec.Code)
		}
	}
	if got, _, retry := toolHTTP(errors.New("x")); got != 503 || !retry {
		t.Fatal("plain error")
	}
	rec := httptest.NewRecorder()
	writeGuardError(rec, nil, guard.ErrNotFound, "")
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	writeGuardError(rec, nil, guard.ErrDenied, "нет")
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
}

func TestUnimplementedIs501(t *testing.T) {
	h := newHarness(t)
	be := h.login("u-intern-be")
	expect(t, be.do("GET", "/api/mentor/access-requests", nil), http.StatusNotImplemented)
}

func TestPanicRecovered(t *testing.T) {
	h := newHarness(t)
	h.srv.Health = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	expect(t, h.anon().do("GET", "/healthz", nil), http.StatusInternalServerError, "INTERNAL")
}
