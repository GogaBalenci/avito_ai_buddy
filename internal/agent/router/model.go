package router

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/llm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/reply"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/rules"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
)

// modelState is what step 1 decided for the reply: mode and notices.
type modelState struct {
	mode      api.AgentReplyMode
	notices   []string
	useModel  bool // the model answered step 1: step 2 may run
	rephrased bool
}

func (st modelState) apply(r *api.AgentReply) *api.AgentReply {
	if st.mode != "" {
		r.Mode = st.mode
	}
	for _, n := range st.notices {
		reply.Notice(r, n)
	}
	return r
}

func (g *Agent) fallback(ctx context.Context, st *modelState, reason string) {
	st.mode = api.AgentReplyModeFallback
	if reason == llm.ReasonAuth || g.LLM.AuthBlocked() {
		st.notices = append(st.notices, "llm_auth_error")
	} else {
		st.notices = append(st.notices, "simplified_mode")
	}
	g.llmEvent(ctx, "llm_fallback", map[string]any{"reason": reason}, llm.IntentVersion)
}

func (g *Agent) llmEvent(ctx context.Context, name string, payload map[string]any, prompt string) {
	if g.Events != nil {
		_ = g.Events.Publish(ctx, events.Event{Name: name, Mode: "llm", PromptVersion: prompt, Payload: payload})
	}
}

var weekdays = []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

// intentContext fills the prompt context (09 §4.2): role, track, period and
// catalogs; no names or e-mails.
func (g *Agent) intentContext(ctx context.Context, a actor.Actor, in Input, l *path.Loaded) llm.IntentContext {
	now := g.Clock.Now()
	ic := llm.IntentContext{Today: clock.ISO(now), Weekday: weekdays[now.Weekday()], Role: a.Role, MaskedText: in.Text}
	if l != nil {
		ic.Track, ic.PeriodTitle = l.Track.ID, l.PeriodTitle(l.CurrentPeriod)
		open := map[string]bool{}
		for _, p := range l.Periods {
			open[p.ID] = p.State != "future"
		}
		for _, s := range l.Steps {
			if open[s.Period] && s.Status != "done" && len(ic.Steps) < 30 {
				ref := s.ChecklistRef
				if ref == "" {
					ref = s.ID
				}
				ic.Steps = append(ic.Steps, llm.Step{Ref: ref, Title: s.Title})
			}
		}
	}
	if g.Catalog != nil {
		if rs, err := g.Catalog.List(ctx, a); err == nil {
			for _, r := range rs {
				if r.Allowed {
					ic.Resources = append(ic.Resources, llm.Resource{ID: r.ResourceID, Title: r.Title})
				}
			}
		}
	}
	if b, err := g.Redis.Get(ctx, lbqKey(a.UserID)).Bytes(); err == nil {
		var opts []string
		if json.Unmarshal(b, &opts) == nil {
			var labels []string
			for _, o := range opts {
				labels = append(labels, intentLabels[o])
			}
			ic.LastBotQuestion = strings.Join(labels, " / ")
		}
	}
	return ic
}

// modelIntent runs step 1 and merges it into rec by 09 §3.3.
func (g *Agent) modelIntent(ctx context.Context, a actor.Actor, in Input, h rules.Hints, l *path.Loaded, rec *recognition) modelState {
	st := modelState{}
	if !g.LLM.Requested() {
		return st
	}
	if mis := g.LLM.Misconfigured(); mis != "" { // AG-24
		st.mode = api.AgentReplyModeFallback
		st.notices = append(st.notices, "llm_misconfigured")
		return st
	}
	if !g.LLM.Enabled(ctx) { // the kill switch llm_enabled = off: like demo
		return st
	}
	ir, err := g.LLM.Intent(ctx, g.intentContext(ctx, a, in, l))
	if err != nil {
		g.fallback(ctx, &st, llm.ReasonOf(err))
		return st
	}
	st.mode, st.useModel = api.AgentReplyModeLlm, true
	g.merge(ctx, ir, h, l, rec)
	return st
}

// merge applies the Go checks of 09 §3.3 to the model result.
func (g *Agent) merge(ctx context.Context, ir llm.IntentResult, h rules.Hints, l *path.Loaded, rec *recognition) {
	if ir.Confidence < 0.6 {
		g.llmEvent(ctx, "llm_fallback", map[string]any{"reason": "low_confidence"}, llm.IntentVersion)
		return // the rules stay
	}
	if rec.Intent == rules.OutOfScope && rules.GuardTopics[rec.Topic] && ir.Intent != rules.OutOfScope {
		rec.Override = "llm" // the guard topic of the rules wins over the model
		return
	}
	if rules.SmallTalkKind(rec.Topic) != "" && (ir.Intent == rules.Help || ir.Intent == rules.KBQuestion || ir.Intent == rules.Unknown) {
		rec.Override = "llm" // chit-chat of the rules: the model does not answer it (09 §17.3)
		return
	}
	merged := recognition{Result: rules.Result{Intent: ir.Intent, Confidence: ir.Confidence, ContentTokens: rec.ContentTokens,
		Second: rec.Second}, Source: "llm", Query: rec.Query}
	if ir.Intent == rules.OutOfScope {
		merged.Topic = ir.Params.Topic
		if merged.Topic == "" {
			merged.Topic = "other"
		}
	}
	// step_ref: kept only if it is a visible step; the rules' regex is more reliable
	merged.StepRef, merged.StepID = rec.StepRef, rec.StepID
	if ir.Params.StepRef != "" && l != nil {
		if s, ok := l.StepByRef(ir.Params.StepRef); ok {
			merged.StepRef = s.ID
		}
	}
	// resource_hint → a catalog alias; otherwise dropped (choice from the list)
	merged.ResourceID = rec.ResourceID
	if ir.Params.ResourceHint != "" {
		if id := matchResource(ir.Params.ResourceHint, h.Aliases); id != "" {
			merged.ResourceID = id
		}
	}
	if ir.Intent == rules.KBQuestion || ir.Intent == rules.AskMentor {
		if q := strings.TrimSpace(ir.Params.Query); q != "" {
			merged.Query = q
		}
	}
	if ir.Intent == rules.TimeOff && reDate.MatchString(ir.Params.DateFrom) {
		to := ir.Params.DateTo
		if !reDate.MatchString(to) {
			to = ir.Params.DateFrom
		}
		if _, e1 := clock.ParseISO(ir.Params.DateFrom); e1 == nil {
			if _, e2 := clock.ParseISO(to); e2 == nil {
				merged.DateFrom, merged.DateTo = ir.Params.DateFrom, to
			}
		}
	}
	*rec = merged
}

var reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// matchResource maps the user's words to a resource by aliases.
func matchResource(hint string, aliases []rules.Alias) string {
	hs := strings.Join(rules.StemPhrase(hint), " ")
	for _, a := range aliases {
		as := strings.Join(rules.StemPhrase(a.Phrase), " ")
		if as == hs || strings.Contains(" "+hs+" ", " "+as+" ") || a.ResourceID == strings.TrimSpace(strings.ToLower(hint)) {
			return a.ResourceID
		}
	}
	return ""
}

var (
	reRequestID = regexp.MustCompile(`(?:ABS|ACC)-\d{6}`)
	reDDMM      = regexp.MustCompile(`\d{2}\.\d{2}`)
	reMarkerN   = regexp.MustCompile(`\[\d+\]`)
)

// rephrasable: step 2 never runs for refusals, out of scope, errors, «не
// нашёл», clarifications, draft cards and conflicts (09 §7).
func rephrasable(r *api.AgentReply) bool {
	if r.Escalation != nil || r.PendingAction != nil || r.MascotState == api.MascotStateOops ||
		r.MascotState == api.MascotStateAskMentor {
		return false
	}
	switch string(r.Intent) {
	case rules.OutOfScope, rules.Unknown, rules.AccessRequest, rules.AskMentor:
		return false
	}
	for _, n := range r.Notices {
		if n == "doc_conflict" {
			return false
		}
	}
	return true
}

// rephrase runs step 2 and ReplyGuard (09 §7.3).
func (g *Agent) rephrase(ctx context.Context, in Input, r *api.AgentReply, st *modelState) {
	if !st.useModel || !g.LLM.RephraseOn(ctx) || !rephrasable(r) {
		return
	}
	ri := llm.RephraseInput{Intent: string(r.Intent), UserQuestion: in.Text, DraftText: r.Text, Facts: []string{r.Text},
		ActionState: "none", MaxChars: 400}
	for _, s := range r.Sources {
		ri.Sources = append(ri.Sources, llm.Source{N: s.N, Title: s.Title})
	}
	ri.MustKeep = append(ri.MustKeep, reMarkerN.FindAllString(r.Text, -1)...)
	ri.MustKeep = append(ri.MustKeep, reRequestID.FindAllString(r.Text, -1)...)
	ri.MustKeep = append(ri.MustKeep, reDDMM.FindAllString(r.Text, -1)...)
	text, err := g.LLM.Rephrase(ctx, ri)
	if err != nil {
		st.mode = api.AgentReplyModeFallback // the template stays (09 §13.1)
		g.llmEvent(ctx, "llm_fallback", map[string]any{"reason": llm.ReasonOf(err), "step": "rephrase"}, llm.RephraseVersion)
		return
	}
	if rule := llm.Check(text, ri); rule != 0 {
		g.llmEvent(ctx, "llm_rephrase_rejected", map[string]any{"rule": rule}, llm.RephraseVersion)
		return
	}
	r.Text, st.rephrased = text, true
}
