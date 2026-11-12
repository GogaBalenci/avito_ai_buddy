// Package access is the application side of the access service (S4, 11 §3):
// the catalog as the access service gives it to the actor, and helpers shared
// by drafts and the orchestrator.
package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
)

// Resource is access/resource.v1.json.
type Resource struct {
	ResourceID      string   `json:"resource_id"`
	Title           string   `json:"title"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	Policy          string   `json:"policy"`
	EffectivePolicy string   `json:"effective_policy"`
	TimerMinutes    *int     `json:"timer_minutes"`
	AccessLevel     string   `json:"access_level"`
	Approver        *string  `json:"approver"`
	Allowed         bool     `json:"allowed"`
	DenyReasonCode  *string  `json:"deny_reason_code"`
	DenyExplanation *string  `json:"deny_explanation"`
	Aliases         []string `json:"aliases"`
}

// Catalog reads the catalog through ToolClient (no cache: the effective
// policy depends on a demo flag that may change at any moment).
type Catalog struct {
	Tools mcpclient.Client
}

// List returns the whole catalog for the actor, with `allowed`.
func (c *Catalog) List(ctx context.Context, a actor.Actor) ([]Resource, error) {
	if c == nil || c.Tools == nil {
		return nil, errNoCatalog
	}
	var out struct {
		Items []Resource `json:"items"`
	}
	if err := c.Tools.Call(ctx, "access", "list_resources", map[string]any{"actor": a, "include_denied": true}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

var errNoCatalog = errors.New("access catalog is not configured")

// Get returns one resource; ok=false if it is not in the catalog.
func (c *Catalog) Get(ctx context.Context, a actor.Actor, id string) (Resource, bool, error) {
	rs, err := c.List(ctx, a)
	if err != nil {
		return Resource{}, false, err
	}
	for _, r := range rs {
		if r.ResourceID == id {
			return r, true, nil
		}
	}
	return Resource{}, false, nil
}

// ParamsHash is SHA-256 of the canonical JSON {"resource_id": …} (11 §3.12).
func ParamsHash(resourceID string) string {
	b, _ := json.Marshal(map[string]string{"resource_id": resourceID})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// LevelRU is the access level in the card (11 §3.7, p. 4).
func LevelRU(level string) string {
	switch level {
	case "read":
		return "чтение"
	case "write":
		return "запись"
	case "admin":
		return "администрирование"
	}
	return level
}

// PolicyHint is the rule hint of a draft (11 §3.7, p. 4).
func PolicyHint(policy string, timerMinutes *int) string {
	switch policy {
	case "auto":
		return "Одобряется сразу"
	case "mentor":
		return "Заявку одобряет наставник"
	}
	n := 2
	if timerMinutes != nil {
		n = *timerMinutes
	}
	return "Одобрится автоматически примерно через " + strconv.Itoa(n) + " мин"
}
