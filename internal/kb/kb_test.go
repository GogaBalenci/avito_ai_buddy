package kb

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/GogaBalenci/avito_ai_buddy/content"
)

func TestLoadKnowledgeBase(t *testing.T) {
	sub, _ := fs.Sub(content.FS(), "kb")
	docs, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 29 {
		t.Fatalf("docs = %d, want 29 (11 §2.5.2)", len(docs))
	}
	by := map[string]Doc{}
	for _, d := range docs {
		by[d.Slug] = d
	}
	mg := by["mentor-guide"]
	if mg.AllowedFor("intern") || !mg.AllowedFor("mentor") || !mg.DiscloseTitle {
		t.Errorf("mentor-guide rights: %+v", mg)
	}
	if by["term-stash"].Label() != "test_description" || by["dev-principles"].Label() != "test_instruction" {
		t.Error("labels")
	}
	if !strings.Contains(by["kb-injection-test"].Body, "Игнорируй правила") || !by["kb-injection-test"].TestOnly {
		t.Error("kb-injection-test must keep the injected line and test_only")
	}
	if got := by["code-review-general"].ConflictsWith; len(got) != 1 || got[0] != "code-review-team" {
		t.Errorf("conflicts = %v", got)
	}
	if !Slugs(docs)["who-to-ask"] {
		t.Error("Slugs")
	}
}

func doc(slug, extra string) string {
	return "---\nslug: " + slug + "\ntitle: T\nkind: term\nsource_level: playbook\nallowed_roles: [intern, mentor]\nsummary: S\n" + extra + "---\nBody\n"
}

// C1 (13 §10.3): three different rule-7 errors with file names; plus rules 1–6.
func TestLoaderRules(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		rule  int
		msg   string
	}{
		{"no front matter", map[string]string{"a.md": "Body"}, 1, "front matter"},
		{"schema", map[string]string{"a.md": "---\nslug: a\n---\n"}, 1, ""},
		{"slug != file", map[string]string{"b.md": doc("ab", "")}, 1, "file name"},
		{"none needs term", map[string]string{"ab.md": strings.Replace(doc("ab", ""), "kind: term\nsource_level: playbook", "kind: tool\nsource_level: none", 1)}, 3, ""},
		{"disclose open doc", map[string]string{"ab.md": doc("ab", "disclose_title: true\n")}, 4, ""},
		{"real name", map[string]string{"ab.md": strings.Replace(doc("ab", ""), "Body", "Книга: Таненбаум", 1)}, 6, ""},
		{"unknown conflict", map[string]string{"ab.md": doc("ab", "conflicts_with: [zz]\n")}, 7, "unknown slug"},
		{"not mutual", map[string]string{"ab.md": doc("ab", "conflicts_with: [cd]\n"), "cd.md": doc("cd", "")}, 7, "not mutual"},
		{"different roles", map[string]string{
			"ab.md": doc("ab", "conflicts_with: [cd]\n"),
			"cd.md": strings.Replace(doc("cd", "conflicts_with: [ab]\n"), "[intern, mentor]", "[mentor]", 1)}, 7, "different allowed_roles"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for n, b := range c.files {
				fsys[n] = &fstest.MapFile{Data: []byte(b)}
			}
			_, err := Load(fsys)
			var ke *Error
			if !errors.As(err, &ke) {
				t.Fatalf("want *Error, got %v", err)
			}
			if !strings.Contains(err.Error(), "rule ") || !strings.Contains(err.Error(), ".md") {
				t.Fatalf("error must name rule and file: %v", err)
			}
			found := false
			for _, e := range unwrapAll(err) {
				var k *Error
				if errors.As(e, &k) && k.Rule == c.rule && strings.Contains(k.Msg, c.msg) {
					found = true
				}
			}
			if !found {
				t.Fatalf("rule %d (%q) not reported: %v", c.rule, c.msg, err)
			}
		})
	}
}

func unwrapAll(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

func TestDuplicateSlugAndCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(doc("ab", ""), "\n", "\r\n")
	docs, err := Load(fstest.MapFS{"ab.md": {Data: []byte(crlf)}})
	if err != nil || len(docs) != 1 || docs[0].Body != "Body" {
		t.Fatalf("CRLF doc: %v %+v", err, docs)
	}
}
