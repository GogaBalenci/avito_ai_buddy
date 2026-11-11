package catalog

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/GogaBalenci/avito_ai_buddy/content"
)

// XM-13: the catalog loads; prod-db-write is denied to everyone.
func TestLoadCatalog(t *testing.T) {
	sub, _ := fs.Sub(content.FS(), "access")
	rs, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 7 {
		t.Fatalf("resources = %d", len(rs))
	}
	ids := IDs(rs)
	if !ids["prod-db-write"] || !ids["team-channels"] {
		t.Fatal("ids")
	}
	for _, r := range rs {
		if r.ID == "prod-db-write" && r.AllowedFor("intern") {
			t.Fatal("prod-db-write must be denied")
		}
		if r.ID == "team-channels" && !r.AllowedFor("intern") {
			t.Fatal("team-channels must be allowed")
		}
	}
}

func TestValidateRules(t *testing.T) {
	cases := map[string]string{
		"generic alias":  "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: [каналы]}\n",
		"dup alias":      "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: [x y]}\n  - {id: bb, title: B, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: [x y]}\n",
		"no deny expl":   "  - {id: aa, title: A, kind: database, description: d, policy: auto, access_level: write, approver: null, allowed_roles: [], aliases: []}\n",
		"no approver":    "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: null, allowed_roles: [intern], aliases: []}\n",
		"bad level":      "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: owner, approver: X, allowed_roles: [intern], aliases: []}\n",
		"bad policy":     "  - {id: aa, title: A, kind: channel, description: d, policy: never, access_level: read, approver: X, allowed_roles: [intern], aliases: []}\n",
		"bad kind":       "  - {id: aa, title: A, kind: bucket, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: []}\n",
		"mentor role":    "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [mentor], aliases: []}\n",
		"dup id + bad id": "  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: []}\n  - {id: aa, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: []}\n  - {id: A, title: A, kind: channel, description: d, policy: auto, access_level: read, approver: X, allowed_roles: [intern], aliases: []}\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(fstest.MapFS{"catalog.yaml": {Data: []byte("version: 1\nresources:\n" + body)}})
			if err == nil || !strings.Contains(err.Error(), "catalog.yaml") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if _, err := Load(fstest.MapFS{"catalog.yaml": {Data: []byte("x: 1")}}); err == nil {
		t.Fatal("unknown field accepted")
	}
}
