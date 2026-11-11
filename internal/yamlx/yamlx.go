// Package yamlx decodes YAML content files through JSON, so the same
// value is checked by JSON Schema and decoded into structs with json tags.
package yamlx

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/goccy/go-yaml"
)

// ToJSON converts a YAML document into JSON bytes.
func ToJSON(b []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	return json.Marshal(v)
}

// Decode converts YAML to JSON and decodes it into out, rejecting unknown fields.
func Decode(b []byte, out any) ([]byte, error) {
	j, err := ToJSON(b)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(j))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return j, fmt.Errorf("decode: %w", err)
	}
	return j, nil
}
