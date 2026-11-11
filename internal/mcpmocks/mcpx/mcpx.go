// Package mcpx is the shared plumbing of the mock MCP servers: tools declare
// inputSchema/outputSchema straight from contracts/, inputs and outputs are
// checked against them, errors follow common/error.v1.json (12 §7.1).
package mcpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/contracts"
)

// Actor is the caller as passed in every tool input. The mock trusts it
// because it is reachable only from the compose network (12 §7.1).
type Actor struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// Error is a tool error with a contract code.
type Error struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Err builds a tool error.
func Err(code, msg string, details map[string]any) *Error {
	return &Error{Code: code, Message: msg, Details: details, Retryable: code == "UNAVAILABLE"}
}

// Common errors.
func Denied(msg string) *Error   { return Err("PERMISSION_DENIED", msg, nil) }
func NotFound(msg string) *Error { return Err("NOT_FOUND", msg, nil) }

// Handler implements a tool: raw is the input already checked by the contract.
type Handler func(ctx context.Context, raw json.RawMessage) (any, error)

// Tool describes a tool of an endpoint.
type Tool struct {
	Endpoint    string
	Name        string
	Description string
	ReadOnly    bool
	Idempotent  bool
	Handler     Handler
}

// Add registers the tool on the server with schemas from contracts/.
func Add(s *mcp.Server, log *slog.Logger, t Tool) {
	in, err := contracts.Bundle(t.Endpoint + "/" + t.Name + ".input.v1.json")
	if err != nil {
		panic(err)
	}
	out, err := contracts.Bundle(t.Endpoint + "/" + t.Name + ".output.v1.json")
	if err != nil {
		panic(err)
	}
	ann := &mcp.ToolAnnotations{ReadOnlyHint: t.ReadOnly, IdempotentHint: t.Idempotent}
	s.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: in, OutputSchema: out, Annotations: ann},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()
			rid, _ := req.Params.Meta["request_id"].(string)
			res := Call(ctx, t, req.Params.Arguments)
			code := ""
			if res.IsError {
				code = "error"
			}
			if log != nil {
				log.InfoContext(ctx, "tool call", "tool", t.Endpoint+"."+t.Name, "request_id", rid,
					"duration_ms", time.Since(start).Milliseconds(), "is_error", code != "")
			}
			return res, nil
		})
}

// Call runs the tool on raw arguments and builds the MCP result.
func Call(ctx context.Context, t Tool, raw json.RawMessage) *mcp.CallToolResult {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := contracts.ValidateJSON(t.Endpoint+"/"+t.Name+".input.v1.json", raw); err != nil {
		var ve *contracts.ValidationError
		details := map[string]any{}
		if errors.As(err, &ve) {
			details["schema_errors"] = ve.Errors
		}
		return ErrorResult(Err("VALIDATION_FAILED", "input does not match the contract", details))
	}
	out, err := t.Handler(ctx, raw)
	if err != nil {
		var te *Error
		if errors.As(err, &te) {
			return ErrorResult(te)
		}
		return ErrorResult(Err("INTERNAL", "internal error", map[string]any{"cause": trim(err.Error())}))
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ErrorResult(Err("INTERNAL", err.Error(), nil))
	}
	if err := contracts.ValidateJSON(t.Endpoint+"/"+t.Name+".output.v1.json", b); err != nil {
		return ErrorResult(Err("INTERNAL", "output does not match the contract", map[string]any{"cause": trim(err.Error())}))
	}
	var structured any
	_ = json.Unmarshal(b, &structured)
	return &mcp.CallToolResult{StructuredContent: structured, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// ErrorResult builds an isError result by common/error.v1.json.
func ErrorResult(e *Error) *mcp.CallToolResult {
	body := map[string]any{"code": e.Code, "message": trim(e.Message), "retryable": e.Retryable}
	if len(e.Details) > 0 {
		body["details"] = e.Details
	}
	wrapped := map[string]any{"error": body}
	b, _ := json.Marshal(wrapped)
	return &mcp.CallToolResult{IsError: true, StructuredContent: wrapped, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func trim(s string) string {
	if len([]rune(s)) > 280 {
		return string([]rune(s)[:280]) + "…"
	}
	return s
}

// Decode decodes a validated input.
func Decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return Err("VALIDATION_FAILED", fmt.Sprintf("decode: %v", err), nil)
	}
	return nil
}
