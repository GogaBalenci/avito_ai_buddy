package mcpclient

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/GogaBalenci/avito_ai_buddy/contracts"
)

// Handler answers a fake tool call: return a value matching the output
// contract or a *ToolError.
type Handler func(ctx context.Context, in map[string]any) (any, error)

// Fake is an in-process ToolClient for orchestrator tests. Inputs and outputs
// are checked against the same contracts as the real client.
type Fake struct {
	mu       sync.Mutex
	handlers map[string]Handler
	calls    []FakeCall
}

// FakeCall is one recorded call.
type FakeCall struct {
	Tool  string
	Input map[string]any
}

// NewFake returns an empty fake.
func NewFake() *Fake { return &Fake{handlers: map[string]Handler{}} }

// On registers a handler for "endpoint.tool".
func (f *Fake) On(tool string, h Handler) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[tool] = h
	return f
}

// Calls returns recorded calls of a tool ("" for all).
func (f *Fake) Calls(tool string) []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FakeCall
	for _, c := range f.calls {
		if tool == "" || c.Tool == tool {
			out = append(out, c)
		}
	}
	return out
}

// Call implements Client.
func (f *Fake) Call(ctx context.Context, endpoint, tool string, in any, out any) error {
	name := endpoint + "." + tool
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if err := contracts.ValidateJSON(InputSchema(endpoint, tool), raw); err != nil {
		return &ToolError{Tool: name, Code: CodeInternal, Message: "input violates contract: " + err.Error()}
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	f.mu.Lock()
	f.calls = append(f.calls, FakeCall{Tool: name, Input: m})
	h := f.handlers[name]
	f.mu.Unlock()
	if h == nil {
		return &ToolError{Tool: name, Code: CodeUnavailable, Message: "no fake handler", Retryable: true}
	}
	res, err := h(ctx, m)
	if err != nil {
		if te, ok := AsToolError(err); ok && te.Tool == "" {
			te.Tool = name
		}
		return err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	if err := contracts.ValidateJSON(OutputSchema(endpoint, tool), b); err != nil {
		return &ToolError{Tool: name, Code: CodeInternal, Message: "answer violates contract: " + err.Error(), Retryable: true}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}
