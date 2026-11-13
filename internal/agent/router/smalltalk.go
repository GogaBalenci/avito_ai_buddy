package router

import (
	"context"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
)

// smallTalk answers chit-chat with a template in Vitya's voice and a soft way
// back to work (09 §17.3, AG-29): no model, no write tools; the intern's path
// is only read for the next step.
func (g *Agent) smallTalk(ctx context.Context, a actor.Actor, in Input, kind string) *api.AgentReply {
	r := reply.New(in.ClientMessageID, rules.Help)
	variants, ok := smallTalkReplies[kind]
	if !ok {
		return g.help(in, a)
	}
	r.Text = pick(in.ClientMessageID, variants)
	if a.Role != actor.Intern {
		r.Text += " Если что — стажёры и их заявки на экранах «Мои стажёры» и «Входящие»."
		menu(r, a.Role)
		return r
	}
	l, err := g.Path.Load(ctx, a, "")
	if err != nil {
		r.Text += " Чем помочь?"
		menu(r, a.Role)
		return r
	}
	switch l.State {
	case path.StateFinished:
		r.Text += " А по делу: все шаги пройдены — обсуди итоги с наставником на 1:1."
		return r
	case path.StatePreStart:
		r.Text += " А стажировка начнётся " + ddmm(l.Start) + " — можно заглянуть в первую неделю."
		return r
	case path.StateOnlyWaiting:
		if w := waitingTitles(l); len(w) > 0 {
			r.Text += " А по делу: ждём ответа по «" + w[0] + "»."
		}
		reply.Quick(r, "Обновить", "intent:my_path")
		return r
	}
	if l.Next != nil {
		if s, ok := l.Step(l.Next.StepID); ok {
			r.Text += " А у тебя, кстати, следующий шаг — «" + s.Title + "»."
			reply.Quick(r, stepButton(s)+": "+s.Title, "step:"+s.ID)
			return r
		}
	}
	r.Text += " Чем помочь?"
	menu(r, a.Role)
	return r
}
