// Package admin holds the mock flags (mock_admin.flags) and the demo
// endpoints /internal/demo/{reset,flags}. They exist only with
// DEMO_TOOLS_ENABLED=true and are not part of any corporate contract (12 §7.2).
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Mock flags and their defaults (white list, 10 §11.6, 11 §0.3).
const (
	FlagAbsencesFault  = "mock_absences_fault"
	FlagMentorApproval = "access_mentor_approval"
)

// Defaults lists the allowed values and default of each mock flag.
var Defaults = []struct {
	Name    string
	Value   string
	Allowed []string
}{
	{FlagAbsencesFault, "off", []string{"off", "unavailable", "slow"}},
	{FlagMentorApproval, "on", []string{"on", "off"}},
}

// Flags reads and writes mock flags.
type Flags struct{ Pool *pgxpool.Pool }

// Querier is *pgxpool.Pool or pgx.Tx.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Get returns the flag value; on error the default (fail safe).
func (f *Flags) Get(ctx context.Context, name string) string { return GetIn(ctx, f.Pool, name) }

// GetIn reads a flag through q — inside a transaction use the tx, so a busy
// pool cannot deadlock.
func GetIn(ctx context.Context, q Querier, name string) string {
	var v string
	if err := q.QueryRow(ctx, `SELECT value FROM mock_admin.flags WHERE name = $1`, name).Scan(&v); err != nil {
		for _, d := range Defaults {
			if d.Name == name {
				return d.Value
			}
		}
	}
	return v
}

// ResetTx writes defaults inside tx.
func ResetTx(ctx context.Context, tx pgx.Tx) error {
	for _, d := range Defaults {
		if _, err := tx.Exec(ctx, `INSERT INTO mock_admin.flags (name, value, allowed_values) VALUES ($1, $2, $3)
			ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, allowed_values = EXCLUDED.allowed_values`,
			d.Name, d.Value, d.Allowed); err != nil {
			return err
		}
	}
	return nil
}

// Flag is the JSON of a flag (as the API's DemoFlag).
type Flag struct {
	Name          string   `json:"name"`
	Value         string   `json:"value"`
	AllowedValues []string `json:"allowed_values"`
}

// List returns all flags.
func (f *Flags) List(ctx context.Context) ([]Flag, error) {
	rows, err := f.Pool.Query(ctx, `SELECT name, value, allowed_values FROM mock_admin.flags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var fl Flag
		if err := rows.Scan(&fl.Name, &fl.Value, &fl.AllowedValues); err != nil {
			return nil, err
		}
		out = append(out, fl)
	}
	return out, rows.Err()
}

// ErrBadFlag is a flag or value outside the white list.
var ErrBadFlag = errors.New("flag or value is not allowed")

// Set changes a flag of the white list.
func (f *Flags) Set(ctx context.Context, name, value string) (Flag, error) {
	for _, d := range Defaults {
		if d.Name != name {
			continue
		}
		for _, a := range d.Allowed {
			if a == value {
				_, err := f.Pool.Exec(ctx, `UPDATE mock_admin.flags SET value = $2 WHERE name = $1`, name, value)
				return Flag{Name: name, Value: value, AllowedValues: d.Allowed}, err
			}
		}
	}
	return Flag{}, ErrBadFlag
}

// Handler serves /internal/demo/reset and /internal/demo/flags.
func Handler(f *Flags, reset func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/demo/reset", func(w http.ResponseWriter, r *http.Request) {
		if err := reset(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "UNAVAILABLE", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /internal/demo/flags", func(w http.ResponseWriter, r *http.Request) {
		items, err := f.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "UNAVAILABLE", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /internal/demo/flags", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, Value string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"code": "BAD_REQUEST", "message": "bad body"})
			return
		}
		fl, err := f.Set(r.Context(), body.Name, body.Value)
		if errors.Is(err, ErrBadFlag) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"code": "BAD_REQUEST", "message": err.Error()})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "UNAVAILABLE", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, fl)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
