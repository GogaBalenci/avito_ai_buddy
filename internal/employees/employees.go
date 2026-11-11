// Package employees loads the synthetic test accounts (content/seeds/employees.yaml)
// and defines EmployeeProvider — the replacement point for the corporate
// employee data service (13 §5: the mentor–intern link lives there).
package employees

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

// Roles of a person; `system` is never an employee (13 §2).
const (
	RoleIntern = "intern"
	RoleMentor = "mentor"
)

// Employee is a person the buddy knows about.
type Employee struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Track       string `json:"track,omitempty"`
	MentorID    string `json:"mentor_id,omitempty"`
	Description string `json:"description,omitempty"`
	// Seed settings: start = today − days ago (env var or default).
	StartDaysAgoEnv     string `json:"start_days_ago_env,omitempty"`
	StartDaysAgoDefault int    `json:"start_days_ago_default,omitempty"`

	// StartDate is filled by the api from app.employees; mocks do not need it.
	StartDate *time.Time `json:"-"`
}

// ErrNotFound is returned for an unknown user id.
var ErrNotFound = errors.New("employee not found")

// Provider is EmployeeProvider (12 §5.1).
type Provider interface {
	Get(ctx context.Context, userID string) (Employee, error)
	InternsOf(ctx context.Context, mentorID string) ([]Employee, error)
}

type file struct {
	Version   int        `json:"version"`
	Employees []Employee `json:"employees"`
}

var userIDRe = regexp.MustCompile(`^u-[a-z0-9-]{1,40}$`)

// Load reads employees.yaml from fsys (the seeds directory) and checks it.
// knownTracks — ids of loaded directions; nil skips the track check.
func Load(fsys fs.FS, knownTracks map[string]bool) ([]Employee, error) {
	raw, err := fs.ReadFile(fsys, "employees.yaml")
	if err != nil {
		return nil, err
	}
	var f file
	if _, err := yamlx.Decode(raw, &f); err != nil {
		return nil, fmt.Errorf("seeds/employees.yaml: %w", err)
	}
	if err := Validate(f.Employees, knownTracks); err != nil {
		return nil, err
	}
	return f.Employees, nil
}

// Validate checks ids, roles, tracks and the mentor link (P-A16).
func Validate(list []Employee, knownTracks map[string]bool) error {
	byID := map[string]Employee{}
	var errs []error
	for _, e := range list {
		if !userIDRe.MatchString(e.UserID) {
			errs = append(errs, fmt.Errorf("employees: bad user_id %q", e.UserID))
		}
		if _, dup := byID[e.UserID]; dup {
			errs = append(errs, fmt.Errorf("employees: duplicate user_id %q", e.UserID))
		}
		byID[e.UserID] = e
	}
	for _, e := range list {
		switch e.Role {
		case RoleIntern:
			if e.Track == "" || e.MentorID == "" {
				errs = append(errs, fmt.Errorf("employees: intern %s needs track and mentor_id", e.UserID))
			}
			if knownTracks != nil && e.Track != "" && !knownTracks[e.Track] {
				errs = append(errs, fmt.Errorf("employees: intern %s has unknown track %q", e.UserID, e.Track))
			}
			if m, ok := byID[e.MentorID]; e.MentorID != "" && (!ok || m.Role != RoleMentor) {
				errs = append(errs, fmt.Errorf("employees: intern %s: mentor %q is not a mentor", e.UserID, e.MentorID))
			}
		case RoleMentor:
			if e.Track != "" || e.MentorID != "" {
				errs = append(errs, fmt.Errorf("employees: mentor %s must not have track or mentor_id", e.UserID))
			}
		default:
			errs = append(errs, fmt.Errorf("employees: %s has unknown role %q", e.UserID, e.Role))
		}
	}
	return errors.Join(errs...)
}

// StartDaysAgo returns how many days ago an intern started, from the env var
// named in the seed or the default.
func (e Employee) StartDaysAgo(getenv func(string) string) int {
	if e.StartDaysAgoEnv != "" && getenv != nil {
		if v, err := strconv.Atoi(getenv(e.StartDaysAgoEnv)); err == nil {
			return v
		}
	}
	return e.StartDaysAgoDefault
}

// Static is an in-memory Provider (used by mcp-mocks and tests).
type Static struct{ byID map[string]Employee }

// NewStatic builds a Static provider.
func NewStatic(list []Employee) *Static {
	s := &Static{byID: map[string]Employee{}}
	for _, e := range list {
		s.byID[e.UserID] = e
	}
	return s
}

// Get returns an employee.
func (s *Static) Get(_ context.Context, id string) (Employee, error) {
	e, ok := s.byID[id]
	if !ok {
		return Employee{}, ErrNotFound
	}
	return e, nil
}

// InternsOf returns interns of a mentor, sorted by user id.
func (s *Static) InternsOf(_ context.Context, mentorID string) ([]Employee, error) {
	var out []Employee
	for _, e := range s.byID {
		if e.Role == RoleIntern && e.MentorID == mentorID {
			out = append(out, e)
		}
	}
	sortByID(out)
	return out, nil
}

func sortByID(xs []Employee) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].UserID < xs[j-1].UserID; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// IsMentee reports the ABAC condition `mentee` (13 §5).
func IsMentee(ctx context.Context, p Provider, mentorID, internID string) bool {
	e, err := p.Get(ctx, internID)
	return err == nil && e.Role == RoleIntern && e.MentorID == mentorID
}
