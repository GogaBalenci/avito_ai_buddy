// Package config reads service configuration from environment variables.
// Variable names are fixed in 12-architecture.md §4.3 and .env.example.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Moscow without tzdata in the runtime image
)

// Getenv is the signature of os.Getenv; tests pass a map-backed function.
type Getenv func(string) string

// Config is the infrastructure part of the configuration needed at C0.
// Business settings (LLM, MCP, demo) are added together with their features.
type Config struct {
	Addr        string
	DatabaseURL string
	RedisURL    string // empty for services that do not use Redis
	Location    *time.Location
	LogLevel    slog.Level
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
	return c, nil
}

// LoadMocks loads configuration of the mcp-mocks service.
func LoadMocks(getenv Getenv) (Config, error) {
	return load(getenv, ":8090")
}

func load(getenv Getenv, defaultAddr string) (Config, error) {
	c := Config{Addr: defaultAddr}
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
