// Package pii masks personal data in user messages before they are stored,
// logged or sent to a model (09 §11.2, AG-12). Step numbers (1.13), request
// numbers (ABS-000123, ACC-000101) and dates are left intact.
package pii

import (
	"regexp"
	"strings"
)

// Placeholder replaces every masked fragment.
const Placeholder = "[скрыто]"

// Patterns are applied in order: the more specific ones first, so that a
// card number is not partly eaten by the phone pattern.
var patterns = []*regexp.Regexp{
	// e-mail
	regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`),
	// SNILS: 123-456-789 01
	regexp.MustCompile(`\b\d{3}-\d{3}-\d{3}[\s-]?\d{2}\b`),
	// bank card: 13–19 digits in a row or in groups of 4
	regexp.MustCompile(`\b(?:\d{4}[\s-]){3}\d{1,7}\b|\b\d{13,19}\b`),
	// phone: +7 / 8 and 10 digits with optional separators
	regexp.MustCompile(`(?:\+7|\b8)[\s(\-]*\d{3}[\s)\-]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b`),
	// RF passport: 12 34 567890
	regexp.MustCompile(`\b\d{2}\s?\d{2}\s?\d{6}\b`),
}

// Mask returns the text with personal data replaced and whether anything
// was replaced.
func Mask(text string) (string, bool) {
	out := text
	for _, re := range patterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			if protected(m) {
				return m
			}
			return Placeholder
		})
	}
	return out, out != text
}

// protected guards identifiers that look numeric but are not personal data.
func protected(m string) bool {
	return strings.HasPrefix(m, "ABS-") || strings.HasPrefix(m, "ACC-")
}
