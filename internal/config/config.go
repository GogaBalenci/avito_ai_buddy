// Package config reads service configuration from environment variables.
// Variable names are fixed in 12-architecture.md §4.3 and .env.example.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Moscow without tzdata in the runtime image
)

// Getenv is the signature of os.Getenv; tests pass a map-backed function.
type Getenv func(string) string

// Config is the configuration of api and mcp-mocks (names: 12 §4.3).
type Config struct {
	Addr        string
	DatabaseURL string
	RedisURL    string // empty for services that do not use Redis
	Location    *time.Location
	LogLevel    slog.Level

	// MCP endpoints of corporate services (api). Replacing a mock = changing a URL.
	MCPURLs       map[string]string // checklist, knowledge, absences, access
	MCPTimeout    time.Duration
	MocksAdminURL string

	// LLM (optional). Misconfigured → demo with a notice (AG-24), not a start error.
	LLM LLMConfig

	DemoToolsEnabled   bool
	PollIntervalMS     int
	KBRHigh, KBRLow    float64
	SessionTTL         time.Duration
	CookieSecure       bool
	AllowedOrigins     []string
	AccessTimerMinutes int // mcp-mocks
	Getenv             Getenv
}

// LLMConfig is the LLM part of the configuration. APIKey is never logged.
type LLMConfig struct {
	Provider     string // demo | openai_compatible (effective)
	Requested    string // as set in LLM_PROVIDER
	BaseURL      string
	APIKey       string
	Model        string
	ExtraHeaders map[string]string
	Timeout      time.Duration
	// Misconfigured names the missing variable when openai_compatible could not start.
	Misconfigured string
}

// ErrMissing is returned when a required variable is empty.
var ErrMissing = errors.New("required environment variable is not set")

// LoadAPI loads configuration of the api service.
func LoadAPI(getenv Getenv) (Config, error) {
	c, err := load(getenv, ":8080")
	if err != nil {
		return Config{}, err
	}
	c.RedisURL = strings.TrimSpace(getenv("REDIS_URL"))
	if c.RedisURL == "" {
		return Config{}, fmt.Errorf("REDIS_URL: %w", ErrMissing)
	}
	c.MCPURLs = map[string]string{}
	for _, svc := range []string{"checklist", "knowledge", "absences", "access"} {
		name := "MCP_" + strings.ToUpper(svc) + "_URL"
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			v = "http://mcp-mocks:8090/mcp/" + svc
		}
		if _, err := url.ParseRequestURI(v); err != nil {
			return Config{}, fmt.Errorf("%s: %w", name, err)
		}
		c.MCPURLs[svc] = v
	}
	c.MocksAdminURL = strings.TrimRight(orDefault(getenv("MCP_MOCKS_ADMIN_URL"), "http://mcp-mocks:8090/internal"), "/")
	ms, err := intVar(getenv, "MCP_TIMEOUT_MS", 3000, 50)
	if err != nil {
		return Config{}, err
	}
	c.MCPTimeout = time.Duration(ms) * time.Millisecond
	if c.PollIntervalMS, err = intVar(getenv, "PATH_POLL_INTERVAL_MS", 10000, 1000); err != nil {
		return Config{}, err
	}
	hours, err := intVar(getenv, "SESSION_TTL_HOURS", 12, 1)
	if err != nil {
		return Config{}, err
	}
	c.SessionTTL = time.Duration(hours) * time.Hour
	if c.KBRHigh, err = floatVar(getenv, "KB_R_HIGH", 0.65); err != nil {
		return Config{}, err
	}
	if c.KBRLow, err = floatVar(getenv, "KB_R_LOW", 0.3); err != nil {
		return Config{}, err
	}
	c.CookieSecure = boolVar(getenv, "COOKIE_SECURE", false)
	for _, o := range strings.Split(getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	if c.LLM, err = loadLLM(getenv); err != nil {
		return Config{}, err
	}
	return c, nil
}

// LoadMocks loads configuration of the mcp-mocks service.
func LoadMocks(getenv Getenv) (Config, error) {
	c, err := load(getenv, ":8090")
	if err != nil {
		return Config{}, err
	}
	if c.AccessTimerMinutes, err = intVar(getenv, "ACCESS_TIMER_MINUTES", 2, 1); err != nil {
		return Config{}, err
	}
	return c, nil
}

func loadLLM(getenv Getenv) (LLMConfig, error) {
	l := LLMConfig{Requested: strings.ToLower(orDefault(getenv("LLM_PROVIDER"), "demo"))}
	l.BaseURL = strings.TrimRight(strings.TrimSpace(getenv("LLM_BASE_URL")), "/")
	l.APIKey = strings.TrimSpace(getenv("LLM_API_KEY"))
	l.Model = strings.TrimSpace(getenv("LLM_MODEL"))
	secs, err := intVar(getenv, "LLM_TIMEOUT_SECONDS", 10, 1)
	if err != nil {
		return l, err
	}
	l.Timeout = time.Duration(secs) * time.Second
	l.ExtraHeaders = map[string]string{}
	for _, kv := range strings.Split(getenv("LLM_EXTRA_HEADERS"), ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			k, v, ok = strings.Cut(kv, ":")
		}
		if ok && strings.TrimSpace(k) != "" {
			l.ExtraHeaders[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	switch l.Requested {
	case "demo":
		l.Provider = "demo"
	case "openai_compatible":
		l.Provider = "openai_compatible"
		switch {
		case l.BaseURL == "":
			l.Misconfigured = "LLM_BASE_URL"
		case l.Model == "":
			l.Misconfigured = "LLM_MODEL"
		}
		if l.Misconfigured != "" {
			l.Provider = "demo" // AG-24: start in demo with a notice
		}
	default:
		return l, fmt.Errorf("LLM_PROVIDER: unknown provider %q (demo|openai_compatible)", l.Requested)
	}
	return l, nil
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v == "" {
		return def
	}
	return v
}

func intVar(getenv Getenv, name string, def, min int) (int, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s: want an integer >= %d, got %q", name, min, v)
	}
	return n, nil
}

func floatVar(getenv Getenv, name string, def float64) (float64, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		return 0, fmt.Errorf("%s: want a number in [0, 1], got %q", name, v)
	}
	return f, nil
}

func boolVar(getenv Getenv, name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func load(getenv Getenv, defaultAddr string) (Config, error) {
	c := Config{Addr: defaultAddr, Getenv: getenv}
	if v := strings.TrimSpace(getenv("HTTP_ADDR")); v != "" { // only for local runs and tests
		c.Addr = v
	}

	c.DatabaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL: %w", ErrMissing)
	}

	tz := strings.TrimSpace(getenv("TZ"))
	if tz == "" {
		tz = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("TZ: %w", err)
	}
	c.Location = loc

	lvl, err := parseLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}
	c.LogLevel = lvl
	c.DemoToolsEnabled = boolVar(getenv, "DEMO_TOOLS_ENABLED", true)
	return c, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL: unknown level %q (debug|info|warn|error)", s)
	}
}
