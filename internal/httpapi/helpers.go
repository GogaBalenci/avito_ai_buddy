package httpapi

import (
	"encoding/json"

	"github.com/google/uuid"
)

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mustUUID(s string) uuid.UUID { u, _ := uuid.Parse(s); return u }

func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }
