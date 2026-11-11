// Package guard is layer 2 of the permission model (13 §9, row 4):
// role × permission × object, checked before every MCP call. The table below
// is the matrix of 13 §4 as data — RBAC (role → permissions) plus ABAC
// conditions on the object. Missing key = deny (deny by default).
package guard

import (
	"context"
	"errors"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
)

// Permission is buddy.<service>.<action> (13 §3).
type Permission string

// Permissions.
const (
	PathRead         Permission = "buddy.path.read"
	PathUpdate       Permission = "buddy.path.update"
	PathSync         Permission = "buddy.path.sync"
	KBSearch         Permission = "buddy.kb.search"
	KBRead           Permission = "buddy.kb.read"
	AbsencesCreate   Permission = "buddy.absences.create"
	AbsencesList     Permission = "buddy.absences.list"
	AccessCatalog    Permission = "buddy.access.catalog"
	AccessCreate     Permission = "buddy.access.create"
	AccessList       Permission = "buddy.access.list"
	AccessDecide     Permission = "buddy.access.decide"
	ChatUse          Permission = "buddy.chat.use"
	ChatInternRoutes Permission = "buddy.chat.intern_routes"
	MentorDashboard  Permission = "buddy.mentor.dashboard"
	DemoManage       Permission = "buddy.demo.manage"
)

// AllPermissions lists every permission (the matrix test iterates them).
var AllPermissions = []Permission{PathRead, PathUpdate, PathSync, KBSearch, KBRead, AbsencesCreate,
	AbsencesList, AccessCatalog, AccessCreate, AccessList, AccessDecide, ChatUse, ChatInternRoutes,
	MentorDashboard, DemoManage}

// AllRoles lists the roles of the matrix.
var AllRoles = []string{actor.Intern, actor.Mentor, actor.System}

// Condition is an ABAC condition (13 §5).
type Condition int

// Conditions.
const (
	Any Condition = iota + 1
	Self
	SelfUserReason
	DocAllowed
	SelfAndResourceAllowed
	MenteeReadOnly
	Mentee
	MenteePending
	SystemReason
	EmployeeRequired
)

var policy = map[string]map[Permission]Condition{
	actor.Intern: {
		PathRead: Self, PathUpdate: SelfUserReason, KBSearch: DocAllowed, KBRead: DocAllowed,
		AbsencesCreate: Self, AbsencesList: Self, AccessCatalog: Any,
		AccessCreate: SelfAndResourceAllowed, AccessList: Self,
		ChatUse: Any, ChatInternRoutes: Any, DemoManage: Any,
	},
	actor.Mentor: {
		PathRead: MenteeReadOnly, KBSearch: DocAllowed, KBRead: DocAllowed,
		AbsencesList: Mentee, AccessCatalog: Any, AccessList: Mentee,
		AccessDecide: MenteePending, ChatUse: Any, MentorDashboard: Mentee, DemoManage: Any,
	},
	actor.System: {PathSync: SystemReason, AccessCatalog: Any, AccessList: EmployeeRequired},
}

// Object holds the attributes conditions look at.
type Object struct {
	EmployeeID    string   // target employee; "" means the actor itself (self)
	DocRoles      []string // allowed_roles of a document
	ResourceRoles []string // allowed_roles of an access resource
	Reason        string   // set_step_status reason
	HelpKind      string   // help.kind of the step
	RequestRef    string   // ACC-… for system transitions
	RequestStatus string   // status of an access request (decide)
}

// Errors: 403 — known object, action not allowed; 404 — foreign object whose
// existence must not be revealed (13 §7, «Правило 404 или 403»).
var (
	ErrDenied   = errors.New("permission denied")
	ErrNotFound = errors.New("not found")
)

// Guard evaluates the policy; mentee needs the employee data.
type Guard struct{ Employees employees.Provider }

// Can reports the RBAC part only: the role has the permission at all.
func Can(role string, p Permission) bool { _, ok := policy[role][p]; return ok }

// Authorize checks role × permission × object.
func (g *Guard) Authorize(ctx context.Context, a actor.Actor, p Permission, o Object) error {
	cond, ok := policy[a.Role][p]
	if !ok {
		return ErrDenied
	}
	switch cond {
	case Any:
		return nil
	case Self:
		return self(a, o)
	case SelfUserReason:
		if err := self(a, o); err != nil {
			return err
		}
		switch o.Reason {
		case "user_start":
			return nil
		case "user_mark_done", "user_reopen":
			if o.HelpKind == "access_request" {
				return ErrDenied
			}
			return nil
		}
		return ErrDenied
	case DocAllowed:
		if contains(o.DocRoles, a.Role) {
			return nil
		}
		return ErrDenied
	case SelfAndResourceAllowed:
		if err := self(a, o); err != nil {
			return err
		}
		if contains(o.ResourceRoles, a.Role) {
			return nil
		}
		return ErrDenied
	case Mentee, MenteeReadOnly:
		return g.mentee(ctx, a, o)
	case MenteePending:
		if err := g.mentee(ctx, a, o); err != nil {
			return err
		}
		if o.RequestStatus != "pending" {
			return ErrDenied
		}
		return nil
	case SystemReason:
		switch o.Reason {
		case "access_pending", "access_approved", "access_rejected":
		default:
			return ErrDenied
		}
		if o.HelpKind != "access_request" || o.RequestRef == "" || o.EmployeeID == "" {
			return ErrDenied
		}
		return nil
	case EmployeeRequired:
		if o.EmployeeID == "" {
			return ErrDenied
		}
		return nil
	}
	return ErrDenied
}

func self(a actor.Actor, o Object) error {
	if o.EmployeeID == "" || o.EmployeeID == a.UserID {
		return nil
	}
	return ErrNotFound
}

// mentee: the object's employee is an intern of this mentor; the employee id
// is required (a mentor has no own path or requests).
func (g *Guard) mentee(ctx context.Context, a actor.Actor, o Object) error {
	if o.EmployeeID == "" {
		return ErrDenied
	}
	if g.Employees == nil || !employees.IsMentee(ctx, g.Employees, a.UserID, o.EmployeeID) {
		return ErrNotFound
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
