package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ok() Pinger {
	return PingerFunc(func(ctx context.Context) error { return nil })
}

func serve(t *testing.T, h http.Handler) (*httptest.ResponseRecorder, map[string]string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

func TestHandler_AllHealthy(t *testing.T) {
	h := Handler(map[string]Pinger{"postgres": ok(), "redis": ok()}, 2*time.Second)

	rec, body := serve(t, h)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	want := map[string]string{"postgres": "ok", "redis": "ok"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
}

func TestHandler_OneDown(t *testing.T) {
	down := PingerFunc(func(ctx context.Context) error { return errors.New("boom") })
	h := Handler(map[string]Pinger{"postgres": ok(), "redis": down}, 2*time.Second)

	rec, body := serve(t, h)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	want := map[string]string{"postgres": "ok", "redis": "down"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("body %q leaks error detail", rec.Body.String())
	}
}

func TestHandler_HungCheckTimesOut(t *testing.T) {
	hung := PingerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h := Handler(map[string]Pinger{"postgres": hung, "redis": ok()}, 50*time.Millisecond)

	start := time.Now()
	rec, body := serve(t, h)
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Errorf("request took %v, want < 1s", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	want := map[string]string{"postgres": "down", "redis": "ok"}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
}
