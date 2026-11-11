// Package content embeds the synthetic data of the corporate service mocks
// (12 §8.6): adaptation tracks, the knowledge base, the access catalog and the
// test accounts. Loaders take an fs.FS, so tests can use their own folders.
package content

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed tracks kb access seeds
var files embed.FS

// FS returns the embedded content.
func FS() fs.FS { return files }

// bannedNames are surnames of real people found in the case CSV (book authors,
// speakers, a director). Track rule 7 and knowledge base rule 6 forbid them in
// content: the data stays synthetic and free of personal data.
var bannedNames = []string{
	"ходаков", "гандапас", "донован", "керниган", "ричардсон", "бхаргава",
	"клеппман", "уэзеролл", "таненбаум", "кормен", "лейзерсон", "макконнелл",
}

// FindBannedName returns the first real person's name found in s, or "".
func FindBannedName(s string) string {
	l := strings.ToLower(s)
	for _, n := range bannedNames {
		if strings.Contains(l, n) {
			return n
		}
	}
	return ""
}
