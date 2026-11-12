// Package sysactor builds the service principal svc-buddy with the role
// `system` (13 §2). Only internal/access/sync may import it (depguard): the
// role cannot come from a session, a request body or a model answer.
package sysactor

import "github.com/GogaBalenci/avito_ai_buddy/internal/actor"

// UserID of the service principal.
const UserID = "svc-buddy"

// New returns the system actor.
func New() actor.Actor { return actor.Actor{UserID: UserID, Role: actor.System} }
