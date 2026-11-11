package fsm

import (
	"errors"
	"testing"
)

// P-07: allowed transitions succeed, every other combination is refused,
// a transition into the current status is a no-op.
func TestCheckAllCombinations(t *testing.T) {
	statuses := []string{NotStarted, InProgress, Waiting, Done}
	allowed := map[string]bool{ // reason|from|to|help
		UserStart + "|" + NotStarted + "|" + InProgress:   true,
		UserMarkDone + "|" + NotStarted + "|" + Done:      true,
		UserMarkDone + "|" + InProgress + "|" + Done:      true,
		UserReopen + "|" + Done + "|" + InProgress:        true,
		AccessPending + "|" + NotStarted + "|" + Waiting:  true,
		AccessPending + "|" + InProgress + "|" + Waiting:  true,
		AccessApproved + "|" + NotStarted + "|" + Done:    true,
		AccessApproved + "|" + InProgress + "|" + Done:    true,
		AccessApproved + "|" + Waiting + "|" + Done:       true,
		AccessRejected + "|" + Waiting + "|" + InProgress: true,
	}
	refused, ok := 0, 0
	for reason := range rules {
		help := "explain"
		ref := ""
		if IsSystemReason(reason) {
			help, ref = HelpAccessRequest, "ACC-000101"
		}
		to, _ := Target(reason)
		for _, from := range statuses {
			changed, err := Check(from, to, reason, help, ref)
			switch {
			case from == to:
				if err != nil || changed {
					t.Errorf("%s %s→%s: no-op expected, got %v %v", reason, from, to, changed, err)
				}
			case allowed[reason+"|"+from+"|"+to]:
				ok++
				if err != nil || !changed {
					t.Errorf("%s %s→%s refused: %v", reason, from, to, err)
				}
			default:
				refused++
				if !errors.Is(err, ErrNotAllowed) {
					t.Errorf("%s %s→%s: want TRANSITION_NOT_ALLOWED, got %v", reason, from, to, err)
				}
			}
		}
	}
	if ok != 10 {
		t.Fatalf("allowed = %d", ok)
	}
	_ = refused
}

func TestCheckConstraints(t *testing.T) {
	if _, err := Check(InProgress, Done, UserMarkDone, HelpAccessRequest, ""); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("C-06: mark done on an access step")
	}
	if _, err := Check(Done, InProgress, UserReopen, HelpAccessRequest, ""); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("reopen access step")
	}
	if _, err := Check(Waiting, Done, AccessApproved, HelpAccessRequest, ""); !errors.Is(err, ErrRequestRefNeed) {
		t.Fatal("C-07: system without request_ref")
	}
	if _, err := Check(Waiting, Done, AccessApproved, "explain", "ACC-000101"); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("access reason on explain step")
	}
	if _, err := Check(NotStarted, Done, UserStart, "explain", ""); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("target does not match reason")
	}
	if _, err := Check(NotStarted, Done, "hack", "explain", ""); !errors.Is(err, ErrUnknownReason) {
		t.Fatal("unknown reason")
	}
	if !IsSystemReason(AccessPending) || IsSystemReason(UserStart) || IsSystemReason("x") {
		t.Fatal("IsSystemReason")
	}
	for _, c := range [][3]string{{InProgress, Done, UserMarkDone}, {Done, InProgress, UserReopen}, {NotStarted, InProgress, UserStart}} {
		if got := UserReasonFor(c[0], c[1]); got != c[2] {
			t.Errorf("UserReasonFor(%s,%s) = %s", c[0], c[1], got)
		}
	}
}
