package health

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeServer accepts connections and answers each with handle.
func fakeServer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); handle(c) }()
		}
	}()
	return ln.Addr().String()
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return c
}

func pgServer(t *testing.T, reply byte) string {
	return fakeServer(t, func(c net.Conn) {
		buf := make([]byte, 8)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		_, _ = c.Write([]byte{reply})
	})
}

func TestPostgresProbe(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply byte
		ok    bool
	}{{"no ssl", 'N', true}, {"ssl", 'S', true}, {"garbage", 'E', false}} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Postgres("postgres://u:p@" + pgServer(t, tc.reply) + "/db?sslmode=disable")
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Check(ctx(t)); (err == nil) != tc.ok {
				t.Fatalf("ok=%v, err=%v", tc.ok, err)
			}
		})
	}
}

func TestPostgresProbeClosedConnection(t *testing.T) {
	addr := fakeServer(t, func(net.Conn) {}) // closes immediately
	p, _ := Postgres("postgres://" + addr + "/db")
	if err := p.Check(ctx(t)); err == nil {
		t.Fatal("expected error")
	}
}

func TestProbeUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	pg, _ := Postgres("postgres://" + addr + "/db")
	rd, _ := Redis("redis://" + addr + "/0")
	for _, p := range []Probe{pg, rd} {
		if err := p.Check(ctx(t)); err == nil {
			t.Fatal("expected dial error")
		}
	}
}

func redisServer(t *testing.T, password string) string {
	return fakeServer(t, func(c net.Conn) {
		r := bufio.NewReader(c)
		authed := password == ""
		for {
			args, err := readCmd(r)
			if err != nil {
				return
			}
			switch strings.ToUpper(args[0]) {
			case "AUTH":
				if args[len(args)-1] == password {
					authed = true
					_, _ = c.Write([]byte("+OK\r\n"))
				} else {
					_, _ = c.Write([]byte("-WRONGPASS invalid password\r\n"))
				}
			case "PING":
				if authed {
					_, _ = c.Write([]byte("+PONG\r\n"))
				} else {
					_, _ = c.Write([]byte("-NOAUTH Authentication required.\r\n"))
				}
			}
		}
	})
}

func readCmd(r *bufio.Reader) ([]string, error) {
	var n int
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Sscanf(line, "*%d", &n); err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if _, err := r.ReadString('\n'); err != nil { // $len
			return nil, err
		}
		v, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		args = append(args, strings.TrimRight(v, "\r\n"))
	}
	return args, nil
}

func TestRedisProbe(t *testing.T) {
	plain := redisServer(t, "")
	secured := redisServer(t, "s3cret")
	for _, tc := range []struct {
		name, url string
		ok        bool
	}{
		{"plain", "redis://" + plain + "/0", true},
		{"auth ok", "redis://:s3cret@" + secured + "/0", true},
		{"auth with user", "redis://default:s3cret@" + secured + "/0", true},
		{"auth wrong", "redis://:nope@" + secured + "/0", false},
		{"no auth", "redis://" + secured + "/0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Redis(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Check(ctx(t)); (err == nil) != tc.ok {
				t.Fatalf("ok=%v, err=%v", tc.ok, err)
			}
		})
	}
}

func TestRedisProbeNoReply(t *testing.T) {
	addr := fakeServer(t, func(c net.Conn) { _, _ = io.Copy(io.Discard, io.LimitReader(c, 14)) })
	p, _ := Redis("redis://" + addr)
	if err := p.Check(ctx(t)); err == nil {
		t.Fatal("expected read error")
	}
}

func TestBadURLs(t *testing.T) {
	for _, u := range []string{"", "http://postgres:5432", "postgres://", "::bad"} {
		if _, err := Postgres(u); err == nil {
			t.Fatalf("Postgres(%q): expected error", u)
		}
	}
	if _, err := Redis("postgres://x:1"); err == nil {
		t.Fatal("Redis: expected scheme error")
	}
	if _, err := Postgres("http://user:topsecret@host"); err == nil || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error must not leak password: %v", err)
	}
}

func TestDefaultPorts(t *testing.T) {
	if hp, _ := hostPort("postgres://db/x", "postgres", "5432"); hp != "db:5432" {
		t.Fatal(hp)
	}
	if hp, _ := hostPort("rediss://cache", "redis", "6379"); hp != "cache:6379" {
		t.Fatal(hp)
	}
}

func TestDialWithoutDeadline(t *testing.T) {
	addr := pgServer(t, 'N')
	p, _ := Postgres("postgres://" + addr)
	if err := p.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHandler(t *testing.T) {
	ok := ProbeFunc(func(context.Context) error { return nil })
	fail := ProbeFunc(func(context.Context) error { return errors.New("down") })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range []struct {
		name   string
		probes map[string]Probe
		code   int
		status string
	}{
		{"all ok", map[string]Probe{"postgres": ok, "redis": ok}, http.StatusOK, "ok"},
		{"redis down", map[string]Probe{"postgres": ok, "redis": fail}, http.StatusServiceUnavailable, "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Handler(tc.probes, log).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tc.code {
				t.Fatalf("code = %d", rec.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != tc.status || body["postgres"] != true {
				t.Fatalf("body = %v", body)
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Fatal("content type")
			}
		})
	}
}
