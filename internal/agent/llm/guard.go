package llm

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ReplyGuard rules (09 §7.3): the rephrased text is accepted only if all hold.
const (
	RuleLength    = 1
	RuleMustKeep  = 2
	RuleMarkers   = 3
	RuleNumbers   = 4
	RuleMarkup    = 5
	RuleForbidden = 6
	RuleCreated   = 7
)

var (
	reMarker = regexp.MustCompile(`\[(\d+)\]`)
	reNumber = regexp.MustCompile(`\d+`)
	reWord   = regexp.MustCompile(`[\p{L}]+`)
	reURL    = regexp.MustCompile(`(?i)https?://|www\.`)
)

// forbidden phrases as sequences of word prefixes (case-insensitive): the
// prefix covers every form («официальн…» — официальный, официально…).
var forbidden = [][]string{
	{"регламент", "авито"}, {"официальн"}, {"по", "правилам", "компании"}, {"обязан"}, {"гарантир"},
}

// words returns lower-case words.
func words(s string) []string {
	return reWord.FindAllString(strings.ToLower(strings.ReplaceAll(s, "ё", "е")), -1)
}

func hasSeq(ws, seq []string) bool {
	for i := 0; i+len(seq) <= len(ws); i++ {
		ok := true
		for j := range seq {
			if !strings.HasPrefix(ws[i+j], seq[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Check runs ReplyGuard; it returns 0 when the text is accepted, or the
// number of the first broken rule.
func Check(text string, in RephraseInput) int {
	max := in.MaxChars
	if max <= 0 {
		max = 400
	}
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > max { // 1
		return RuleLength
	}
	for _, k := range in.MustKeep { // 2
		if !strings.Contains(text, k) {
			return RuleMustKeep
		}
	}
	allowed := map[string]bool{}
	for _, s := range in.Sources {
		allowed[strconv.Itoa(s.N)] = true
	}
	markers := reMarker.FindAllStringSubmatch(text, -1)
	for _, m := range markers { // 3
		if !allowed[m[1]] {
			return RuleMarkers
		}
	}
	if len(in.Sources) > 0 && len(markers) == 0 {
		return RuleMarkers
	}
	known := in.DraftText + " " + strings.Join(in.Facts, " ")
	for _, n := range reNumber.FindAllString(reMarker.ReplaceAllString(text, ""), -1) { // 4
		if !strings.Contains(known, n) {
			return RuleNumbers
		}
	}
	clean := reMarker.ReplaceAllString(text, "")
	if reURL.MatchString(clean) || strings.ContainsAny(clean, "<>") || strings.Contains(clean, "\n#") || strings.HasPrefix(clean, "#") { // 5
		return RuleMarkup
	}
	ws := words(text)
	for _, f := range forbidden { // 6
		if hasSeq(ws, f) {
			return RuleForbidden
		}
	}
	if in.ActionState != "created" { // 7
		near := false
		for i, w := range ws {
			if !strings.HasPrefix(w, "заявк") {
				continue
			}
			for j := i - 3; j <= i+3; j++ {
				if j < 0 || j >= len(ws) || j == i {
					continue
				}
				for _, bad := range []string{"создан", "оформл", "отправл"} {
					if strings.HasPrefix(ws[j], bad) {
						near = true
					}
				}
			}
		}
		if near {
			return RuleCreated
		}
	}
	return 0
}
