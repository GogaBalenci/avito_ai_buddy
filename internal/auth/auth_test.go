package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

func newSessions(t *testing.T) (*Sessions, *testkit.Clock) {
	rc, _ := testkit.Redis(t)
	clk := testkit.NewClock(testkit.Today)
	return &Sessions{Redis: rc, Employees: testkit.EmployeeProvider(), Clock: clk, TTL: time.Hour}, clk
}

func TestLoginAndGet(t *testing.T) {
	s, _ := newSessions(t)
	ctx := context.Background()
	sess, err := s.Login(ctx, "u-intern-be")
	if err != nil || sess.Role != "intern" || sess.ID == "" {
		t.Fatalf("login: %+v %v", sess, err)
	}
	got, err := s.Get(ctx, sess.ID)
	if err != nil || got.Actor().UserID != "u-intern-be" || !got.Actor().IsIntern() {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.Login(ctx, "u-nobody"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	if err := s.Logout(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, sess.ID); !errors.Is(err, ErrNoSession) {
		t.Fatal("session survived logout")
	}
	if _, err := s.Get(ctx, ""); !errors.Is(err, ErrNoSession) {
		t.Fatal("empty sid")
	}
	c := s.Cookie("x")
	if !c.HttpOnly || c.Name != CookieName || s.ClearCookie().MaxAge != -1 {
		t.Fatal("cookie flags")
	}
}

// D13: a planted session with role system is rejected and deleted.
func TestSystemRoleSessionRejected(t *testing.T) {
	s, _ := newSessions(t)
	ctx := context.Background()
	s.Redis.Set(ctx, "sess:planted", `{"user_id":"svc-buddy","role":"system"}`, time.Hour)
	if _, err := s.Get(ctx, "planted"); !errors.Is(err, ErrNoSession) {
		t.Fatal("system session accepted")
	}
	if s.Redis.Exists(ctx, "sess:planted").Val() != 0 {
		t.Fatal("planted session not deleted")
	}
	s.Redis.Set(ctx, "sess:junk", `{`, time.Hour)
	if _, err := s.Get(ctx, "junk"); !errors.Is(err, ErrNoSession) {
		t.Fatal("junk session accepted")
	}
}

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		host, origin, fwd string
		allowed           []string
		want              bool
	}{
		{"localhost:8380", "http://localhost:8380", "", nil, true},
		{"127.0.0.1:8380", "http://127.0.0.1:8380", "", nil, true},
		{"localhost:8380", "http://evil.test", "", nil, false},
		{"localhost:8380", "", "", nil, false},
		{"localhost:8380", "null", "", nil, false},
		{"api:8080", "http://localhost:8380", "localhost:8380", nil, true},
		{"localhost:8380", "http://demo.test", "", []string{"http://demo.test"}, true},
		{"localhost:8380", "ftp://localhost:8380", "", nil, false},
		{"localhost:8380", "::", "", nil, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/x", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.fwd != "" {
			r.Header.Set("X-Forwarded-Host", c.fwd)
		}
		if got := OriginAllowed(r, c.allowed); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(s0())
	if SessionID(r) != "abc" || SessionID(httptest.NewRequest("GET", "/", nil)) != "" {
		t.Fatal("SessionID")
	}
}

func s0() *http.Cookie { return &http.Cookie{Name: CookieName, Value: "abc"} }

// A-33: the 21st action within a minute is refused.
func TestLimiter(t *testing.T) {
	rc, _ := testkit.Redis(t)
	clk := testkit.NewClock(testkit.Today)
	l := &Limiter{Redis: rc, Clock: clk, Max: 20}
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if ok, err := l.Allow(ctx, "u-intern-be", "act"); !ok || err != nil {
			t.Fatalf("action %d refused", i+1)
		}
	}
	if ok, _ := l.Allow(ctx, "u-intern-be", "act"); ok {
		t.Fatal("21st action allowed")
	}
	if ok, _ := l.Allow(ctx, "u-intern-be", "msg"); !ok {
		t.Fatal("messages share the action bucket")
	}
	clk.Advance(time.Minute)
	if ok, _ := l.Allow(ctx, "u-intern-be", "act"); !ok {
		t.Fatal("next minute refused")
	}
}
