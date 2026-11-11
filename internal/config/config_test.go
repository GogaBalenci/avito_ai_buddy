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
