// Package kb loads the synthetic knowledge base (content/kb/*.md) and checks
// it with the loader rules 1–8 (11 §2.10.3). Shared by mcp-mocks and CI.
package kb

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

// Doc is a knowledge base document: front matter + markdown body.
type Doc struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	SourceLevel   string   `json:"source_level"`
	Dated         *string  `json:"dated"`
	AllowedRoles  []string `json:"allowed_roles"`
	DiscloseTitle bool     `json:"disclose_title"`
	Tracks        []string `json:"tracks"`
	Periods       []string `json:"periods"`
	Tags          []string `json:"tags"`
	ConflictsWith []string `json:"conflicts_with"`
	TestOnly      bool     `json:"test_only"`
	Summary       string   `json:"summary"`

	Body string `json:"-"`
	File string `json:"-"`
}

// Label is derived, never stored: a document without an open source is a
// "test description — ask your mentor" (11 §2.5.1).
func (d Doc) Label() string {
	if d.SourceLevel == "none" {
		return "test_description"
	}
	return "test_instruction"
}

// AllowedFor reports whether the role may read the document.
func (d Doc) AllowedFor(role string) bool {
	for _, r := range d.AllowedRoles {
		if r == role {
			return true
		}
	}
	return false
}

// Error is a loader rule violation.
type Error struct {
	File string
	Rule int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("kb/%s: rule %d: %s", e.File, e.Rule, e.Msg) }

// Load reads every *.md in fsys (the kb directory). Any violation fails the
// whole load (rule 8): the service must not start.
func Load(fsys fs.FS) ([]Doc, error) {
	names, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var docs []Doc
	var errs []error
	seen := map[string]bool{}
	for _, name := range names {
		d, err := parse(fsys, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[d.Slug] {
			errs = append(errs, &Error{File: name, Rule: 1, Msg: "duplicate slug " + d.Slug})
		}
		seen[d.Slug] = true
		errs = append(errs, checkDoc(d)...)
		docs = append(docs, d)
	}
	errs = append(errs, checkConflicts(docs)...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return docs, nil
}

func parse(fsys fs.FS, name string) (Doc, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Doc{}, err
	}
	fm, body, ok := splitFrontMatter(raw)
	if !ok {
		return Doc{}, &Error{File: name, Rule: 1, Msg: "no YAML front matter between --- lines"}
	}
	j, err := yamlx.ToJSON(fm)
	if err != nil {
		return Doc{}, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	if err := contracts.ValidateJSON("knowledge/doc-frontmatter.v1.json", j); err != nil {
		return Doc{}, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	var d Doc
	if _, err := yamlx.Decode(fm, &d); err != nil {
		return Doc{}, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	if want := strings.TrimSuffix(path.Base(name), ".md"); d.Slug != want {
		return Doc{}, &Error{File: name, Rule: 1, Msg: fmt.Sprintf("slug %q does not match file name", d.Slug)}
	}
	d.Body = strings.TrimSpace(string(body))
	d.File = name
	d.Summary = strings.TrimSpace(d.Summary)
	return d, nil
}

func splitFrontMatter(raw []byte) (fm, body []byte, ok bool) {
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, nil, false
	}
	rest := raw[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, nil, false
	}
	return rest[:end+1], rest[end+5:], true
}

func checkDoc(d Doc) []error {
	var errs []error
	if len(d.AllowedRoles) == 0 {
		errs = append(errs, &Error{File: d.File, Rule: 2, Msg: "allowed_roles is empty"})
	}
	for _, r := range d.AllowedRoles {
		if r != "intern" && r != "mentor" {
			errs = append(errs, &Error{File: d.File, Rule: 2, Msg: "unknown role " + r})
		}
	}
	if d.SourceLevel == "none" && d.Kind != "term" && d.Kind != "guide" {
		errs = append(errs, &Error{File: d.File, Rule: 3, Msg: "source_level none is allowed only for kind term or guide"})
	}
	if d.DiscloseTitle && d.AllowedFor("intern") && d.AllowedFor("mentor") {
		errs = append(errs, &Error{File: d.File, Rule: 4, Msg: "disclose_title on a document open to every role"})
	}
	if d.Summary == "" || len([]rune(d.Summary)) > 300 {
		errs = append(errs, &Error{File: d.File, Rule: 5, Msg: "summary must be 1–300 characters"})
	}
	for _, s := range []string{d.Title, d.Summary, d.Body, strings.Join(d.Tags, " ")} {
		if n := content.FindBannedName(s); n != "" {
			errs = append(errs, &Error{File: d.File, Rule: 6, Msg: "real person's name: " + n})
			break
		}
	}
	return errs
}

// checkConflicts is rule 7: conflicts_with points to existing documents, the
// link is mutual and both documents are open to exactly the same roles —
// otherwise a conflict would reveal a closed document (13 §9, row 8).
func checkConflicts(docs []Doc) []error {
	bySlug := map[string]Doc{}
	for _, d := range docs {
		bySlug[d.Slug] = d
	}
	var errs []error
	for _, d := range docs {
		for _, other := range d.ConflictsWith {
			o, ok := bySlug[other]
			switch {
			case !ok:
				errs = append(errs, &Error{File: d.File, Rule: 7, Msg: "conflicts_with: unknown slug " + other})
			case !contains(o.ConflictsWith, d.Slug):
				errs = append(errs, &Error{File: d.File, Rule: 7, Msg: "conflicts_with: link to " + other + " is not mutual"})
			case !sameRoles(d.AllowedRoles, o.AllowedRoles):
				errs = append(errs, &Error{File: d.File, Rule: 7, Msg: "conflicts_with: " + other + " has different allowed_roles"})
			}
		}
	}
	return errs
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func sameRoles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// Slugs returns the set of document slugs (for the tracks cross-check).
func Slugs(docs []Doc) map[string]bool {
	m := make(map[string]bool, len(docs))
	for _, d := range docs {
		m[d.Slug] = true
	}
	return m
}
