// Package httpapi wires the API's HTTP routes and middleware.
package httpapi

import (
	"net/http"
	"time"

	"github.com/tjonty/collab-tasks/api/internal/health"
)

// healthCheckTimeout bounds each dependency check behind /healthz.
const healthCheckTimeout = 2 * time.Second

// NewRouter returns the API's root handler: the routes wrapped in CORS.
func NewRouter(checks map[string]health.Pinger, corsOrigin string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(checks, healthCheckTimeout))
	return CORS(corsOrigin, mux)
}
