package guard

import (
	"context"
	"errors"
	"testing"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

// TestPolicyMatrix checks all 3 × 15 = 45 role × permission pairs against the
// expected table of 13 §4. A new permission without a row here fails the test.
func TestPolicyMatrix(t *testing.T) {
	want := map[Permission][3]bool{ // intern, mentor, system
		PathRead:         {true, true, false},
		PathUpdate:       {true, false, false},
		PathSync:         {false, false, true},
		KBSearch:         {true, true, false},
		KBRead:           {true, true, false},
		AbsencesCreate:   {true, false, false},
		AbsencesList:     {true, true, false},
		AccessCatalog:    {true, true, true},
		AccessCreate:     {true, false, false},
		AccessList:       {true, true, true},
		AccessDecide:     {false, true, false},
		ChatUse:          {true, true, false},
		ChatInternRoutes: {true, false, false},
		MentorDashboard:  {false, true, false},
		DemoManage:       {true, true, false},
	}
	if len(want) != len(AllPermissions) {
		t.Fatalf("expected table has %d rows, policy has %d permissions", len(want), len(AllPermissions))
	}
	pairs := 0
	for _, p := range AllPermissions {
		row, ok := want[p]
		if !ok {
			t.Fatalf("permission %s has no expected row", p)
		}
		for i, role := range AllRoles {
			pairs++
			if got := Can(role, p); got != row[i] {
				t.Errorf("%s × %s = %v, want %v", role, p, got, row[i])
			}
		}
	}
	if pairs != 45 {
		t.Fatalf("pairs = %d", pairs)
	}
	for role := range policy {
		for p := range policy[role] {
			if _, ok := want[p]; !ok {
				t.Errorf("policy has unknown permission %s", p)
			}
		}
	}
}

func TestConditions(t *testing.T) {
	g := &Guard{Employees: testkit.EmployeeProvider()}
	ctx := context.Background()
	intern := actor.Actor{UserID: "u-intern-be", Role: actor.Intern}
	mentor := actor.Actor{UserID: "u-mentor", Role: actor.Mentor}
	sys := actor.Actor{UserID: "svc-buddy", Role: actor.System}
	cases := []struct {
		name string
		a    actor.Actor
		p    Permission
		o    Object
		want error
	}{
		{"own path", intern, PathRead, Object{}, nil},
		{"foreign path is 404", intern, PathRead, Object{EmployeeID: "u-intern-fe"}, ErrNotFound},
		{"mentor own mentee", mentor, PathRead, Object{EmployeeID: "u-intern-fe"}, nil},
		{"mentor foreign intern 404", mentor, PathRead, Object{EmployeeID: "u-intern-other"}, ErrNotFound},
		{"mentor without employee", mentor, AccessList, Object{}, ErrDenied},
		{"mentor writes path", mentor, PathUpdate, Object{EmployeeID: "u-intern-be"}, ErrDenied},
		{"user_start", intern, PathUpdate, Object{Reason: "user_start", HelpKind: "access_request"}, nil},
		{"mark done access step", intern, PathUpdate, Object{Reason: "user_mark_done", HelpKind: "access_request"}, ErrDenied},
		{"mark done", intern, PathUpdate, Object{Reason: "user_mark_done", HelpKind: "explain"}, nil},
		{"intern system reason", intern, PathUpdate, Object{Reason: "access_approved"}, ErrDenied},
		{"intern sync", intern, PathSync, Object{}, ErrDenied},
		{"system sync", sys, PathSync, Object{EmployeeID: "u-intern-be", Reason: "access_approved", HelpKind: "access_request", RequestRef: "ACC-000101"}, nil},
		{"system without ref", sys, PathSync, Object{EmployeeID: "u-intern-be", Reason: "access_approved", HelpKind: "access_request"}, ErrDenied},
		{"system user reason", sys, PathSync, Object{EmployeeID: "u-intern-be", Reason: "user_mark_done", HelpKind: "access_request", RequestRef: "ACC-000101"}, ErrDenied},
		{"system list needs employee", sys, AccessList, Object{}, ErrDenied},
		{"system list", sys, AccessList, Object{EmployeeID: "u-intern-be"}, nil},
		{"closed doc", intern, KBRead, Object{DocRoles: []string{"mentor"}}, ErrDenied},
		{"mentor doc", mentor, KBRead, Object{DocRoles: []string{"mentor"}}, nil},
		{"prod-db-write", intern, AccessCreate, Object{ResourceRoles: nil}, ErrDenied},
		{"team channels", intern, AccessCreate, Object{ResourceRoles: []string{"intern"}}, nil},
		{"access create for other", intern, AccessCreate, Object{EmployeeID: "u-intern-fe", ResourceRoles: []string{"intern"}}, ErrNotFound},
		{"decide pending", mentor, AccessDecide, Object{EmployeeID: "u-intern-be", RequestStatus: "pending"}, nil},
		{"decide decided", mentor, AccessDecide, Object{EmployeeID: "u-intern-be", RequestStatus: "approved"}, ErrDenied},
		{"decide foreign", mentor, AccessDecide, Object{EmployeeID: "u-intern-other", RequestStatus: "pending"}, ErrNotFound},
		{"intern decides", intern, AccessDecide, Object{EmployeeID: "u-intern-be", RequestStatus: "pending"}, ErrDenied},
		{"mentor time off", mentor, AbsencesCreate, Object{}, ErrDenied},
		{"mentor routes", mentor, ChatInternRoutes, Object{}, ErrDenied},
		{"unknown role", actor.Actor{UserID: "u-x", Role: "lead"}, ChatUse, Object{}, ErrDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := g.Authorize(ctx, c.a, c.p, c.o); !errors.Is(err, c.want) && (err != nil || c.want != nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	if err := (&Guard{}).Authorize(ctx, mentor, PathRead, Object{EmployeeID: "u-intern-be"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("guard without employee data must deny")
	}
}
