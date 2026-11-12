package router

import (
	"context"
	"fmt"
	"strconv"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

// Hit is an item of knowledge.search.
type Hit struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	Summary       string   `json:"summary"`
	Snippet       string   `json:"snippet"`
	Rank          float64  `json:"rank"`
	Label         string   `json:"label"`
	SourceLevel   string   `json:"source_level"`
	Dated         *string  `json:"dated"`
	URL           string   `json:"url"`
	ConflictsWith []string `json:"conflicts_with"`
}

// SearchResult is knowledge/search.output.
type SearchResult struct {
	Items  []Hit `json:"items"`
	Denied []struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"denied"`
}

// MCPDocs reads documents through knowledge.get_doc (the rights are checked
// by the knowledge service; a closed document does not exist for get_doc).
type MCPDocs struct{ Tools mcpclient.Client }

// Doc implements DocSource.
func (m MCPDocs) Doc(ctx context.Context, a actor.Actor, slug string) (Doc, error) {
	var out struct {
		Doc struct {
			Slug          string   `json:"slug"`
			Title         string   `json:"title"`
			Summary       string   `json:"summary"`
			Label         string   `json:"label"`
			SourceLevel   string   `json:"source_level"`
			Dated         *string  `json:"dated"`
			ConflictsWith []string `json:"conflicts_with"`
		} `json:"doc"`
	}
	err := m.Tools.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": a, "slug": slug}, &out)
	if mcpclient.IsCode(err, "NOT_FOUND") || mcpclient.IsCode(err, "PERMISSION_DENIED") {
		return Doc{}, ErrDocNotFound
	}
	if err != nil {
		return Doc{}, err
	}
	d := out.Doc
	return Doc{Slug: d.Slug, Title: d.Title, Label: d.Label, SourceLevel: d.SourceLevel, Dated: d.Dated, Summary: d.Summary}, nil
}

func feedbackActions(r *api.AgentReply) {
	up, down := api.ReplyActionValue("up"), api.ReplyActionValue("down")
	r.Actions = append(r.Actions, api.ReplyAction{Type: api.Feedback, Label: "Помогло", Value: &up},
		api.ReplyAction{Type: api.Feedback, Label: "Не то", Value: &down})
}

func (g *Agent) thresholds() (high, low float64) {
	high, low = g.RHigh, g.RLow
	if high == 0 {
		high = 0.65
	}
	if low == 0 {
		low = 0.3
	}
	return
}

// kbQuestion chooses the answer by 11 §2.6.4 (09 §6.5): the first matching
// rule wins.
func (g *Agent) kbQuestion(ctx context.Context, a actor.Actor, in Input, rec recognition) *api.AgentReply {
	if g.Tools == nil {
		return g.notFound(ctx, a, in, rec, nil)
	}
	search := map[string]any{"actor": a, "query": rec.Query, "limit": 3}
	var period, stepTitle string
	if a.Role == actor.Intern {
		if l, err := g.Path.Load(ctx, a, ""); err == nil {
			search["context"] = map[string]any{"track": l.Track.ID, "period": l.CurrentPeriod}
			period, stepTitle = g.contextOf(ctx, a, l)
		}
	}
	var res SearchResult
	if err := g.Tools.Call(ctx, "knowledge", "search", search, &res); err != nil { // K-A10
		r := reply.New(in.ClientMessageID, rules.KBQuestion)
		r.MascotState = api.MascotStateOops
		r.Text = "Не могу заглянуть в базу знаний, попробуй ещё раз."
		r.Actions = append(r.Actions, api.ReplyAction{Type: api.Retry, Label: "Повторить"})
		return r
	}
	high, low := g.thresholds()
	var topHit *Hit
	if len(res.Items) > 0 {
		topHit = &res.Items[0]
	}
	r := reply.New(in.ClientMessageID, rules.KBQuestion)
	switch {
	case len(res.Denied) > 0 && (topHit == nil || topHit.Rank < high): // 1
		d := res.Denied[0]
		r.MascotState = api.MascotStateAskMentor
		r.Text = "«" + d.Title + "» доступна только наставникам, её содержимое я не показываю. Если нужно что-то оттуда — спроси наставника, помогу сформулировать вопрос."
		g.publish(ctx, "permission_denied", map[string]any{"tool": "knowledge.search", "object": d.Slug, "role": a.Role})
		g.escalate(ctx, a, r, "permission_denied", rec.Query, period, stepTitle, []string{d.Title})
		return r
	case topHit != nil && topHit.SourceLevel == "none" && topHit.Rank >= low: // 2
		n := reply.Source(r, topHit.Slug, topHit.Title, topHit.Label, topHit.SourceLevel, topHit.Dated)
		r.MascotState = api.MascotStateAskMentor
		r.Text = fmt.Sprintf("В открытых источниках про «%s» ничего нет, поэтому вот тестовое описание [%d]: %s Уточни у наставника — помогу сформулировать вопрос.",
			termOf(topHit.Title), n, topHit.Summary)
		g.answered(ctx, []string{topHit.Slug}, band(topHit.Rank, high), topHit)
		g.escalate(ctx, a, r, "no_source_term", rec.Query, period, stepTitle, []string{topHit.Title})
		return r
	case topHit != nil && topHit.Rank >= low: // 3, 4
		n := reply.Source(r, topHit.Slug, topHit.Title, topHit.Label, topHit.SourceLevel, topHit.Dated)
		slugs := []string{topHit.Slug}
		shown := []shownHit{{topHit, n}}
		if topHit.Rank >= high {
			r.Text = fmt.Sprintf("%s [%d]", topHit.Summary, n)
			if len(res.Items) > 1 && res.Items[1].Rank >= high && res.Items[1].SourceLevel != "none" && !contains(topHit.ConflictsWith, res.Items[1].Slug) {
				x := res.Items[1]
				m := reply.Source(r, x.Slug, x.Title, x.Label, x.SourceLevel, x.Dated)
				slugs = append(slugs, x.Slug)
				shown = append(shown, shownHit{&res.Items[1], m})
			}
			if len(res.Denied) > 0 {
				r.Text += " Есть ещё документ «" + res.Denied[0].Title + "», но он доступен только наставникам."
				reply.Notice(r, "restricted_doc_exists")
			}
		} else {
			r.Text = fmt.Sprintf("Похоже, ответ есть здесь [%d]: %s", n, topHit.Summary)
		}
		for _, sh := range shown {
			if conflict := g.conflict(ctx, a, r, sh, rec.Query, period); conflict != "" {
				slugs = append(slugs, conflict)
				break
			}
		}
		feedbackActions(r)
		g.answered(ctx, slugs, band(topHit.Rank, high), topHit)
		return r
	}
	return g.notFound(ctx, a, in, rec, &stepContext{period, stepTitle})
}

type stepContext struct{ period, step string }

// shownHit is a source already in the reply with its marker.
type shownHit struct {
	hit *Hit
	n   int
}

// notFound: rules 5–6 of 11 §2.6.4.
func (g *Agent) notFound(ctx context.Context, a actor.Actor, in Input, rec recognition, sc *stepContext) *api.AgentReply {
	if rec.ContentTokens < 3 {
		return g.unknown(ctx, a, in, nil)
	}
	r := reply.New(in.ClientMessageID, rules.KBQuestion)
	r.MascotState = api.MascotStateAskMentor
	r.Text = "В базе знаний ответа не нашёл, а придумывать не буду. Давай спросим наставника — вот черновик."
	var period, step string
	if sc != nil {
		period, step = sc.period, sc.step
	} else {
		period, step = g.contextOf(ctx, a, nil)
	}
	g.escalate(ctx, a, r, "no_answer", rec.Query, period, step, nil)
	return r
}

// conflict adds the conflicting instruction as the second source (KQ-08).
func (g *Agent) conflict(ctx context.Context, a actor.Actor, r *api.AgentReply, sh shownHit, query, period string) string {
	top := sh.hit
	for _, slug := range top.ConflictsWith {
		d, err := g.Docs.Doc(ctx, a, slug)
		if err != nil {
			continue
		}
		n := 0
		for _, src := range r.Sources {
			if src.Slug == d.Slug {
				n = src.N
			}
		}
		if n == 0 {
			n = reply.Source(r, d.Slug, d.Title, d.Label, d.SourceLevel, d.Dated)
		}
		r.Text += fmt.Sprintf(" Инструкции расходятся: «%s» [%d] и «%s» [%s]. Уточни у наставника, какая действует у вас.", top.Title, sh.n, d.Title, strconv.Itoa(n))
		r.MascotState = api.MascotStateAskMentor
		reply.Notice(r, "doc_conflict")
		draft := "Привет!"
		if period != "" {
			draft += " Я на этапе «" + period + "»."
		}
		draft += "\nВопрос: " + query + "\nИнструкции расходятся: «" + top.Title + "» и «" + d.Title + "». Какая действует у нас?"
		reply.Copy(r, "Сообщить о расхождении", draft, "doc_conflict_reported")
		return d.Slug
	}
	return ""
}

func band(rank, high float64) string {
	if rank >= high {
		return "high"
	}
	return "medium"
}

func (g *Agent) answered(ctx context.Context, slugs []string, confidence string, top *Hit) {
	g.publish(ctx, "question_answered", map[string]any{"doc_slugs": slugs, "confidence_band": confidence,
		"label": top.Label, "top_rank": top.Rank})
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
