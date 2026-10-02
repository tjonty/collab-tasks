package httpapi

import "net/http"

// CORS allows allowedOrigin to call the API from a browser. Every response
// carries the allow-origin header, and preflight OPTIONS requests are
// answered here with 204 without reaching next.
func CORS(allowedOrigin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", allowedOrigin)
		h.Add("Vary", "Origin")

		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
