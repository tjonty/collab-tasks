// Package httpapi wires the API's HTTP routes and middleware.
package httpapi

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/gzhttp"

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
	// Realtime serves GET /ws; nil leaves it unrouted.
	Realtime http.Handler
}

// NewRouter returns the API's root handler: /healthz, the contract (as
// /openapi.json, and browsable at /docs), its operations and the /ws
// endpoint, wrapped in CORS. Responses of 1 KB or more are gzipped for
// clients that accept it, which shrinks a page of task summaries several
// times over.
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler(cfg.Checks, healthCheckTimeout))
	// Public, like /healthz: no X-User-Id needed to read the contract.
	mux.Handle("GET /openapi.json", openAPIHandler())
	mux.Handle("GET /docs", docsHandler())

	q := store.New(cfg.Pool)
	srv := &server{pool: cfg.Pool, q: q, changes: changes.NewService(cfg.Pool, cfg.Doorbell), doorbell: cfg.Doorbell}
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
	root := http.NewServeMux()
	if cfg.Realtime != nil {
		// Outside gzip: an upgrade must reach the handler unwrapped.
		root.Handle("GET /ws", cfg.Realtime)
	}
	root.Handle("/", gzhttp.GzipHandler(mux))
	return CORS(cfg.CORSOrigin, root)
}
