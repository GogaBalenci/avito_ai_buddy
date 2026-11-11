// Package catalog loads the synthetic access catalog (content/access/catalog.yaml)
// for the access mock and checks it (11 §3.10.6).
package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

// Resource is an item of the access catalog.
type Resource struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	Policy          string   `json:"policy"`
	AccessLevel     string   `json:"access_level"`
	Approver        *string  `json:"approver"`
	AllowedRoles    []string `json:"allowed_roles"`
	DenyExplanation *string  `json:"deny_explanation,omitempty"`
	Aliases         []string `json:"aliases"`
	Sort            int      `json:"-"`
}

// AllowedFor reports the ABAC condition resource_allowed (13 §5).
func (r Resource) AllowedFor(role string) bool {
	for _, x := range r.AllowedRoles {
		if x == role {
			return true
		}
	}
	return false
}

type file struct {
	Version   int        `json:"version"`
	Resources []Resource `json:"resources"`
}

// genericAliases are single generic words that must not be aliases (§3.5.4):
// "в каком канале…" is a question, not an access request.
var genericAliases = map[string]bool{
	"канал": true, "каналы": true, "чат": true, "чаты": true, "доступ": true,
	"пространство": true, "репозиторий": true, "база": true,
}

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)

// Load reads catalog.yaml from fsys (the access directory).
func Load(fsys fs.FS) ([]Resource, error) {
	raw, err := fs.ReadFile(fsys, "catalog.yaml")
	if err != nil {
		return nil, err
	}
	var f file
	if _, err := yamlx.Decode(raw, &f); err != nil {
		return nil, fmt.Errorf("access/catalog.yaml: %w", err)
	}
	for i := range f.Resources {
		f.Resources[i].Sort = i + 1
	}
	if err := Validate(f.Resources); err != nil {
		return nil, err
	}
	return f.Resources, nil
}

// Validate applies the catalog rules.
func Validate(rs []Resource) error {
	var errs []error
	ids := map[string]bool{}
	aliasOwner := map[string]string{}
	bad := func(id, msg string) { errs = append(errs, fmt.Errorf("access/catalog.yaml: %s: %s", id, msg)) }
	for _, r := range rs {
		if !idRe.MatchString(r.ID) {
			bad(r.ID, "bad id")
		}
		if ids[r.ID] {
			bad(r.ID, "duplicate id")
		}
		ids[r.ID] = true
		switch r.Kind {
		case "channel", "space", "repo", "tool", "database":
		default:
			bad(r.ID, "unknown kind "+r.Kind)
		}
		switch r.Policy {
		case "auto", "mentor", "timer":
		default:
			bad(r.ID, "unknown policy "+r.Policy)
		}
		switch r.AccessLevel {
		case "read", "write", "admin":
		default:
			bad(r.ID, "access_level must be read, write or admin")
		}
		for _, role := range r.AllowedRoles {
			if role != "intern" {
				bad(r.ID, "allowed_roles may contain only intern")
			}
		}
		if len(r.AllowedRoles) == 0 && (r.DenyExplanation == nil || strings.TrimSpace(*r.DenyExplanation) == "") {
			bad(r.ID, "resource nobody may request needs deny_explanation")
		}
		if len(r.AllowedRoles) > 0 && (r.Approver == nil || strings.TrimSpace(*r.Approver) == "") {
			bad(r.ID, "approver is required for a requestable resource")
		}
		for _, a := range r.Aliases {
			a = strings.ToLower(strings.TrimSpace(a))
			if genericAliases[a] {
				bad(r.ID, "alias "+a+" is a single generic word")
			}
			if owner, dup := aliasOwner[a]; dup && owner != r.ID {
				bad(r.ID, "alias "+a+" is also an alias of "+owner)
			}
			aliasOwner[a] = r.ID
		}
	}
	return errors.Join(errs...)
}

// IDs returns the set of resource ids (for the tracks cross-check).
func IDs(rs []Resource) map[string]bool {
	m := make(map[string]bool, len(rs))
	for _, r := range rs {
		m[r.ID] = true
	}
	return m
}
