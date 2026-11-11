// Package mcpclient is ToolClient (12 §5.1): the only way the orchestrator
// reaches corporate services. Addresses come from MCP_*_URL; every answer is
// checked against OUR contract from contracts/ (not the server's tools/list),
// so a real service that drifted from the contract gives a clear INTERNAL
// error instead of a wrong answer to the intern (12 §7.1).
package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// Client calls MCP tools. endpoint: checklist | knowledge | absences | access.
type Client interface {
	Call(ctx context.Context, endpoint, tool string, in any, out any) error
}

// Error codes of ToolError besides the contract ones (common/error.v1.json).
const (
	CodeTimeout     = "TIMEOUT"
	CodeUnavailable = "UNAVAILABLE"
	CodeInternal    = "INTERNAL"
)

// ToolError is a tool failure: a contract error or a transport problem.
type ToolError struct {
	Tool      string
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
}

func (e *ToolError) Error() string { return fmt.Sprintf("%s: %s: %s", e.Tool, e.Code, e.Message) }

// AsToolError extracts a *ToolError.
func AsToolError(err error) (*ToolError, bool) {
	var te *ToolError
	ok := errors.As(err, &te)
	return te, ok
}

// IsCode reports whether err is a ToolError with the code.
func IsCode(err error, code string) bool {
	te, ok := AsToolError(err)
	return ok && te.Code == code
}

// MCP is the real client over Streamable HTTP.
type MCP struct {
	URLs    map[string]string
	Timeout time.Duration
	HTTP    *http.Client
	Log     *slog.Logger
}

// InputSchema and OutputSchema return contract ids of a tool.
func InputSchema(endpoint, tool string) string  { return endpoint + "/" + tool + ".input.v1.json" }
func OutputSchema(endpoint, tool string) string { return endpoint + "/" + tool + ".output.v1.json" }

// Call invokes endpoint.tool with in and decodes the checked result into out.
func (c *MCP) Call(ctx context.Context, endpoint, tool string, in any, out any) error {
	name := endpoint + "." + tool
	start := time.Now()
	err := c.call(ctx, endpoint, tool, in, out)
	if c.Log != nil {
		code := ""
		if te, ok := AsToolError(err); ok {
			code = te.Code
		}
		c.Log.InfoContext(ctx, "mcp call", "tool", name, "request_id", reqctx.RequestID(ctx),
			"duration_ms", time.Since(start).Milliseconds(), "error_code", code)
	}
	return err
}

func (c *MCP) call(ctx context.Context, endpoint, tool string, in any, out any) error {
	name := endpoint + "." + tool
	url, ok := c.URLs[endpoint]
	if !ok {
		return &ToolError{Tool: name, Code: CodeInternal, Message: "unknown endpoint " + endpoint}
	}
	args, err := json.Marshal(in)
	if err != nil {
		return &ToolError{Tool: name, Code: CodeInternal, Message: err.Error()}
	}
	if err := contracts.ValidateJSON(InputSchema(endpoint, tool), args); err != nil {
		return &ToolError{Tool: name, Code: CodeInternal, Message: "input violates contract: " + err.Error()}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "buddy-api", Version: "1.0.0"}, nil)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	sess, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: hc, MaxRetries: -1}, nil)
	if err != nil {
		return transportError(ctx, name, err)
	}
	defer func() { _ = sess.Close() }()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(args),
		Meta: mcp.Meta{"request_id": reqctx.RequestID(ctx)}})
	if err != nil {
		return transportError(ctx, name, err)
	}
	return Decode(name, OutputSchema(endpoint, tool), res, out)
}

func transportError(ctx context.Context, tool string, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &ToolError{Tool: tool, Code: CodeTimeout, Message: "timeout", Retryable: true}
	}
	return &ToolError{Tool: tool, Code: CodeUnavailable, Message: err.Error(), Retryable: true}
}

// Decode turns a tools/call result into out or a *ToolError, checking the
// structured content against the output contract or common/error.v1.json.
func Decode(tool, outputSchema string, res *mcp.CallToolResult, out any) error {
	raw, err := structured(res)
	if err != nil {
		return &ToolError{Tool: tool, Code: CodeInternal, Message: err.Error(), Retryable: true}
	}
	if res.IsError {
		if err := contracts.ValidateJSON("common/error.v1.json", raw); err != nil {
			return &ToolError{Tool: tool, Code: CodeInternal, Message: "error violates contract: " + err.Error(), Retryable: true}
		}
		var e struct {
			Error struct {
				Code      string         `json:"code"`
				Message   string         `json:"message"`
				Retryable bool           `json:"retryable"`
				Details   map[string]any `json:"details"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return &ToolError{Tool: tool, Code: e.Error.Code, Message: e.Error.Message, Retryable: e.Error.Retryable, Details: e.Error.Details}
	}
	if err := contracts.ValidateJSON(outputSchema, raw); err != nil {
		return &ToolError{Tool: tool, Code: CodeInternal, Message: "answer violates contract: " + err.Error(), Retryable: true}
	}
	if isNil(out) {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &ToolError{Tool: tool, Code: CodeInternal, Message: err.Error(), Retryable: true}
	}
	return nil
}

// isNil also catches a typed nil pointer passed as out.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func structured(res *mcp.CallToolResult) ([]byte, error) {
	if res == nil {
		return nil, errors.New("empty result")
	}
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	// Fallback: the same JSON in content[0].text (12 §7.1).
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return []byte(t.Text), nil
		}
	}
	return nil, errors.New("no structured content")
}
