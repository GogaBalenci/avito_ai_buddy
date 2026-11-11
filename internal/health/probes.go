// Package health implements dependency probes and the /healthz handler.
//
// C0 uses protocol-level probes built on the standard library only, so the
// scaffold builds without third-party modules. At C1 the probes are replaced
// by pgxpool.Ping and go-redis Ping; the Probe interface and the /healthz
// response format stay the same.
package health

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Probe checks a single dependency.
type Probe interface {
	Check(ctx context.Context) error
}

// ProbeFunc adapts a function to Probe.
type ProbeFunc func(ctx context.Context) error

// Check implements Probe.
func (f ProbeFunc) Check(ctx context.Context) error { return f(ctx) }

// sslRequestCode is the PostgreSQL SSLRequest magic number.
const sslRequestCode = 80877103

// Postgres returns a probe that asks the server for SSL negotiation.
// A running PostgreSQL always answers with a single byte 'S' or 'N';
// closing the connection afterwards is not logged by the server.
func Postgres(databaseURL string) (Probe, error) {
	addr, err := hostPort(databaseURL, "postgres", "5432")
	if err != nil {
		return nil, err
	}
	return ProbeFunc(func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()

		msg := make([]byte, 8)
		binary.BigEndian.PutUint32(msg[0:4], 8)
		binary.BigEndian.PutUint32(msg[4:8], sslRequestCode)
		if _, err := conn.Write(msg); err != nil {
			return fmt.Errorf("postgres: write: %w", err)
		}
		reply := make([]byte, 1)
		if _, err := conn.Read(reply); err != nil {
			return fmt.Errorf("postgres: read: %w", err)
		}
		if reply[0] != 'S' && reply[0] != 'N' {
			return fmt.Errorf("postgres: unexpected reply %q", reply[0])
		}
		return nil
	}), nil
}

// Redis returns a probe that sends PING (with AUTH if the URL has a password)
// and expects +PONG.
func Redis(redisURL string) (Probe, error) {
	addr, err := hostPort(redisURL, "redis", "6379")
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(redisURL) // already validated by hostPort
	var auth []string
	if pw, ok := u.User.Password(); ok {
		auth = []string{"AUTH", pw}
		if name := u.User.Username(); name != "" {
			auth = []string{"AUTH", name, pw}
		}
	}
	return ProbeFunc(func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		r := bufio.NewReader(conn)

		if auth != nil {
			if err := redisCmd(conn, r, "+OK", auth...); err != nil {
				return err
			}
		}
		return redisCmd(conn, r, "+PONG", "PING")
	}), nil
}

func redisCmd(conn net.Conn, r *bufio.Reader, want string, args ...string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := conn.Write([]byte(b.String())); err != nil {
		return fmt.Errorf("redis: write: %w", err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("redis: read: %w", err)
	}
	if got := strings.TrimRight(line, "\r\n"); got != want {
		return fmt.Errorf("redis: %s: unexpected reply %q", args[0], got)
	}
	return nil
}

func dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	return conn, nil
}

// errBadURL is returned for URLs without a host or with a wrong scheme.
var errBadURL = errors.New("invalid URL")

func hostPort(raw, scheme, defaultPort string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s URL: %w", scheme, err)
	}
	if !strings.HasPrefix(u.Scheme, scheme) || u.Hostname() == "" {
		return "", fmt.Errorf("%s URL %q: %w", scheme, redact(u), errBadURL)
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

// redact hides credentials so they never reach logs.
func redact(u *url.URL) string {
	c := *u
	c.User = nil
	return c.String()
}
