// Package httpx holds shared HTTP helpers: JSON responses, error envelopes, and
// list-query parsing. These mirror the telemetry backend's writeJSON /
// {"error": ...} conventions so both services feel identical to callers.
package httpx

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes a standard error envelope: {"error": "<msg>"}.
func Error(w http.ResponseWriter, code int, msg string) {
	JSON(w, code, map[string]string{"error": msg})
}

// DecodeJSON reads and unmarshals a JSON request body into dst.
func DecodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// List is the standard list-endpoint response envelope.
type List[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

// ListParams captures the common list query params (?limit=&offset=&q=&status=).
type ListParams struct {
	Limit  int
	Offset int
	Q      string
	Status string
}

const (
	defaultLimit = 50
	maxLimit     = 500
)

// CORS returns middleware that allows browser requests from the given origins.
// An empty allowlist reflects any Origin (local dev). With a non-empty list only
// matching origins are allowed. Credentials are permitted so cookie-based auth
// works; preflight OPTIONS requests are answered directly.
func CORS(origins []string) func(http.Handler) http.Handler {
	allow := make(map[string]bool, len(origins))
	for _, o := range origins {
		allow[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (len(allow) == 0 || allow[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ParseList extracts pagination + search params from the request query string,
// clamping limit to a safe range.
func ParseList(r *http.Request) ListParams {
	q := r.URL.Query()
	p := ListParams{
		Limit:  defaultLimit,
		Offset: 0,
		Q:      strings.TrimSpace(q.Get("q")),
		Status: strings.TrimSpace(q.Get("status")),
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		p.Limit = v
	}
	if p.Limit > maxLimit {
		p.Limit = maxLimit
	}
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v >= 0 {
		p.Offset = v
	}
	return p
}
