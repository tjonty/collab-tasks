// Package health reports whether the API's dependencies are reachable.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger checks that a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingerFunc adapts a plain function to the Pinger interface.
type PingerFunc func(ctx context.Context) error

// Ping calls f(ctx).
func (f PingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// Handler runs every check, each under its own timeout, and responds with
// {"<name>":"ok"|"down",...}. Status is 200 when every check is ok, 503
// otherwise. Error details are logged, never returned to the client.
func Handler(checks map[string]Pinger, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := make(map[string]string, len(checks))
		status := http.StatusOK
		for name, p := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			err := p.Ping(ctx)
			cancel()
			if err != nil {
				slog.Error("health check failed", "check", name, "err", err)
				result[name] = "down"
				status = http.StatusServiceUnavailable
				continue
			}
			result[name] = "ok"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			slog.Error("write health response", "err", err)
		}
	})
}
