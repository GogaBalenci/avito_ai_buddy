package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/contracts"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mockstest"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/server"
	"github.com/GogaBalenci/avito_ai_buddy/internal/testkit"
)

// C-11 (and M-14, KM-12, XM-14 as endpoints get tools): tools/list declares
// exactly the schemas from contracts/.
func TestToolsListMatchesContracts(t *testing.T) {
	env := mockstest.Start(t, testkit.NewClock(testkit.Today))
	for _, ep := range []string{"checklist"} {
		client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil)
		sess, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: env.URL + "/mcp/" + ep}, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := sess.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Tools) == 0 {
			t.Fatalf("%s: no tools", ep)
		}
		for _, tool := range res.Tools {
			for kind, got := range map[string]any{"input": tool.InputSchema, "output": tool.OutputSchema} {
				want, err := contracts.Bundle(ep + "/" + tool.Name + "." + kind + ".v1.json")
				if err != nil {
					t.Fatal(err)
				}
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(want)
				var g, w any
				_ = json.Unmarshal(gb, &g)
				_ = json.Unmarshal(wb, &w)
				if !reflect.DeepEqual(g, w) {
					t.Errorf("%s.%s %s schema differs from contracts", ep, tool.Name, kind)
				}
			}
		}
		_ = sess.Close()
	}
}

func TestHealthzAndDemoEndpoints(t *testing.T) {
	env := mockstest.Start(t, testkit.NewClock(testkit.Today))
	get := func(path string) (int, string) {
		r, err := http.Get(env.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	post := func(path, body string) (int, string) {
		r, err := http.Post(env.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if code, body := get("/healthz"); code != 200 || !strings.Contains(body, `"postgres":true`) {
		t.Fatalf("healthz %d %s", code, body)
	}
	if code, body := get("/internal/demo/flags"); code != 200 || !strings.Contains(body, "mock_absences_fault") {
		t.Fatalf("flags %d %s", code, body)
	}
	if code, _ := post("/internal/demo/flags", `{"name":"mock_absences_fault","value":"unavailable"}`); code != 200 {
		t.Fatal(code)
	}
	if code, _ := post("/internal/demo/flags", `{"name":"llm_enabled","value":"off"}`); code != 400 {
		t.Fatal("non-white-listed flag accepted")
	}
	if code, _ := post("/internal/demo/flags", `{`); code != 400 {
		t.Fatal("bad body")
	}
	ctx := context.Background()
	if _, err := env.Pool.Exec(ctx, `UPDATE mock_checklist.progress SET status = 'done' WHERE step_id = 'be-1.11'`); err != nil {
		t.Fatal(err)
	}
	if code, body := post("/internal/demo/reset", ``); code != 204 {
		t.Fatalf("reset %d %s", code, body)
	}
	var st, fault string
	_ = env.Pool.QueryRow(ctx, `SELECT status FROM mock_checklist.progress WHERE employee_id = 'u-intern-be' AND step_id = 'be-1.11'`).Scan(&st)
	_ = env.Pool.QueryRow(ctx, `SELECT value FROM mock_admin.flags WHERE name = 'mock_absences_fault'`).Scan(&fault)
	if st != "in_progress" || fault != "off" {
		t.Fatalf("after reset: %s %s", st, fault)
	}
	if env.Mocks.Flags.Get(ctx, "nope") != "" || env.Mocks.Flags.Get(ctx, "access_mentor_approval") != "on" {
		t.Fatal("flag get")
	}
}

// P-A15: invalid content stops the service with the file and rule in the error.
func TestLoadContentFailFast(t *testing.T) {
	if _, err := server.LoadContent(content.FS()); err != nil {
		t.Fatal(err)
	}
	bad := fstest.MapFS{"kb/x.md": {Data: []byte("no front matter")}}
	if _, err := server.LoadContent(bad); err == nil || !strings.Contains(err.Error(), "x.md") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunAndErrors(t *testing.T) {
	if err := server.Run(context.Background(), func(string) string { return "" }, nil); err == nil {
		t.Fatal("config error expected")
	}
	cfg, _ := config.LoadMocks(func(k string) string { return map[string]string{"DATABASE_URL": "http://x"}[k] })
	if _, err := server.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), testkit.NewClock(time.Now()), server.Options{}); err == nil {
		t.Fatal("bad url accepted")
	}
}
