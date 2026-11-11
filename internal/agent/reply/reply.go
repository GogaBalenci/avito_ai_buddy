// Package reply builds AgentReply (09 §9.1) from demo templates (09 §9.3).
// Text is plain text only; [n] markers refer to sources (AG-26); badges and
// sources are drawn by the UI from structured fields, not from the text.
package reply

import (
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
)

// New returns an empty reply in demo mode with the hello mascot.
func New(clientMessageID uuid.UUID, intent string) *api.AgentReply {
	return &api.AgentReply{
		ReplyId:         openapi_types.UUID(uuid.New()),
		ClientMessageId: openapi_types.UUID(clientMessageID),
		Mode:            api.AgentReplyModeDemo,
		Intent:          api.AgentReplyIntent(intent),
		MascotState:     api.MascotStateHello,
		Sources:         []api.Source{},
		Actions:         []api.ReplyAction{},
		Notices:         []api.AgentReplyNotices{},
	}
}

// Notice adds a notice once.
func Notice(r *api.AgentReply, n string) {
	for _, x := range r.Notices {
		if string(x) == n {
			return
		}
	}
	r.Notices = append(r.Notices, api.AgentReplyNotices(n))
}

// Quick adds a quick reply button with a payload (09 §2.1 grammar).
func Quick(r *api.AgentReply, label, payload string) {
	p := payload
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.QuickReply, Label: label, Payload: &p})
}

// Copy adds a "copy to clipboard" action with the event to report.
func Copy(r *api.AgentReply, label, text, event string) {
	t := text
	a := api.ReplyAction{Type: api.Copy, Label: label, Text: &t}
	if event != "" {
		ev := api.ReplyActionCopyEvent(event)
		a.CopyEvent = &ev
	}
	r.Actions = append(r.Actions, a)
}

// Link adds an in-app link.
func Link(r *api.AgentReply, label, url string) {
	u := url
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.Link, Label: label, Url: &u})
}

// Source adds a numbered source and returns its number.
func Source(r *api.AgentReply, slug, title, label, sourceLevel string, dated *string) int {
	n := len(r.Sources) + 1
	r.Sources = append(r.Sources, api.Source{N: n, Slug: slug, Title: title, Url: "/kb/" + slug,
		Label: api.SourceLabel(label), SourceLevel: api.SourceSourceLevel(sourceLevel), Dated: dated})
	return n
}
