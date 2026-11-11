// Package auth is layer 1 of the permission model (13 §9): the test sign-in
// (a stand-in for corporate SSO), sessions in Redis, the same-origin check on
// every POST and per-user rate limits (12 §5.2).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
)

// CookieName is the session cookie (HttpOnly, SameSite=Lax).
const CookieName = "buddy_session"

// Errors of the session layer.
var (
	ErrNoSession = errors.New("no session")
	ErrUnknown   = errors.New("unknown account")
)

// Session is stored in Redis as sess:{sid}. The role is never in the cookie.
type Session struct {
	ID        string    `json:"-"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Actor returns the principal of the session.
func (s Session) Actor() actor.Actor { return actor.Actor{UserID: s.UserID, Role: s.Role} }

// Sessions manages sessions.
type Sessions struct {
	Redis     *redis.Client
	Employees employees.Provider
	Clock     clock.Clock
	TTL       time.Duration
	Secure    bool
}

func key(sid string) string { return "sess:" + sid }

// Login creates a session for a test account; the role comes from the
// employee data, never from the request (13 §2).
func (s *Sessions) Login(ctx context.Context, userID string) (Session, error) {
	e, err := s.Employees.Get(ctx, userID)
	if errors.Is(err, employees.ErrNotFound) {
		return Session{}, ErrUnknown
	}
	if err != nil {
		return Session{}, err
	}
	if e.Role != actor.Intern && e.Role != actor.Mentor {
		return Session{}, ErrUnknown
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Session{}, err
	}
	sess := Session{ID: base64.RawURLEncoding.EncodeToString(b), UserID: e.UserID, Role: e.Role, CreatedAt: s.Clock.Now()}
	raw, _ := json.Marshal(sess)
	if err := s.Redis.Set(ctx, key(sess.ID), raw, s.TTL).Err(); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Get loads a session and extends its TTL. A session with any role other than
// intern or mentor (for example, a planted "system") is deleted: D13.
func (s *Sessions) Get(ctx context.Context, sid string) (Session, error) {
	if sid == "" {
		return Session{}, ErrNoSession
	}
	raw, err := s.Redis.Get(ctx, key(sid)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	var sess Session
	if err := json.Unmarshal(raw, &sess); err != nil || (sess.Role != actor.Intern && sess.Role != actor.Mentor) || sess.UserID == "" {
		_ = s.Redis.Del(ctx, key(sid)).Err()
		return Session{}, ErrNoSession
	}
	sess.ID = sid
	_ = s.Redis.Expire(ctx, key(sid), s.TTL).Err()
	return sess, nil
}

// Logout deletes the session.
func (s *Sessions) Logout(ctx context.Context, sid string) error {
	return s.Redis.Del(ctx, key(sid)).Err()
}

// Cookie returns the session cookie.
func (s *Sessions) Cookie(sid string) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: sid, Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(s.TTL.Seconds())}
}

// ClearCookie expires the session cookie.
func (s *Sessions) ClearCookie() *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1}
}

// SessionID reads the cookie value.
func SessionID(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// OriginAllowed is the CSRF check (AG-26): the Origin of a POST must be the
// same origin as the request Host (so localhost and 127.0.0.1 both work) or
// listed in ALLOWED_ORIGINS. A missing Origin is rejected.
func OriginAllowed(r *http.Request, allowed []string) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimRight(o, "/"), a) {
			return true
		}
	}
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

// Limiter counts actions per user per minute in Redis (rl:{user}:{kind}:{epoch_min}).
type Limiter struct {
	Redis *redis.Client
	Clock clock.Clock
	Max   int
}

// Allow reports whether one more action of kind ("msg" or "act") fits the limit.
func (l *Limiter) Allow(ctx context.Context, userID, kind string) (bool, error) {
	k := fmt.Sprintf("rl:%s:%s:%d", userID, kind, l.Clock.Now().Unix()/60)
	n, err := l.Redis.Incr(ctx, k).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		_ = l.Redis.Expire(ctx, k, 70*time.Second).Err()
	}
	return int(n) <= l.Max, nil
}
