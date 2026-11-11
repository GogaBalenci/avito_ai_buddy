package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Timeout bounds the whole /healthz check.
const Timeout = 2 * time.Second

// Handler runs all probes concurrently and answers with
// {"status":"ok|degraded", "<name>": true|false, ...}: 200 if every probe
// passed, 503 otherwise. For api the shape matches the OpenAPI Health schema.
func Handler(probes map[string]Probe, log *slog.Logger) http.Handler {
	names := make([]string, 0, len(probes))
	for n := range probes {
		names = append(names, n)
	}
	sort.Strings(names)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), Timeout)
		defer cancel()

		results := make(map[string]bool, len(names))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, n := range names {
			wg.Add(1)
			go func(name string, p Probe) {
				defer wg.Done()
				err := p.Check(ctx)
				if err != nil {
					log.Warn("health probe failed", "probe", name, "error", err.Error())
				}
				mu.Lock()
				results[name] = err == nil
				mu.Unlock()
			}(n, probes[n])
		}
		wg.Wait()

		body := map[string]any{"status": "ok"}
		code := http.StatusOK
		for _, n := range names {
			body[n] = results[n]
			if !results[n] {
				body["status"] = "degraded"
				code = http.StatusServiceUnavailable
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	})
}
