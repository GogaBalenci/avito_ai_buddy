// Package actor is the principal of every call: who and in which role (13 §1).
// Identity and role come only from the session; the service role `system`
// cannot come from a session (13 §2).
package actor

// Roles.
const (
	Intern = "intern"
	Mentor = "mentor"
	System = "system"
)

// Actor is passed to every MCP tool as `actor` (contracts/common/actor.v1.json).
type Actor struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// IsIntern reports the intern role.
func (a Actor) IsIntern() bool { return a.Role == Intern }

// IsMentor reports the mentor role.
func (a Actor) IsMentor() bool { return a.Role == Mentor }
