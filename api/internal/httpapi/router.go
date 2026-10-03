// Package httpapi wires the API's HTTP routes and middleware.
package httpapi

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
	"github.com/tjonty/collab-tasks/api/internal/changes"
	"github.com/tjonty/collab-tasks/api/internal/health"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

// healthCheckTimeout bounds each dependency check behind /healthz.
const healthCheckTimeout = 2 * time.Second

// Config is what the router needs.
type Config struct {
	Checks     map[string]health.Pinger
	CORSOrigin string
	Pool       *pgxpool.Pool
	// Doorbell is rung after each accepted change; nil rings nothing.
	Doorbell changes.Doorbell
}

// NewRouter returns the API's root handler: /healthz and the contract's
// operations, wrapped in CORS.
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(cfg.Checks, healthCheckTimeout))

	q := store.New(cfg.Pool)
	srv := &server{q: q, changes: changes.NewService(cfg.Pool, cfg.Doorbell)}
	strict := apigen.NewStrictHandlerWithOptions(srv,
		[]apigen.StrictMiddlewareFunc{identify(q)},
		apigen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  badBody,
			ResponseErrorHandlerFunc: respondError,
		})
	apigen.HandlerWithOptions(strict, apigen.StdHTTPServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: badParam,
	})
	return CORS(cfg.CORSOrigin, mux)
}
