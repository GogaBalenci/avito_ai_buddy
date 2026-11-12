package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/llm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/auth"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// GetHealthz is /healthz: PostgreSQL and Redis (12 §3.4).
func (s *Server) GetHealthz(w http.ResponseWriter, r *http.Request) { s.Health.ServeHTTP(w, r) }

// GetConfig gives the client its settings.
func (s *Server) GetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.AppConfig{
		PollIntervalMs:   s.Cfg.PollIntervalMS,
		DemoToolsEnabled: s.Cfg.DemoToolsEnabled,
		Timezone:         s.Cfg.Location.String(),
		LlmProvider:      api.AppConfigLlmProvider(s.Cfg.LLM.Requested),
	})
}

// GetLlmHealth reports the LLM mode without secrets (09 §13.2).
func (s *Server) GetLlmHealth(w http.ResponseWriter, r *http.Request) {
	h := api.LlmHealth{Provider: api.LlmHealthProvider(s.Cfg.LLM.Requested), CheckedAt: s.Clock.Now()}
	var eng *llm.Engine
	if s.Agent != nil {
		eng = s.Agent.LLM
	}
	status, code := llm.StatusDisabled, ""
	switch {
	case eng != nil:
		status, code = eng.Status(r.Context())
	case s.Cfg.LLM.Misconfigured != "":
		status, code = llm.StatusMisconfigured, "missing_"+s.Cfg.LLM.Misconfigured
	}
	h.Status = api.LlmHealthStatus(status)
	if code != "" {
		h.LastErrorCode = &code
	}
	if s.Cfg.LLM.Model != "" && s.Cfg.LLM.Requested != "demo" {
		m := s.Cfg.LLM.Model
		h.Model = &m
	}
	writeJSON(w, http.StatusOK, h)
}

// ListTestAccounts lists the sign-in accounts (SSO stand-in).
func (s *Server) ListTestAccounts(w http.ResponseWriter, r *http.Request) {
	all, err := s.Accounts.All(r.Context())
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	items := make([]api.TestAccount, 0, len(all))
	for _, e := range all {
		if _, fixture := s.AccountNotes[e.UserID]; !fixture && len(s.AccountNotes) > 0 {
			continue
		}
		items = append(items, api.TestAccount{UserId: e.UserID, DisplayName: e.DisplayName, Role: api.Role(e.Role), Description: s.AccountNotes[e.UserID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Login signs in with a test account; role from employee data (13 §2).
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body api.LoginRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	sess, err := s.Sessions.Login(r.Context(), body.UserId)
	if errors.Is(err, auth.ErrUnknown) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Такой тестовой учётки нет", nil)
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	http.SetCookie(w, s.Sessions.Cookie(sess.ID))
	ctx := reqctx.WithActor(r.Context(), sess.Actor())
	me, err := s.me(ctx, sess.Actor())
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// Logout deletes the session.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	_ = s.Sessions.Logout(r.Context(), auth.SessionID(r))
	http.SetCookie(w, s.Sessions.ClearCookie())
	w.WriteHeader(http.StatusNoContent)
}

// GetMe returns the current user.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	a, _ := reqctx.Actor(r.Context())
	me, err := s.me(r.Context(), a)
	if errors.Is(err, employees.ErrNotFound) {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Сессия истекла — войди снова", nil)
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	writeJSON(w, http.StatusOK, me)
}

func (s *Server) me(ctx context.Context, a actor.Actor) (api.Me, error) {
	e, err := s.Employees.Get(ctx, a.UserID)
	if err != nil {
		return api.Me{}, err
	}
	me := api.Me{UserId: e.UserID, DisplayName: e.DisplayName, Role: api.Role(e.Role)}
	if e.Role == actor.Intern {
		ref := s.trackRef(ctx, a, e.Track)
		me.Track = &ref
	}
	return me, nil
}

// trackRef asks the checklist service for the track title; on failure the id
// is enough for the header (the path panel shows its own error).
func (s *Server) trackRef(ctx context.Context, a actor.Actor, track string) api.TrackRef {
	ref := api.TrackRef{Id: track, Title: track}
	var out struct {
		Track struct {
			ID         string  `json:"id"`
			Title      string  `json:"title"`
			Synthetic  bool    `json:"synthetic"`
			Disclaimer *string `json:"disclaimer"`
		} `json:"track"`
	}
	if s.Tools == nil {
		return ref
	}
	if err := s.Tools.Call(ctx, "checklist", "get_path", map[string]any{"actor": a}, &out); err == nil {
		ref = api.TrackRef{Id: out.Track.ID, Title: out.Track.Title, Synthetic: out.Track.Synthetic, Disclaimer: out.Track.Disclaimer}
	}
	return ref
}
