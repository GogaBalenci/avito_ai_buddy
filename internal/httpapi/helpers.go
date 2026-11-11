package httpapi

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
)

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mustUUID(s string) uuid.UUID { u, _ := uuid.Parse(s); return u }

func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }

// summaryJSON builds ActionSummary from the draft summary map.
func summaryJSON(d actions.Draft) api.ActionSummary {
	var s api.ActionSummary
	b, _ := json.Marshal(d.Summary)
	_ = json.Unmarshal(b, &s)
	return s
}

// pendingJSON converts a draft to PendingAction.
func pendingJSON(st *actions.Store, d actions.Draft) api.PendingAction {
	return api.PendingAction{ActionId: mustUUID(d.ActionID), Kind: api.RequestKind(d.Kind),
		State: api.PendingActionState(st.ViewState(d)), ExpiresAt: d.ExpiresAt, Summary: summaryJSON(d)}
}
