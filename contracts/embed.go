// Package contracts embeds the JSON Schema contracts of the MCP tools (12 §7.3)
// and validates tool inputs and outputs against them.
//
// The same files are the documentation for a team that replaces a mock with a
// real service: the client (ToolClient) checks every answer against OUR
// contract, not against what the server declares (12 §7.1).
package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed common checklist knowledge absences access
var files embed.FS

// base is the virtual location of the contracts; $id values are relative to it.
const base = "https://contracts.buddy.local/"

// FS returns the embedded contract files.
func FS() fs.FS { return files }

// Names returns ids of all embedded schemas, sorted ("absences/request.v1.json", ...).
func Names() []string {
	var out []string
	_ = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// ValidationError lists violations of a contract.
type ValidationError struct {
	Schema string
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Schema, strings.Join(e.Errors, "; "))
}

var (
	once     sync.Once
	compiled map[string]*jsonschema.Schema
	loadErr  error
)

func load() {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	compiled = map[string]*jsonschema.Schema{}
	names := Names()
	for _, n := range names {
		raw, err := files.ReadFile(n)
		if err != nil {
			loadErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", n, err)
			return
		}
		// $id in the files is relative to the contracts root; make it absolute,
		// so relative $ref ("../common/actor.v1.json") resolve against the file.
		if m, ok := doc.(map[string]any); ok {
			m["$id"] = base + n
		}
		if err := c.AddResource(base+n, doc); err != nil {
			loadErr = fmt.Errorf("%s: %w", n, err)
			return
		}
	}
	for _, n := range names {
		s, err := c.Compile(base + n)
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", n, err)
			return
		}
		compiled[n] = s
	}
}

// Check compiles all schemas; services call it at startup (fail fast).
func Check() error {
	once.Do(load)
	return loadErr
}

// ErrUnknownSchema is returned for a schema id that is not embedded.
var ErrUnknownSchema = errors.New("unknown contract")

// ValidateJSON validates raw JSON against the schema with the given id.
func ValidateJSON(id string, raw []byte) error {
	once.Do(load)
	if loadErr != nil {
		return loadErr
	}
	s, ok := compiled[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSchema, id)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &ValidationError{Schema: id, Errors: []string{"invalid JSON: " + err.Error()}}
	}
	if err := s.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return &ValidationError{Schema: id, Errors: flatten(ve)}
		}
		return &ValidationError{Schema: id, Errors: []string{err.Error()}}
	}
	return nil
}

// Validate marshals v to JSON and validates it.
func Validate(id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ValidateJSON(id, raw)
}

func flatten(ve *jsonschema.ValidationError) []string {
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			out = append(out, fmt.Sprintf("%s: %v", loc, e.ErrorKind))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// Bundle returns the schema with all $ref resolved inline, as one JSON object.
// MCP servers declare it as inputSchema/outputSchema (12 §7.1).
func Bundle(id string) (map[string]any, error) {
	return bundle(id, map[string]bool{})
}

func bundle(id string, seen map[string]bool) (map[string]any, error) {
	if seen[id] {
		return nil, fmt.Errorf("contracts: cyclic $ref at %s", id)
	}
	raw, err := files.ReadFile(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSchema, id)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	seen[id] = true
	defer delete(seen, id)
	out, err := inline(doc, path.Dir(id), seen)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func inline(v any, dir string, seen map[string]bool) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok && !strings.HasPrefix(ref, "#") {
			sub, err := bundle(path.Clean(path.Join(dir, ref)), seen)
			if err != nil {
				return nil, err
			}
			delete(sub, "$schema")
			delete(sub, "$id")
			return sub, nil
		}
		out := make(map[string]any, len(t))
		for k, val := range t {
			r, err := inline(val, dir, seen)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			r, err := inline(val, dir, seen)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}
