package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tjonty/collab-tasks/api/internal/health"
)

const origin = "http://localhost:3000"

// countingChecks returns two healthy pingers and a counter of how often
// either was called.
func countingChecks() (map[string]health.Pinger, *atomic.Int32) {
	var calls atomic.Int32
	p := health.PingerFunc(func(ctx context.Context) error {
		calls.Add(1)
		return nil
	})
	return map[string]health.Pinger{"postgres": p, "redis": p}, &calls
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouter_HealthzOK(t *testing.T) {
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := rec.Header().Values("Vary"); !containsToken(got, "Origin") {
		t.Errorf("Vary = %q, want it to contain Origin", got)
	}
}

func TestRouter_Preflight(t *testing.T) {
	checks, calls := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodOptions, "/healthz")

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got, want := rec.Header().Get("Access-Control-Allow-Methods"), "GET, POST, PATCH, DELETE, OPTIONS"; got != want {
		t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Access-Control-Allow-Headers"), "Content-Type, X-User-Id"; got != want {
		t.Errorf("Access-Control-Allow-Headers = %q, want %q", got, want)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("pingers called %d times, want 0", n)
	}
}

func TestRouter_WrongMethod(t *testing.T) {
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodPost, "/healthz")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestRouter_UnknownPath(t *testing.T) {
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodGet, "/nope")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func containsToken(values []string, token string) bool {
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
