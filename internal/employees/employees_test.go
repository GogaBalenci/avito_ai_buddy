package employees

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/GogaBalenci/avito_ai_buddy/content"
)

func TestLoadSeeds(t *testing.T) {
	sub, _ := fs.Sub(content.FS(), "seeds")
	list, err := Load(sub, map[string]bool{"backend": true, "frontend": true})
	if err != nil {
		t.Fatal(err)
	}
	p := NewStatic(list)
	ctx := context.Background()
	interns, _ := p.InternsOf(ctx, "u-mentor")
	if len(interns) != 2 || interns[0].UserID != "u-intern-be" || interns[1].UserID != "u-intern-fe" {
		t.Fatalf("interns = %+v", interns)
	}
	if !IsMentee(ctx, p, "u-mentor", "u-intern-fe") || IsMentee(ctx, p, "u-mentor", "u-mentor") || IsMentee(ctx, p, "x", "u-intern-be") {
		t.Fatal("IsMentee")
	}
	if _, err := p.Get(ctx, "u-nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatal("Get unknown")
	}
	be, _ := p.Get(ctx, "u-intern-be")
	env := map[string]string{"DEMO_INTERN_BE_DAYS_AGO": "5"}
	if be.StartDaysAgo(func(k string) string { return env[k] }) != 5 || be.StartDaysAgo(nil) != 3 {
		t.Fatal("StartDaysAgo")
	}
	if be.StartDaysAgo(func(string) string { return "x" }) != 3 {
		t.Fatal("bad env falls back to default")
	}
}

func TestValidate(t *testing.T) {
	bad := [][]Employee{
		{{UserID: "bad", Role: "mentor"}},
		{{UserID: "u-a", Role: "mentor"}, {UserID: "u-a", Role: "mentor"}},
		{{UserID: "u-a", Role: "intern"}},
		{{UserID: "u-a", Role: "intern", Track: "qa", MentorID: "u-m"}, {UserID: "u-m", Role: "mentor"}},
		{{UserID: "u-a", Role: "intern", Track: "backend", MentorID: "u-b"}, {UserID: "u-b", Role: "intern", Track: "backend", MentorID: "u-a"}},
		{{UserID: "u-m", Role: "mentor", Track: "backend"}},
		{{UserID: "u-s", Role: "system"}},
	}
	for i, l := range bad {
		if err := Validate(l, map[string]bool{"backend": true}); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if _, err := Load(fstest.MapFS{"employees.yaml": {Data: []byte("version: 1\nemployees: [{user_id: u-a, role: intern}]\n")}}, nil); err == nil {
		t.Error("intern without track accepted")
	}
	if _, err := Load(fstest.MapFS{"employees.yaml": {Data: []byte("x: 1")}}, nil); err == nil {
		t.Error("unknown field accepted")
	}
	if _, err := Load(fstest.MapFS{}, nil); err == nil {
		t.Error("missing file accepted")
	}
}
