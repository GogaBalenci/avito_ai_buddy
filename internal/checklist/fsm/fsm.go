// Package fsm is the step status transition table (11 §1.5.3). Shared by the
// API and the checklist mock, so both check transitions the same way.
package fsm

import "errors"

// Statuses of a step.
const (
	NotStarted = "not_started"
	InProgress = "in_progress"
	Waiting    = "waiting"
	Done       = "done"
)

// Reasons of a transition: user_* come from the intern, access_* from the
// buddy's sync of access requests (role system).
const (
	UserStart      = "user_start"
	UserMarkDone   = "user_mark_done"
	UserReopen     = "user_reopen"
	AccessPending  = "access_pending"
	AccessApproved = "access_approved"
	AccessRejected = "access_rejected"
)

// HelpAccessRequest is help.kind of steps closed by access requests.
const HelpAccessRequest = "access_request"

// Errors.
var (
	ErrNotAllowed     = errors.New("TRANSITION_NOT_ALLOWED")
	ErrRequestRefNeed = errors.New("request_ref is required for access_* reasons")
	ErrUnknownReason  = errors.New("unknown reason")
)

type rule struct {
	from     []string
	to       string
	system   bool
	helpKind string // "" any; "=access_request"; "!access_request"
}

var rules = map[string]rule{
	UserStart:      {from: []string{NotStarted}, to: InProgress},
	UserMarkDone:   {from: []string{NotStarted, InProgress}, to: Done, helpKind: "!" + HelpAccessRequest},
	UserReopen:     {from: []string{Done}, to: InProgress, helpKind: "!" + HelpAccessRequest},
	AccessPending:  {from: []string{NotStarted, InProgress}, to: Waiting, system: true, helpKind: "=" + HelpAccessRequest},
	AccessApproved: {from: []string{NotStarted, InProgress, Waiting}, to: Done, system: true, helpKind: "=" + HelpAccessRequest},
	AccessRejected: {from: []string{Waiting}, to: InProgress, system: true, helpKind: "=" + HelpAccessRequest},
}

// IsSystemReason reports access_* reasons.
func IsSystemReason(reason string) bool { r, ok := rules[reason]; return ok && r.system }

// Target returns the status a reason leads to.
func Target(reason string) (string, bool) { r, ok := rules[reason]; return r.to, ok }

// Check validates current → target by reason. A transition into the current
// status is a successful no-op (changed=false): repeated sync is safe.
func Check(current, target, reason, helpKind, requestRef string) (changed bool, err error) {
	r, ok := rules[reason]
	if !ok {
		return false, ErrUnknownReason
	}
	if r.system && requestRef == "" {
		return false, ErrRequestRefNeed
	}
	if target != r.to {
		return false, ErrNotAllowed
	}
	switch r.helpKind {
	case "=" + HelpAccessRequest:
		if helpKind != HelpAccessRequest {
			return false, ErrNotAllowed
		}
	case "!" + HelpAccessRequest:
		if helpKind == HelpAccessRequest {
			return false, ErrNotAllowed
		}
	}
	if current == target {
		return false, nil
	}
	for _, f := range r.from {
		if f == current {
			return true, nil
		}
	}
	return false, ErrNotAllowed
}

// UserReasonFor derives the reason of a user request (11 §1.10): done →
// user_mark_done; in_progress from done → user_reopen; otherwise user_start.
func UserReasonFor(current, target string) string {
	switch {
	case target == Done:
		return UserMarkDone
	case target == InProgress && current == Done:
		return UserReopen
	default:
		return UserStart
	}
}
