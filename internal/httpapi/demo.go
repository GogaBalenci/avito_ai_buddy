package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
)

// redisPatterns are cleared by the demo reset; sessions stay (12 §8.5).
var redisPatterns = []string{"action:*", "active_action:*", "msg:*", "msglock:*", "ctx:*", "dedupe:*"}

func (s *Server) demoOff(w http.ResponseWriter, r *http.Request) bool {
	if !s.Cfg.DemoToolsEnabled {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
		return true
	}
	return false
}

func (s *Server) admin(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.Cfg.MocksAdminURL+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := s.AdminHTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

// ResetDemo resets mocks first, then the app schema and Redis (12 §8.5).
func (s *Server) ResetDemo(w http.ResponseWriter, r *http.Request) {
	if s.demoOff(w, r) {
		return
	}
	ctx := r.Context()
	if _, code, err := s.admin(ctx, http.MethodPost, "/demo/reset", nil); err != nil || code != http.StatusNoContent {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Не получилось сбросить демо-данные — попробуй ещё раз", map[string]any{"retryable": true})
		return
	}
	if err := s.Seeder.Reset(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Не получилось сбросить демо-данные — попробуй ещё раз", map[string]any{"retryable": true})
		return
	}
	for _, p := range redisPatterns {
		iter := s.Redis.Scan(ctx, 0, p, 200).Iterator()
		for iter.Next(ctx) {
			_ = s.Redis.Del(ctx, iter.Val()).Err()
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListDemoFlags lists the mock flags of the white list.
func (s *Server) ListDemoFlags(w http.ResponseWriter, r *http.Request) {
	if s.demoOff(w, r) {
		return
	}
	b, code, err := s.admin(r.Context(), http.MethodGet, "/demo/flags", nil)
	var list api.DemoFlagList
	if err != nil || code != http.StatusOK || json.Unmarshal(b, &list) != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// SetDemoFlag changes a white-listed mock flag (the name enum is in OpenAPI).
func (s *Server) SetDemoFlag(w http.ResponseWriter, r *http.Request) {
	if s.demoOff(w, r) {
		return
	}
	var body api.DemoFlagUpdate
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	b, code, err := s.admin(r.Context(), http.MethodPost, "/demo/flags", body)
	switch {
	case err != nil || code >= 500:
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
	case code == http.StatusBadRequest:
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Значение %q для флага %s не разрешено", body.Value, body.Name), nil)
	default:
		var f api.DemoFlag
		_ = json.Unmarshal(b, &f)
		writeJSON(w, http.StatusOK, f)
	}
}
