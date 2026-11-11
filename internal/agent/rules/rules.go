// Package rules recognises intents in demo mode (09 §6): normalisation,
// entity extraction, Snowball stemming and a weighted dictionary that lives
// in data (intents.ru.yaml), not in code. Rules always run: in demo they give
// the intent, with a model they give entities and guard topics (09 §3.3).
package rules

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/kljensen/snowball/russian"

	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

//go:embed intents.ru.yaml
var dictYAML []byte

// Intents of 09 §3.1.
const (
	Help          = "help"
	MyPath        = "my_path"
	StepExplain   = "step_explain"
	StepDone      = "step_done"
	KBQuestion    = "kb_question"
	TimeOff       = "time_off"
	AccessRequest = "access_request"
	MyRequests    = "my_requests"
	AskMentor     = "ask_mentor"
	OutOfScope    = "out_of_scope"
	Unknown       = "unknown"
)

// All lists the intents of the IntentResult enum.
var All = []string{Help, MyPath, StepExplain, StepDone, KBQuestion, TimeOff, AccessRequest,
	MyRequests, AskMentor, OutOfScope, Unknown}

// GuardTopics win over a model (09 §3.3).
var GuardTopics = map[string]bool{"salary": true, "vacation": true, "sick_leave": true,
	"personal_data": true, "wellbeing": true, "evaluation": true}

// actionIntents: a second one above the threshold becomes a quick reply (09 §6.3, p. 4).
var actionIntents = map[string]bool{TimeOff: true, AccessRequest: true}

type rawTopic struct {
	Words   []string `json:"words"`
	Phrases []string `json:"phrases"`
	Weight  float64  `json:"weight"`
}

type rawIntent struct {
	Topics             map[string]rawTopic `json:"topics"`
	Words              map[string]float64  `json:"words"`
	Phrases            map[string]float64  `json:"phrases"`
	ResourceAliasBonus float64             `json:"resource_alias_bonus"`
	AccessVerbs        []string            `json:"access_verbs"`
	Requires           []string            `json:"requires"`
	StepRefBonus       float64             `json:"step_ref_bonus"`
}

type rawDict struct {
	Version   int      `json:"version"`
	Threshold float64  `json:"threshold"`
	Priority  []string `json:"priority"`
	Negation  struct {
		Words     []string `json:"words"`
		Window    int      `json:"window"`
		NeedWords []string `json:"need_words"`
	} `json:"negation"`
	StopWords []string             `json:"stop_words"`
	Intents   map[string]rawIntent `json:"intents"`
}

type pattern struct {
	stems  []string
	weight float64
	topic  string
}

// Dict is a compiled dictionary.
type Dict struct {
	threshold    float64
	priority     map[string]int
	negWords     map[string]bool
	needWords    map[string]bool
	window       int
	stop         map[string]bool
	patterns     map[string][]pattern
	accessVerbs  [][]string
	aliasBonus   float64
	stepRefBonus float64
	requiresStep bool
}

// Load compiles the embedded dictionary.
func Load() (*Dict, error) { return Parse(dictYAML) }

// MustLoad panics on an invalid embedded dictionary (covered by tests).
func MustLoad() *Dict {
	d, err := Load()
	if err != nil {
		panic(err)
	}
	return d
}

// Parse compiles a dictionary from YAML.
func Parse(b []byte) (*Dict, error) {
	var r rawDict
	if _, err := yamlx.Decode(b, &r); err != nil {
		return nil, fmt.Errorf("intents dictionary: %w", err)
	}
	if r.Version != 1 || r.Threshold <= 0 {
		return nil, fmt.Errorf("intents dictionary: version must be 1 and threshold > 0")
	}
	d := &Dict{threshold: r.Threshold, priority: map[string]int{}, negWords: set(r.Negation.Words),
		needWords: stemSet(r.Negation.NeedWords), window: r.Negation.Window, stop: set(r.StopWords),
		patterns: map[string][]pattern{}}
	for i, p := range r.Priority {
		d.priority[p] = i
	}
	known := map[string]bool{}
	for _, i := range All {
		known[i] = true
	}
	for name, ri := range r.Intents {
		if !known[name] || name == KBQuestion || name == Unknown {
			return nil, fmt.Errorf("intents dictionary: unknown intent %q", name)
		}
		if _, ok := d.priority[name]; !ok {
			return nil, fmt.Errorf("intents dictionary: intent %q is missing in priority", name)
		}
		var ps []pattern
		for topic, t := range ri.Topics {
			for _, w := range append(append([]string{}, t.Words...), t.Phrases...) {
				ps = append(ps, pattern{stems: StemPhrase(w), weight: t.Weight, topic: topic})
			}
		}
		for w, wt := range ri.Words {
			ps = append(ps, pattern{stems: StemPhrase(w), weight: wt})
		}
		for w, wt := range ri.Phrases {
			ps = append(ps, pattern{stems: StemPhrase(w), weight: wt})
		}
		sort.Slice(ps, func(a, b int) bool { return strings.Join(ps[a].stems, " ") < strings.Join(ps[b].stems, " ") })
		d.patterns[name] = ps
		if name == AccessRequest {
			d.aliasBonus = ri.ResourceAliasBonus
			for _, v := range ri.AccessVerbs {
				d.accessVerbs = append(d.accessVerbs, StemPhrase(v))
			}
		}
		if name == StepExplain {
			d.stepRefBonus = ri.StepRefBonus
		}
		if name == StepDone {
			for _, req := range ri.Requires {
				if req == "step_ref_or_step_title" {
					d.requiresStep = true
				}
			}
		}
	}
	return d, nil
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[normalize(x)] = true
	}
	return m
}

func stemSet(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[stem(normalize(x))] = true
	}
	return m
}

// normalize: lower case and ё → е (09 §6.1, p. 1).
func normalize(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func stem(tok string) string {
	if hasCyrillic(tok) {
		return russian.Stem(tok, false)
	}
	return tok
}

// Tokens splits normalised text by everything that is not a letter or digit.
func Tokens(s string) []string {
	return strings.FieldsFunc(normalize(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// StemPhrase returns the stems of a phrase.
func StemPhrase(s string) []string {
	toks := Tokens(s)
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = stem(t)
	}
	return out
}

var (
	reStepRef = regexp.MustCompile(`(?:^|[^\d.])(\d)\.(\d{1,2})(?:$|[^\d.]|\.(?:$|[^\d]))`)
	reStepID  = regexp.MustCompile(`(?:^|[^a-z0-9-])((?:cm|be|fe)-[a-z0-9]+(?:[.-][a-z0-9]+)*)`)
)

// StepRef extracts a step id (be-1.12, fe-w1-40) or a checklist number (1.13).
func StepRef(text string) string {
	n := normalize(text)
	if m := reStepID.FindStringSubmatch(n); m != nil {
		return strings.TrimRight(m[1], ".-")
	}
	if m := reStepRef.FindStringSubmatch(n); m != nil {
		return m[1] + "." + m[2]
	}
	return ""
}

// Alias maps a resource alias to the resource id (11 §3.5.1).
type Alias struct {
	Phrase     string
	ResourceID string
}

// StepTitle is a visible step of the user (to accept a step by its title).
type StepTitle struct {
	ID    string
	Title string
}

// Hints is the context the recogniser may use.
type Hints struct {
	Aliases []Alias
	Steps   []StepTitle
}

// Result is the outcome of recognition (09 §3.2 plus diagnostics).
type Result struct {
	Intent        string
	Topic         string
	Confidence    float64
	Score         float64
	StepRef       string // as written: "1.13" or a step id
	StepID        string // a step matched by its title
	ResourceID    string // a catalog alias was found
	AccessVerb    bool
	Second        string   // the second action intent above the threshold (quick reply)
	Ambiguous     []string // two intents within 0.5 of each other: ask (09 §6.4)
	ContentTokens int
	Scores        map[string]float64
}

type input struct {
	raw   []string
	stems []string
}

func (d *Dict) prepare(text string) input {
	toks := Tokens(text)
	in := input{raw: toks, stems: make([]string, len(toks))}
	for i, t := range toks {
		in.stems[i] = stem(t)
	}
	return in
}

// contentIdx returns indexes of content tokens (not stop words, not "скрыто").
func (d *Dict) contentIdx(in input) []int {
	var out []int
	for i, t := range in.raw {
		if d.stop[t] || t == "скрыто" {
			continue
		}
		out = append(out, i)
	}
	return out
}

// find returns start positions of a stem sequence.
func find(in input, p []string) []int {
	var out []int
	if len(p) == 0 {
		return out
	}
	for i := 0; i+len(p) <= len(in.stems); i++ {
		ok := true
		for j := range p {
			if in.stems[i+j] != p[j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// negated: a negation word within window tokens before the match, or within
// window+1 tokens after it when followed by a "need" word ("отгул мне пока не
// нужен"). Distances are counted in raw tokens.
func (d *Dict) negated(in input, start, length int, patternHasNeg bool) bool {
	if patternHasNeg {
		return false
	}
	for i := start - 1; i >= 0 && i >= start-d.window; i-- {
		if d.negWords[in.raw[i]] {
			return true
		}
	}
	end := start + length
	for i := end; i < len(in.raw) && i <= end+d.window; i++ {
		if d.negWords[in.raw[i]] && i+1 < len(in.stems) && d.needWords[in.stems[i+1]] {
			return true
		}
	}
	return false
}

func (d *Dict) hasNeg(p []string) bool {
	for _, s := range p {
		if d.negWords[s] {
			return true
		}
	}
	return false
}

// matched reports a non-negated occurrence of the stem sequence.
func (d *Dict) matched(in input, p []string) bool {
	for _, st := range find(in, p) {
		if !d.negated(in, st, len(p), d.hasNeg(p)) {
			return true
		}
	}
	return false
}

// Recognize runs the algorithm of 09 §6.3 on (already masked) text.
func (d *Dict) Recognize(text string, h Hints) Result {
	in := d.prepare(text)
	content := d.contentIdx(in)
	res := Result{Scores: map[string]float64{}, ContentTokens: len(content), StepRef: StepRef(text)}

	for _, a := range h.Aliases {
		if d.matched(in, StemPhrase(a.Phrase)) {
			res.ResourceID = a.ResourceID
			break
		}
	}
	for _, v := range d.accessVerbs {
		if d.matched(in, v) {
			res.AccessVerb = true
			break
		}
	}
	if res.StepRef == "" {
		res.StepID = d.stepByTitle(in, h.Steps)
	}

	topicScore := map[string]float64{}
	for name, ps := range d.patterns {
		for _, p := range ps {
			if !d.matched(in, p.stems) {
				continue
			}
			if p.topic != "" {
				topicScore[p.topic] += p.weight
				continue
			}
			res.Scores[name] += p.weight
		}
	}
	if res.ResourceID != "" && res.AccessVerb {
		res.Scores[AccessRequest] += d.aliasBonus
	}
	if res.StepRef != "" {
		res.Scores[StepExplain] += d.stepRefBonus
	}
	if d.requiresStep && res.Scores[StepDone] > 0 && res.StepRef == "" && res.StepID == "" {
		res.Scores[MyPath] += res.Scores[StepDone] // downgraded to my_path (09 §6.2)
		delete(res.Scores, StepDone)
	}

	// p. 2: guard topics first.
	if topic, sc := best(topicScore); sc >= d.threshold {
		res.Intent, res.Topic, res.Score = OutOfScope, topic, sc
		res.Scores[OutOfScope] = sc
		res.Confidence = confidence(sc)
		return res
	}

	type cand struct {
		name  string
		score float64
	}
	var cs []cand
	for n, s := range res.Scores {
		if s >= d.threshold {
			cs = append(cs, cand{n, s})
		}
	}
	sort.Slice(cs, func(a, b int) bool {
		if cs[a].score != cs[b].score {
			return cs[a].score > cs[b].score
		}
		return d.priority[cs[a].name] < d.priority[cs[b].name]
	})
	if len(cs) == 0 {
		if res.ContentTokens >= 1 {
			res.Intent, res.Confidence = KBQuestion, 0.5
		} else {
			res.Intent = Unknown
		}
		return res
	}
	// p. 4: two actions — the one first by priority runs, the other becomes a quick reply.
	if len(cs) >= 2 && actionIntents[cs[0].name] && actionIntents[cs[1].name] {
		first, second := cs[0], cs[1]
		if d.priority[second.name] < d.priority[first.name] {
			first, second = second, first
		}
		res.Intent, res.Score, res.Second = first.name, first.score, second.name
		res.Confidence = confidence(first.score)
		return res
	}
	res.Intent, res.Score = cs[0].name, cs[0].score
	res.Confidence = confidence(cs[0].score)
	for _, c := range cs[1:] {
		if cs[0].score-c.score <= 0.5 {
			if len(res.Ambiguous) == 0 {
				res.Ambiguous = []string{cs[0].name}
			}
			if len(res.Ambiguous) < 3 {
				res.Ambiguous = append(res.Ambiguous, c.name)
			}
		}
	}
	return res
}

func best(m map[string]float64) (string, float64) {
	var name string
	var sc float64
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if m[k] > sc {
			name, sc = k, m[k]
		}
	}
	return name, sc
}

// confidence for journals (09 §6.3, p. 6).
func confidence(score float64) float64 {
	if score >= 2 {
		return 0.9
	}
	return 0.7
}

// stepByTitle accepts a step when ≥ 2 content stems of its title are present
// and they make up at least two thirds of the title's content stems.
func (d *Dict) stepByTitle(in input, steps []StepTitle) string {
	have := map[string]bool{}
	for _, s := range in.stems {
		have[s] = true
	}
	bestID, bestHits := "", 0
	for _, st := range steps {
		var stems []string
		for _, t := range Tokens(st.Title) {
			if d.stop[t] {
				continue
			}
			stems = append(stems, stem(t))
		}
		hits := 0
		for _, s := range stems {
			if have[s] {
				hits++
			}
		}
		if hits >= 2 && hits*3 >= len(stems)*2 && hits > bestHits {
			bestID, bestHits = st.ID, hits
		}
	}
	return bestID
}

// IsConfirmWord reports a textual "yes" that must not confirm a draft (09 §8.1).
func IsConfirmWord(text string) bool {
	t := strings.Join(Tokens(text), " ")
	switch t {
	case "да", "ок", "ok", "окей", "подтверждаю", "подтверди", "давай", "да подтверждаю", "отправляй", "отправь":
		return true
	}
	return false
}
