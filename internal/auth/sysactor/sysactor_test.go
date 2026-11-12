package sysactor

import "testing"

func TestNew(t *testing.T) {
	if a := New(); a.UserID != "svc-buddy" || a.Role != "system" {
		t.Fatalf("%+v", a)
	}
}
