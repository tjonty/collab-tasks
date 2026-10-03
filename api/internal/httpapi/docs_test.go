package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestRouter_OpenAPIJSON(t *testing.T) {
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodGet, "/openapi.json")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var spec struct {
		OpenAPI    string                     `json:"openapi"`
		Paths      map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas struct {
				TaskPage struct {
					Properties struct {
						NextCursor struct {
							Type []string `json:"type"`
						} `json:"nextCursor"`
					} `json:"properties"`
				} `json:"TaskPage"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("body is not the JSON spec: %v", err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", spec.OpenAPI)
	}
	if _, ok := spec.Paths["/projects/{projectId}/changes"]; !ok {
		t.Errorf("paths has no /projects/{projectId}/changes (%d paths)", len(spec.Paths))
	}
	// The contract's 3.1 features survive: a nullable cursor is a type list.
	if got := spec.Components.Schemas.TaskPage.Properties.NextCursor.Type; !slices.Equal(got, []string{"string", "null"}) {
		t.Errorf("TaskPage.nextCursor type = %q, want [string null]", got)
	}
}

// The served spec is the contract itself, not oapi-codegen's copy of it:
// same paths, schemas and operation ids.
func TestRouter_OpenAPIJSONIsTheContract(t *testing.T) {
	contract, err := openapi3.NewLoader().LoadFromFile("../../../contract/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodGet, "/openapi.json")

	var got, want any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	if d := firstDiff(got, want, ""); d != "" {
		t.Errorf("/openapi.json differs from contract/openapi.yaml at %s", d)
	}
}

// firstDiff returns where two decoded JSON values first differ, or "".
func firstDiff(got, want any, at string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return at + ": not an object"
		}
		keys := make([]string, 0, len(w)+len(g))
		for k := range w {
			keys = append(keys, k)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			if d := firstDiff(g[k], w[k], at+"/"+k); d != "" {
				return d
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: got %v, want %v", at, got, want)
		}
		for i := range w {
			if d := firstDiff(g[i], w[i], fmt.Sprintf("%s/%d", at, i)); d != "" {
				return d
			}
		}
	default:
		if got != want {
			return fmt.Sprintf("%s: got %v, want %v", at, got, want)
		}
	}
	return ""
}

func TestRouter_Docs(t *testing.T) {
	checks, _ := countingChecks()
	rec := do(NewRouter(Config{Checks: checks, CORSOrigin: origin}), http.MethodGet, "/docs")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	body := rec.Body.String()
	for _, want := range []string{"swagger-ui", "/openapi.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't mention %q", want)
		}
	}
}
