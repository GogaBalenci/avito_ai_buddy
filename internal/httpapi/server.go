// Package httpapi implements the HTTP API of api/openapi.yaml (12 §6):
// middleware chain of 12 §5.2, error mapping of 12 §5.3 and the handlers.
// Operations not implemented yet answer 501 (api.Unimplemented).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/auth"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/demo"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

func init() {
	openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(openapi3.FormatOfStringForUUIDOfRFC9562))
}

// AccountDirectory lists test accounts for the sign-in screen.
type AccountDirectory interface {
	All(ctx context.Context) ([]employees.Employee, error)
}

// Deps are the dependencies of the API.
type Deps struct {
	Cfg       config.Config
	Log       *slog.Logger
	Pool      *pgxpool.Pool
	Redis     *redis.Client
	Clock     clock.Clock
	Sessions  *auth.Sessions
	Limiter   *auth.Limiter
	Employees employees.Provider
	Accounts  AccountDirectory
	// Descriptions of test accounts from the seeds (shown on the sign-in screen).
	AccountNotes map[string]string
	Events       events.Publisher
	Flags        flags.Provider
	Tools        mcpclient.Client
	Guard        *guard.Guard
	Health       http.Handler
	Path         *path.Service
	Actions      *actions.Service
	Seeder       *demo.Seeder
	AdminHTTP    *http.Client // calls MCP_MOCKS_ADMIN_URL (demo only)
	// Sync brings access requests of an intern up to date (S4, 11 §3.6); nil until S4.
	Sync func(ctx context.Context, userID string) error
}

// Server implements api.ServerInterface.
type Server struct {
	api.Unimplemented
	Deps
}

// publicPaths do not need a session (12 §5.2).
var publicPaths = map[string]bool{
	"/healthz": true, "/api/config": true, "/api/llm/health": true,
	"/api/auth/accounts": true, "/api/auth/login": true,
}

// Handler builds the router with the middleware chain of 12 §5.2.
func Handler(s *Server) (http.Handler, error) {
	spec, err := api.GetSpec()
	if err != nil {
		return nil, err
	}
	spec.Servers = nil // match paths as they are, without a server prefix
	router, err := legacy.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	r := chi.NewRouter()
	r.Use(s.requestID, s.recoverer, s.logRequests, s.session, s.origin, s.validator(router))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "BAD_REQUEST", "Метод не поддерживается", nil)
	})
	return api.HandlerWithOptions(s, api.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос: "+err.Error(), nil)
		},
	}), nil
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(reqctx.WithRequestID(r.Context(), id)))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Log.ErrorContext(r.Context(), "panic", "request_id", reqctx.RequestID(r.Context()), "panic", v, "stack", string(debug.Stack()))
				writeError(w, r, http.StatusInternalServerError, "INTERNAL", "Что-то пошло не так", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) { w.code = code; w.ResponseWriter.WriteHeader(code) }

// logRequests logs without bodies and query strings (they may hold user text).
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		lvl := slog.LevelInfo
		if r.URL.Path == "/healthz" && sw.code < 500 {
			lvl = slog.LevelDebug
		}
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", sw.code,
			"duration_ms", time.Since(start).Milliseconds(), "request_id", reqctx.RequestID(r.Context())}
		if a, ok := reqctx.Actor(r.Context()); ok {
			attrs = append(attrs, "user_id", a.UserID)
		}
		s.Log.Log(r.Context(), lvl, "http request", attrs...)
	})
}

// session turns the cookie into the actor (layer 1); identity never comes
// from the body or the model.
func (s *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := s.Sessions.Get(r.Context(), auth.SessionID(r))
		if errors.Is(err, auth.ErrNoSession) {
			http.SetCookie(w, s.Sessions.ClearCookie())
			writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Сессия истекла — войди снова", nil)
			return
		}
		if err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
			return
		}
		ctx := reqctx.WithActor(r.Context(), sess.Actor())
		ctx = reqctx.WithSessionID(ctx, sess.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// origin: every POST must be same-origin (AG-26, D15).
func (s *Server) origin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !auth.OriginAllowed(r, s.Cfg.AllowedOrigins) {
			writeError(w, r, http.StatusForbidden, "BAD_ORIGIN", "Запрос отклонён: неверный Origin", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validator checks parameters and bodies against api/openapi.yaml
// (additionalProperties: false → 400; D3).
func (s *Server) validator(router routers.Router) func(http.Handler) http.Handler {
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, params, err := router.FindRoute(r)
			if err != nil {
				if errors.Is(err, routers.ErrMethodNotAllowed) {
					writeError(w, r, http.StatusMethodNotAllowed, "BAD_REQUEST", "Метод не поддерживается", nil)
					return
				}
				writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
				return
			}
			in := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: opts}
			if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
				writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос: "+shortErr(err), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func shortErr(err error) string {
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// writeJSON writes v.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the common Error body; extra fields are merged in.
func writeError(w http.ResponseWriter, _ *http.Request, code int, errCode, msg string, extra map[string]any) {
	body := map[string]any{"code": errCode, "message": msg}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, code, body)
}

// actorOf returns the session actor (the session middleware guarantees it).
func actorOf(r *http.Request) (a struct{ UserID, Role string }) {
	if x, ok := reqctx.Actor(r.Context()); ok {
		a.UserID, a.Role = x.UserID, x.Role
	}
	return a
}

// decodeBody decodes an already validated JSON body.
func decodeBody(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

// toolHTTP maps a ToolClient error to HTTP (12 §5.3).
func toolHTTP(err error) (int, string, bool) {
	te, ok := mcpclient.AsToolError(err)
	if !ok {
		return http.StatusServiceUnavailable, "UNAVAILABLE", true
	}
	switch te.Code {
	case "VALIDATION_FAILED", "OVERLAP":
		return http.StatusUnprocessableEntity, "VALIDATION_FAILED", false
	case "PERMISSION_DENIED":
		return http.StatusForbidden, "PERMISSION_DENIED", false
	case "NOT_FOUND":
		return http.StatusNotFound, "NOT_FOUND", false
	case "DUPLICATE_ACTIVE", "ALREADY_DECIDED", "STATUS_CONFLICT", "TRANSITION_NOT_ALLOWED", "IDEMPOTENCY_CONFLICT":
		return http.StatusConflict, te.Code, false
	case mcpclient.CodeTimeout:
		return http.StatusServiceUnavailable, "TIMEOUT", true
	case mcpclient.CodeInternal:
		return http.StatusServiceUnavailable, "INTERNAL", true
	default:
		return http.StatusServiceUnavailable, "UNAVAILABLE", true
	}
}

// writeToolError answers with the mapped tool error.
func writeToolError(w http.ResponseWriter, r *http.Request, err error, msg string) {
	code, errCode, retry := toolHTTP(err)
	extra := map[string]any{}
	if retry {
		extra["retryable"] = true
	}
	if msg == "" {
		switch code {
		case http.StatusNotFound:
			msg = "Не найдено"
		case http.StatusForbidden:
			msg = "Недостаточно прав"
		case http.StatusConflict:
			msg = "Состояние уже изменилось"
		default:
			msg = "Сервис сейчас не отвечает, попробуй ещё раз"
		}
	}
	writeError(w, r, code, errCode, msg, extra)
}

// guardHTTP maps guard errors: 404 hides foreign objects, 403 is a known
// object the role may not act on (13 §7).
func writeGuardError(w http.ResponseWriter, r *http.Request, err error, msg403 string) {
	if errors.Is(err, guard.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
		return
	}
	writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", msg403, nil)
}

// limit applies the per-user rate limit (A31: 20 per minute).
func (s *Server) limit(w http.ResponseWriter, r *http.Request, kind string) bool {
	a := actorOf(r)
	ok, err := s.Limiter.Allow(r.Context(), a.UserID, kind)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return false
	}
	if !ok {
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Слишком быстро, подожди немного", nil)
		return false
	}
	return true
}
