package migrate_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/GogaBalenci/avito_ai_buddy/internal/migrate"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit/pgtest"
	"github.com/GogaBalenci/avito_ai_buddy/migrations"
)

func TestApplyIsIdempotent(t *testing.T) {
	pool, _ := pgtest.New(t)
	ctx := testkit.Ctx(t)
	// the template already has both chains: a second run applies nothing
	for _, c := range []struct {
		f      fs.FS
		schema string
	}{{migrations.App(), "app"}, {migrations.Mocks(), "mock_admin"}} {
		got, err := migrate.Apply(ctx, pool, c.f, c.schema)
		if err != nil || len(got) != 0 {
			t.Fatalf("%s: applied %v, err %v", c.schema, got, err)
		}
	}
	extra := fstest.MapFS{"0001_x.sql": {Data: []byte("CREATE TABLE scratch.t (id int); INSERT INTO scratch.t VALUES (1);")}}
	got, err := migrate.Apply(ctx, pool, extra, "scratch")
	if err != nil || len(got) != 1 {
		t.Fatalf("applied %v, err %v", got, err)
	}
	if _, err := migrate.Apply(ctx, pool, fstest.MapFS{"0002_bad.sql": {Data: []byte("SELECT nope FROM nowhere")}}, "scratch"); err == nil {
		t.Fatal("bad migration accepted")
	}
	if _, err := migrate.Apply(ctx, pool, extra, "bad-name"); err == nil {
		t.Fatal("bad schema name accepted")
	}
}

// 12 §2.1: application code never touches mock_* schemas.
func TestAppDoesNotTouchMockSchemas(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch rel {
			case ".git", "web", "docs", "openspec", "internal/mcpmocks", "migrations/mocks", "internal/testkit", "cmd/mcp-mocks", "test":
				return filepath.SkipDir
			}
			return nil
		}
		if (!strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".sql")) || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "mock_") && !strings.Contains(rel, "migrate_test") {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, "mock_") && !allowedMockMention(line) {
					t.Errorf("%s touches mock schemas: %s", rel, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Flag names (mock_absences_fault) are proxied to the mock by name, not by SQL.
func allowedMockMention(line string) bool {
	l := strings.TrimSpace(line)
	if strings.HasPrefix(l, "//") || strings.HasPrefix(l, "--") {
		return true // comments may name the schemas
	}
	return strings.Contains(line, "mock_absences_fault") || strings.Contains(line, "\"mock_admin\"") || strings.Contains(line, "Mocks()") || strings.Contains(line, "mock_* schemas")
}
