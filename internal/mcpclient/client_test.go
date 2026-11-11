package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

var actorIntern = map[string]any{"user_id": "u-intern-be", "role": "intern"}

// newServer serves one tool get_doc on a test MCP endpoint; behavior picks the answer.
func newServer(t *testing.T, behavior func(req *mcp.CallToolRequest) (*mcp.CallToolResult, error)) string {
	t.Helper()
	in, _ := contracts.Bundle("knowledge/get_doc.input.v1.json")
	out, _ := contracts.Bundle("knowledge/get_doc.output.v1.json")
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	srv.AddTool(&mcp.Tool{Name: "get_doc", InputSchema: in, OutputSchema: out}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return behavior(req)
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts.URL
}

func doc() map[string]any {
	return map[string]any{"doc": map[string]any{"slug": "spt", "title": "SPT", "kind": "process", "summary": "s",
		"body_md": "b", "label": "test_instruction", "source_level": "playbook", "url": "/kb/spt",
		"updated_at": "2026-11-10T12:00:00Z"}}
}

func ok(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{StructuredContent: v, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

func TestCallSuccessPassesRequestID(t *testing.T) {
	var gotMeta any
	url := newServer(t, func(req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		gotMeta = req.Params.Meta["request_id"]
		return ok(doc()), nil
	})
	c := &MCP{URLs: map[string]string{"knowledge": url}, Timeout: 2 * time.Second}
	ctx := reqctx.WithRequestID(context.Background(), "req-42")
	var out struct {
		Doc struct{ Slug string } `json:"doc"`
	}
	if err := c.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "spt"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Doc.Slug != "spt" || gotMeta != "req-42" {
		t.Fatalf("out=%+v meta=%v", out, gotMeta)
	}
}

func TestCallErrors(t *testing.T) {
	cases := []struct {
		name     string
		behavior func(*mcp.CallToolRequest) (*mcp.CallToolResult, error)
		code     string
	}{
		{"tool error", func(*mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			r := ok(map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "нет", "retryable": false}})
			r.IsError = true
			return r, nil
		}, "NOT_FOUND"},
		{"answer not by contract (A33)", func(*mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return ok(map[string]any{"doc": map[string]any{"slug": "spt"}}), nil
		}, CodeInternal},
		{"error not by contract", func(*mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			r := ok(map[string]any{"oops": 1})
			r.IsError = true
			return r, nil
		}, CodeInternal},
		{"slow → timeout (A-14)", func(*mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			time.Sleep(400 * time.Millisecond)
			return ok(doc()), nil
		}, CodeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &MCP{URLs: map[string]string{"knowledge": newServer(t, tc.behavior)}, Timeout: 200 * time.Millisecond}
			start := time.Now()
			err := c.Call(context.Background(), "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "spt"}, nil)
			if !IsCode(err, tc.code) {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
			if time.Since(start) > 700*time.Millisecond {
				t.Fatal("call took longer than the timeout budget")
			}
		})
	}
	c := &MCP{URLs: map[string]string{"knowledge": "http://127.0.0.1:1/mcp"}, Timeout: time.Second}
	if err := c.Call(context.Background(), "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "spt"}, nil); !IsCode(err, CodeUnavailable) {
		t.Fatalf("down service: %v", err)
	}
	if err := c.Call(context.Background(), "nope", "x", map[string]any{}, nil); !IsCode(err, CodeInternal) {
		t.Fatalf("unknown endpoint: %v", err)
	}
	if err := c.Call(context.Background(), "knowledge", "get_doc", map[string]any{"slug": "spt"}, nil); !IsCode(err, CodeInternal) {
		t.Fatalf("bad input must not be sent: %v", err)
	}
	if err := c.Call(context.Background(), "knowledge", "get_doc", func() {}, nil); !IsCode(err, CodeInternal) {
		t.Fatal("unmarshalable input")
	}
	te := &ToolError{Tool: "a.b", Code: "X", Message: "m"}
	if !strings.Contains(te.Error(), "a.b: X") {
		t.Fatal(te.Error())
	}
}

func TestDecodeFallbackAndEmpty(t *testing.T) {
	b, _ := json.Marshal(doc())
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
	if err := Decode("knowledge.get_doc", OutputSchema("knowledge", "get_doc"), res, nil); err != nil {
		t.Fatal(err)
	}
	if err := Decode("t", "x", nil, nil); !IsCode(err, CodeInternal) {
		t.Fatal("nil result")
	}
	if err := Decode("t", "x", &mcp.CallToolResult{}, nil); !IsCode(err, CodeInternal) {
		t.Fatal("empty result")
	}
	var bad int
	if err := Decode("knowledge.get_doc", OutputSchema("knowledge", "get_doc"), ok(doc()), &bad); !IsCode(err, CodeInternal) {
		t.Fatal("decode into wrong type")
	}
}

func TestFake(t *testing.T) {
	f := NewFake().On("knowledge.get_doc", func(_ context.Context, in map[string]any) (any, error) {
		if in["slug"] == "nope" {
			return nil, &ToolError{Code: "NOT_FOUND"}
		}
		if in["slug"] == "bad" {
			return map[string]any{"doc": 1}, nil
		}
		return doc(), nil
	})
	ctx := context.Background()
	var out map[string]any
	if err := f.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "spt"}, &out); err != nil || out["doc"] == nil {
		t.Fatal(err)
	}
	if err := f.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "nope"}, nil); !IsCode(err, "NOT_FOUND") {
		t.Fatal(err)
	}
	if err := f.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": actorIntern, "slug": "bad"}, nil); !IsCode(err, CodeInternal) {
		t.Fatal(err)
	}
	if err := f.Call(ctx, "knowledge", "search", map[string]any{"actor": actorIntern, "query": "q"}, nil); !IsCode(err, CodeUnavailable) {
		t.Fatal(err)
	}
	if err := f.Call(ctx, "knowledge", "get_doc", map[string]any{"slug": "x"}, nil); !IsCode(err, CodeInternal) {
		t.Fatal(err)
	}
	if len(f.Calls("knowledge.get_doc")) != 3 || len(f.Calls("")) != 4 {
		t.Fatalf("calls: %v", f.Calls(""))
	}
}
