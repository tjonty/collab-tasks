package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tjonty/collab-tasks/api/internal/apigen"
)

// openAPIHandler serves the contract (contract/openapi.yaml, embedded in
// apigen when the code is generated) as JSON, for tools and for /docs.
func openAPIHandler() http.Handler {
	spec := sync.OnceValues(func() ([]byte, error) {
		s, err := apigen.GetSwagger()
		if err != nil {
			return nil, err
		}
		// oapi-codegen capitalizes operation ids to name its Go methods, in
		// the copy it embeds too. Put back the contract's camelCase.
		for _, item := range s.Paths.Map() {
			for _, op := range item.Operations() {
				op.OperationID = lowerFirst(op.OperationID)
			}
		}
		return json.Marshal(s)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := spec()
		if err != nil {
			writeError(w, http.StatusInternalServerError, apigen.Error{Code: apigen.ErrorCodeInternal, Message: "the embedded OpenAPI spec doesn't load"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// lowerFirst lowercases an id's first letter: SubmitChange → submitChange.
func lowerFirst(id string) string {
	if id == "" {
		return id
	}
	return strings.ToLower(id[:1]) + id[1:]
}

// docsPage is Swagger UI over /openapi.json. Its files come from a CDN, so
// the API ships no front-end assets.
const docsPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Collab Tasks API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/openapi.json", dom_id: "#swagger-ui" });
  </script>
</body>
</html>
`

// docsHandler serves a page to browse the contract: every operation, its
// parameters and schemas.
func docsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, docsPage)
	})
}
