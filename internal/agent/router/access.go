package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GogaBalenci/avito_ai_buddy/internal/access"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
)

// catalogAliases gives the recognizer the aliases of the catalog (11 §3.5.4).
func (g *Agent) catalogAliases(ctx context.Context, a actor.Actor) []rules.Alias {
	if g.Catalog == nil {
		return nil
	}
	rs, err := g.Catalog.List(ctx, a)
	if err != nil {
		return nil
	}
	var out []rules.Alias
	for _, r := range rs {
		for _, al := range r.Aliases {
			out = append(out, rules.Alias{Phrase: al, ResourceID: r.ResourceID})
		}
	}
	return out
}

// resourcePayload handles resource:<id> (11 §3.9: 404 not in the catalog,
// 403 not allowed for the role).
func (g *Agent) resourcePayload(ctx context.Context, a actor.Actor, in Input, id string) (Output, error) {
	if a.Role != actor.Intern {
		return Output{Reply: g.mentorTemplate(ctx, a, in, rules.AccessRequest), ButtonLabel: "Запросить доступ"}, nil
	}
	res, ok, err := g.Catalog.Get(ctx, a, id)
	if err != nil {
		r := reply.New(in.ClientMessageID, rules.AccessRequest)
		r.MascotState = api.MascotStateOops
		r.Text = "Сервис доступов сейчас не отвечает. Попробуй ещё раз."
		retry(r, "resource:"+id)
		return Output{Reply: r, ButtonLabel: "Запросить доступ"}, nil
	}
	if !ok {
		return Output{}, &Error{http.StatusNotFound, "NOT_FOUND", "Такого ресурса в тестовом каталоге нет"}
	}
	label := "Запросить доступ: " + res.Title
	if !res.Allowed {
		g.publish(ctx, "permission_denied", map[string]any{"tool": "access.create_request", "object": id, "role": a.Role})
		return Output{}, &Error{http.StatusForbidden, "PERMISSION_DENIED", "Этот доступ роли стажёра не положен"}
	}
	g.recognized(ctx, a, recognition{Result: rules.Result{Intent: rules.AccessRequest, Confidence: 1}, Source: "button"})
	return Output{Reply: g.accessDraft(ctx, a, in, id, actions.FromQuickReply), ButtonLabel: label}, nil
}

// accessRequest is the text route (11 §3.7, p. 1–2).
func (g *Agent) accessRequest(ctx context.Context, a actor.Actor, in Input, rec recognition) *api.AgentReply {
	if rec.ResourceID != "" {
		from := actions.FromChat
		if rec.Source == "button" {
			from = actions.FromQuickReply
		}
		return g.accessDraft(ctx, a, in, rec.ResourceID, from)
	}
	return g.chooseResource(ctx, a, in)
}

// chooseResource lists the resources allowed to the role (X-A6, T-10).
func (g *Agent) chooseResource(ctx context.Context, a actor.Actor, in Input) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.AccessRequest)
	rs, err := g.Catalog.List(ctx, a)
	if err != nil {
		r.MascotState = api.MascotStateOops
		r.Text = "Сервис доступов сейчас не отвечает. Попробуй ещё раз."
		retry(r, "intent:access_request")
		return r
	}
	r.Text = "Какой доступ нужен? Выбери из тестового каталога:"
	for _, x := range rs {
		if x.Allowed {
			reply.Quick(r, x.Title, "resource:"+x.ResourceID)
		}
	}
	return r
}

// accessDraft creates the draft and builds the card text, or explains why not.
func (g *Agent) accessDraft(ctx context.Context, a actor.Actor, in Input, resourceID, from string) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.AccessRequest)
	d, err := g.Actions.CreateAccess(ctx, a, resourceID, from)
	var ae *actions.Error
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusForbidden: // X-A1
			r.MascotState = api.MascotStateAskMentor
			expl := ae.Message
			r.Text = strings.TrimSuffix(expl, ".") + ", поэтому заявку не создаю. Если он нужен для задачи — давай обсудим с наставником, какой доступ подойдёт."
			title := resourceID
			if ae.Resource != nil {
				title = ae.Resource.Title
			}
			period, step := g.contextOf(ctx, a, nil)
			g.escalate(ctx, a, r, "permission_denied", "Нужен ли мне доступ «"+title+"» для задачи и какой доступ подойдёт вместо него?", period, step, nil)
			return r
		case http.StatusNotFound:
			r.Text = "Такого ресурса в тестовом каталоге нет."
			return r
		case http.StatusConflict:
			return g.existingAccess(ctx, r, ae)
		}
		r.MascotState = api.MascotStateOops
		r.Text = ae.Message
		retry(r, "resource:"+resourceID)
		return r
	}
	if err != nil {
		r.MascotState = api.MascotStateOops
		r.Text = "Сервис доступов сейчас не отвечает. Попробуй ещё раз."
		retry(r, "resource:"+resourceID)
		return r
	}
	sm := actions.Summary(d)
	title, level, policy, just := str(sm.Title), "", "", str(sm.Justification)
	if sm.AccessLevel != nil {
		level = access.LevelRU(string(*sm.AccessLevel))
	}
	if sm.EffectivePolicy != nil {
		policy = string(*sm.EffectivePolicy)
	}
	r.Text = fmt.Sprintf("Проверь: заявка на «%s», уровень «%s». %s. Обоснование: %s. Ничего не отправлю, пока не нажмёшь «Подтвердить». Черновик действует до %s.",
		title, level, access.PolicyHint(policy, sm.TimerMinutes), just, d.ExpiresAt.In(g.Clock.Now().Location()).Format("15:04"))
	if doc, err := g.Docs.Doc(ctx, a, "access-test-order"); err == nil {
		n := reply.Source(r, doc.Slug, doc.Title, doc.Label, doc.SourceLevel, doc.Dated)
		r.Text += " Порядок — тестовый [" + strconv.Itoa(n) + "]."
	}
	pa := g.Actions.Store.Pending(d)
	r.PendingAction = &pa
	id := pa.ActionId
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.Confirm, Label: "Подтвердить", ActionId: &id},
		api.ReplyAction{Type: api.Cancel, Label: "Отмена", ActionId: &id})
	return r
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// existingAccess answers X-A4 / X-A5.
func (g *Agent) existingAccess(ctx context.Context, r *api.AgentReply, ae *actions.Error) *api.AgentReply {
	ex := ae.Existing
	if ex == nil {
		r.Text = "Заявка на этот доступ уже есть."
		return r
	}
	if ex.Status == "approved" {
		r.MascotState = api.MascotStateDone
		r.Text = "Доступ уже одобрен (заявка №" + ex.RequestId + ")."
		return r
	}
	who := "наставника"
	if ex.Policy != nil && *ex.Policy == "timer" && ex.AutoApproveAt != nil {
		who = "автоматического одобрения около " + ex.AutoApproveAt.In(g.Clock.Now().Location()).Format("15:04")
	}
	r.Text = "Заявка №" + ex.RequestId + " уже ждёт " + who + "."
	reply.Quick(r, "Обновить", "intent:my_requests")
	return r
}

// accessHint adds «Запросить доступ: …» under a reply that is not an access
// request but mentions an allowed resource (11 §3.5.4, p. 2; A-26).
func (g *Agent) accessHint(ctx context.Context, a actor.Actor, r *api.AgentReply, resourceID string) {
	if a.Role != actor.Intern || g.Catalog == nil || resourceID == "" {
		return
	}
	res, ok, err := g.Catalog.Get(ctx, a, resourceID)
	if err != nil || !ok || !res.Allowed {
		return
	}
	for _, x := range r.Actions {
		if x.Payload != nil && *x.Payload == "resource:"+resourceID {
			return
		}
	}
	reply.Quick(r, "Запросить доступ: "+res.Title, "resource:"+resourceID)
}

// accessStep is the main button of a step with help.kind = access_request.
func (g *Agent) accessStep(ctx context.Context, a actor.Actor, in Input, s path.Step) *api.AgentReply {
	return g.accessDraft(ctx, a, in, s.Resource, actions.FromStep)
}
