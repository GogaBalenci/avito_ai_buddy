// Package testkit holds fakes shared by tests: a fixed clock, miniredis,
// employee fixtures (including the "foreign intern" u-intern-other, 13 §2).
package testkit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
)

// Moscow is the business time zone of the prototype.
var Moscow = mustLoc("Europe/Moscow")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// Today is the acceptance fixture date: Tuesday 2026-11-10 12:00 MSK (11 §0.1).
var Today = time.Date(2026, 11, 10, 12, 0, 0, 0, Moscow)

// Clock is a settable fake of clock.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock fixed at t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) { c.mu.Lock(); c.now = t; c.mu.Unlock() }

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// Redis starts miniredis for the test and returns a client and the server.
func Redis(t testing.TB) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return c, mr
}

// Employees returns the demo accounts plus the foreign-intern fixture.
func Employees() []employees.Employee {
	be := Today.AddDate(0, 0, -3)
	fe := Today.AddDate(0, 0, -10)
	other := Today.AddDate(0, 0, -5)
	return []employees.Employee{
		{UserID: "u-intern-be", DisplayName: "Стажёр Backend (тестовый)", Role: "intern", Track: "backend", MentorID: "u-mentor", StartDate: &be},
		{UserID: "u-intern-fe", DisplayName: "Стажёр Frontend (тестовый)", Role: "intern", Track: "frontend", MentorID: "u-mentor", StartDate: &fe},
		{UserID: "u-mentor", DisplayName: "Наставник (тестовый)", Role: "mentor"},
		{UserID: "u-intern-other", DisplayName: "Чужой стажёр (фикстура)", Role: "intern", Track: "backend", MentorID: "u-mentor-other", StartDate: &other},
		{UserID: "u-mentor-other", DisplayName: "Другой наставник (фикстура)", Role: "mentor"},
	}
}

// EmployeeProvider returns a static provider over Employees().
func EmployeeProvider() employees.Provider { return employees.NewStatic(Employees()) }

// Ctx returns a context canceled at the end of the test.
func Ctx(t testing.TB) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
