package tracks

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/internal/kb"
	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

func sub(t *testing.T, dir string) fs.FS {
	t.Helper()
	f, err := fs.Sub(content.FS(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// catalogIDs reads resource ids without importing the mock package (depguard).
func catalogIDs(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := fs.ReadFile(content.FS(), "access/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Resources []map[string]any `json:"resources"`
	}
	j, err := yamlx.ToJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(j, &f); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range f.Resources {
		ids[r["id"].(string)] = true
	}
	return ids
}

// L-01 + L-04: all embedded tracks pass rules 1–8 against the real KB and catalog.
func TestLoadAllTracks(t *testing.T) {
	docs, err := kb.Load(sub(t, "kb"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := Load(sub(t, "tracks"), Refs{KBDocs: kb.Slugs(docs), Resources: catalogIDs(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(set.IDs(), ","); got != "backend,common,frontend" {
		t.Fatalf("tracks = %s", got)
	}
	be, err := set.Path("backend")
	if err != nil {
		t.Fatal(err)
	}
	if len(be) != 48 {
		t.Errorf("backend path has %d steps, want 48 (06-checklist-analysis)", len(be))
	}
	week1 := 0
	for _, s := range be {
		if s.Period == "week1" && s.Owner == "intern" {
			week1++
		}
	}
	if week1 != 12 {
		t.Errorf("backend week1 intern steps = %d, want 12", week1)
	}
	fe, _ := set.Path("frontend")
	for _, s := range fe {
		if s.TrackID == "frontend" && !s.Synthetic {
			t.Errorf("frontend step %s must be synthetic", s.ID)
		}
	}
	if set.PeriodIndex("after3") != 5 || set.PeriodIndex("nope") != -1 {
		t.Error("period index")
	}
	if _, err := set.Path("common"); err == nil {
		t.Error("common is not a direction")
	}
	if _, ok := set.Track("backend"); !ok {
		t.Error("Track(backend)")
	}
}

const commonYAML = `schema_version: 1
id: common
title: Общие
kind: common
synthetic: false
source: test
periods:
  - { id: week1, title: W1, from_day: 1, to_day: 7 }
  - { id: week2, title: W2, from_day: 8, to_day: null }
steps:
  - { id: cm-1, period: week1, order: 10, type: learn, owner: intern, title: "Шаг", help: { kind: track_only } }
`

// L-02: a new direction is just a file.
func TestNewTrackWithoutCode(t *testing.T) {
	fsys := fstest.MapFS{
		"common.yaml": {Data: []byte(commonYAML)},
		"qa.yaml": {Data: []byte(`schema_version: 1
id: qa
title: QA (тестовый трек)
kind: direction
extends: common
synthetic: true
disclaimer: "Тестовый трек, не материалы Авито."
source: "Иллюстрация формата"
steps:
  - { id: qa-w1-10, period: week1, order: 10, type: learn, owner: intern, title: "Изучи QA-практики команды", help: { kind: track_only } }
`)},
	}
	set, err := Load(fsys, Refs{})
	if err != nil {
		t.Fatal(err)
	}
	path, err := set.Path("qa")
	if err != nil || len(path) != 2 || !path[1].Synthetic || path[0].Synthetic {
		t.Fatalf("path = %+v, %v", path, err)
	}
}

// L-03: negative cases name the file, the step and the rule.
func TestLoaderRules(t *testing.T) {
	dir := func(extra string) string {
		return `schema_version: 1
id: be
title: BE
kind: direction
extends: common
synthetic: false
source: test
steps:
` + extra
	}
	cases := []struct {
		name, file, body string
		rule             int
		refs             Refs
	}{
		{"schema", "be.yaml", "schema_version: 1\nid: be\n", 1, Refs{}},
		{"bad yaml", "be.yaml", "a: [1", 1, Refs{}},
		{"file name", "x.yaml", dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), 2, Refs{}},
		{"duplicate with common", "be.yaml", dir("  - { id: cm-1, period: week1, order: 20, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), 3, Refs{}},
		{"duplicate id", "be.yaml", dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n  - { id: be-1, period: week1, order: 20, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), 3, Refs{}},
		{"unknown period", "be.yaml", dir("  - { id: be-1, period: month3, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), 4, Refs{}},
		{"kb doc", "be.yaml", dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: explain, kb_doc: nope, source_level: none } }\n"), 5, Refs{KBDocs: map[string]bool{}}},
		{"resource", "be.yaml", dir("  - { id: be-1, period: week1, order: 10, type: access, owner: intern, title: T, help: { kind: access_request, resource: nope } }\n"), 5, Refs{Resources: map[string]bool{}}},
		{"real name", "be.yaml", dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: \"Книга Клеппмана\", help: { kind: track_only } }\n"), 7, Refs{}},
		{"slot", "be.yaml", dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n  - { id: be-2, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), 8, Refs{}},
		{"synthetic without disclaimer", "be.yaml", strings.Replace(dir("  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }\n"), "synthetic: false", "synthetic: true", 1), 1, Refs{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{"common.yaml": {Data: []byte(commonYAML)}, c.file: {Data: []byte(c.body)}}
			_, err := Load(fsys, c.refs)
			var le *Error
			if !errors.As(err, &le) {
				t.Fatalf("want *Error, got %v", err)
			}
			if le.Rule != c.rule {
				t.Fatalf("rule = %d, want %d (%v)", le.Rule, c.rule, err)
			}
			if !strings.Contains(err.Error(), "tracks/") {
				t.Fatalf("error must name the file: %v", err)
			}
		})
	}
}

func TestCommonRequiredAndPeriods(t *testing.T) {
	if _, err := Load(fstest.MapFS{}, Refs{}); err == nil {
		t.Fatal("missing common accepted")
	}
	bad := strings.Replace(commonYAML, "from_day: 8", "from_day: 9", 1)
	if _, err := Load(fstest.MapFS{"common.yaml": {Data: []byte(bad)}}, Refs{}); err == nil {
		t.Fatal("gap between periods accepted")
	}
	two := fstest.MapFS{"common.yaml": {Data: []byte(commonYAML)},
		"other.yaml": {Data: []byte(strings.Replace(commonYAML, "id: common", "id: other", 1))}}
	if _, err := Load(two, Refs{}); err == nil {
		t.Fatal("second common accepted")
	}
	noExt := fstest.MapFS{"common.yaml": {Data: []byte(commonYAML)}, "be.yaml": {Data: []byte(`schema_version: 1
id: be
title: BE
kind: direction
synthetic: false
source: test
steps:
  - { id: be-1, period: week1, order: 10, type: learn, owner: intern, title: T, help: { kind: track_only } }
`)}}
	if _, err := Load(noExt, Refs{}); err == nil {
		t.Fatal("direction without extends accepted")
	}
}
