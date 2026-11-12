package config

import (
	"errors"
	"log/slog"
	"testing"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func TestLoadAPI(t *testing.T) {
	c, err := LoadAPI(env(map[string]string{
		"DATABASE_URL": "postgres://u:p@postgres:5432/db",
		"REDIS_URL":    "redis://redis:6379/0",
		"LOG_LEVEL":    "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.RedisURL != "redis://redis:6379/0" || c.LogLevel != slog.LevelDebug {
		t.Fatalf("unexpected config: %+v", c)
	}
	if c.Location.String() != "Europe/Moscow" {
		t.Fatalf("default TZ = %s", c.Location)
	}
}

func TestLoadAPIRequiresRedis(t *testing.T) {
	_, err := LoadAPI(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("want ErrMissing, got %v", err)
	}
}

func TestLoadMocks(t *testing.T) {
	c, err := LoadMocks(env(map[string]string{
		"DATABASE_URL": "postgres://x",
		"HTTP_ADDR":    "127.0.0.1:0",
		"TZ":           "UTC",
		"LOG_LEVEL":    "WARN",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != "127.0.0.1:0" || c.Location.String() != "UTC" || c.LogLevel != slog.LevelWarn {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"no database": {},
		"bad tz":      {"DATABASE_URL": "postgres://x", "TZ": "Mars/Olympus"},
		"bad level":   {"DATABASE_URL": "postgres://x", "LOG_LEVEL": "loud"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadMocks(env(m)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "info": slog.LevelInfo, "warning": slog.LevelWarn, "error": slog.LevelError} {
		got, err := parseLevel(in)
		if err != nil || got != want {
			t.Fatalf("parseLevel(%q) = %v, %v", in, got, err)
		}
	}
}

func TestLoadAPIBusinessSettings(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://r"}
	c, err := LoadAPI(env(base))
	if err != nil {
		t.Fatal(err)
	}
	if c.MCPURLs["absences"] != "http://mcp-mocks:8090/mcp/absences" || c.MCPTimeout.Milliseconds() != 3000 {
		t.Fatalf("mcp defaults: %+v", c.MCPURLs)
	}
	if c.LLM.Provider != "demo" || !c.DemoToolsEnabled || c.KBRHigh != 0.65 || c.KBRLow != 0.3 || c.SessionTTL.Hours() != 12 || c.PollIntervalMS != 10000 {
		t.Fatalf("defaults: %+v", c)
	}
	m := map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://r",
		"LLM_PROVIDER": "openai_compatible", "LLM_BASE_URL": "https://llm.example/v1/", "LLM_EXTRA_HEADERS": "X-Folder=abc, X-B: c",
		"ALLOWED_ORIGINS": "http://a.test/, http://b.test", "COOKIE_SECURE": "true", "DEMO_TOOLS_ENABLED": "false"}
	c, err = LoadAPI(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Provider != "demo" || c.LLM.Misconfigured != "LLM_MODEL" || c.LLM.BaseURL != "https://llm.example/v1" {
		t.Fatalf("AG-24: %+v", c.LLM)
	}
	if c.LLM.ExtraHeaders["X-Folder"] != "abc" || c.LLM.ExtraHeaders["X-B"] != "c" {
		t.Fatalf("headers: %v", c.LLM.ExtraHeaders)
	}
	if len(c.AllowedOrigins) != 2 || c.AllowedOrigins[0] != "http://a.test" || !c.CookieSecure || c.DemoToolsEnabled {
		t.Fatalf("origins: %+v", c)
	}
	m["LLM_MODEL"] = "m"
	if c, _ = LoadAPI(env(m)); c.LLM.Provider != "openai_compatible" || c.LLM.Misconfigured != "" {
		t.Fatalf("configured: %+v", c.LLM)
	}
	delete(m, "LLM_BASE_URL")
	if c, _ = LoadAPI(env(m)); c.LLM.Misconfigured != "LLM_BASE_URL" {
		t.Fatalf("no base url: %+v", c.LLM)
	}
	for _, v := range []string{"gigachat", "openai", "Ollama"} {
		c, err := LoadAPI(env(map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://r", "LLM_PROVIDER": v}))
		if err != nil || c.LLM.Provider != "demo" || c.LLM.Requested != "openai_compatible" ||
			c.LLM.Misconfigured != "LLM_PROVIDER" || c.LLM.MisconfiguredCode() != "invalid_LLM_PROVIDER" {
			t.Errorf("LLM_PROVIDER=%s: %+v %v", v, c.LLM, err)
		}
	}
	if (LLMConfig{Misconfigured: "LLM_MODEL"}).MisconfiguredCode() != "missing_LLM_MODEL" || (LLMConfig{}).MisconfiguredCode() != "" {
		t.Error("MisconfiguredCode")
	}
	for k, v := range map[string]string{"MCP_TIMEOUT_MS": "x", "KB_R_HIGH": "2",
		"SESSION_TTL_HOURS": "0", "PATH_POLL_INTERVAL_MS": "5", "LLM_TIMEOUT_SECONDS": "-1", "MCP_ACCESS_URL": "::bad", "KB_R_LOW": "x"} {
		bad := map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://r", k: v}
		if _, err := LoadAPI(env(bad)); err == nil {
			t.Errorf("%s=%s accepted", k, v)
		}
	}
	if c, err := LoadMocks(env(map[string]string{"DATABASE_URL": "postgres://x", "ACCESS_TIMER_MINUTES": "5"})); err != nil || c.AccessTimerMinutes != 5 {
		t.Fatalf("mocks timer: %v %v", c.AccessTimerMinutes, err)
	}
	if _, err := LoadMocks(env(map[string]string{"DATABASE_URL": "postgres://x", "ACCESS_TIMER_MINUTES": "0"})); err == nil {
		t.Fatal("timer 0 accepted")
	}
	if boolVar(env(map[string]string{"X": "maybe"}), "X", true) != true || boolVar(env(map[string]string{"X": "off"}), "X", true) {
		t.Fatal("boolVar")
	}
}
