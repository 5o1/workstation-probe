package cors

import (
	"net/http"
	"strconv"
	"strings"
)

// Config drives the CORS middleware. A zero value is a valid no-op.
type Config struct {
	Enabled        bool
	AllowedOrigins []string
	AllowMethods   []string
	AllowHeaders   []string
	MaxAgeSeconds  int
}

// Middleware returns the CORS handler. When cfg.Enabled is false the
// returned handler is a no-op, so callers can unconditionally wrap mux.
//
// Origin entries are validated through Parse, so config-time and
// request-time parsing share the same grammar.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if !cfg.Enabled || len(cfg.AllowedOrigins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	patterns := make([]Pattern, 0, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		p, err := Parse(o)
		if err != nil {
			// config validation should have caught this; fail safe by
			// skipping the entry and letting the recovery layer log the
			// eventual bad response if anything else goes wrong.
			continue
		}
		patterns = append(patterns, p)
	}
	if len(patterns) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	methods := strings.Join(cfg.AllowMethods, ", ")
	headers := strings.Join(cfg.AllowHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAgeSeconds)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// same-origin / curl / no browser → no CORS headers needed
				next.ServeHTTP(w, r)
				return
			}
			matched := false
			for _, p := range patterns {
				if p.Match(origin) {
					matched = true
					break
				}
			}
			if !matched {
				// browser will block anyway; just don't attach headers.
				// For preflight we explicitly 403 so the caller sees the
				// denial in their logs.
				if r.Method == http.MethodOptions {
					http.Error(w, `{"error":"cors_origin_not_allowed"}`, http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", headers)
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Max-Age", maxAge)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
