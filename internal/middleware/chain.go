// Package middleware contains the HTTP middlewares wrapped around the mux.
//
// The wrap order in main is fixed by the plan (see internal/metrics or docs):
//
//	recovery → request-id → access-log → cors → ratelimit → mux
//
// Each middleware exposes a Constructor that returns a HandlerFunc, so they
// can be composed without a generic framework.
package middleware

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"
)

// responseWriter wraps http.ResponseWriter so middlewares can observe the
// status code and bytes written without breaking http.Flusher / Hijacker.
type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush propagates to the underlying ResponseWriter when supported.
func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack propagates to the underlying ResponseWriter when supported.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijacker not supported")
}

// Recovery converts panics in downstream handlers into 500 responses with a
// JSON body and logs the stack trace at error level.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler",
						"err", fmt.Sprintf("%v", rec),
						"path", logPath(r.URL.Path),
						"method", r.Method,
						"stack", string(debug.Stack()),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID injects or propagates the X-Request-Id header so log lines from
// the same request can be correlated. The value is also stashed on the
// request context for downstream consumers.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
)

const maxRequestIDLen = 128
const maxLogPathLen = 512

// RequestIDFromContext returns the request id stashed by the middleware, or
// the empty string if none was set.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// RequestID is the public constructor alias. The actual implementation
// lives in requestID; this wrapper exists so callers don't need to know the
// internal name.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !validRequestID(id) {
				id = newRequestID()
			}
			w.Header().Set("X-Request-Id", id)
			ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ':':
		default:
			return false
		}
	}
	return true
}

// AccessLog writes one structured log line per request once the response has
// been written. Latency is measured around the inner handler.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("http",
				"method", r.Method,
				"path", logPath(r.URL.Path),
				"status", rw.status,
				"bytes", rw.bytes,
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
				"remote", clientIP(r),
			)
		})
	}
}

func logPath(path string) string {
	if len(path) <= maxLogPathLen {
		return path
	}
	return path[:maxLogPathLen] + "...(truncated)"
}

// clientIP extracts the best-effort client IP from RemoteAddr.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// newRequestID returns a short random hex identifier; we don't need crypto-
// grade uniqueness, just enough to correlate log lines.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read essentially never fails on Linux, but fall back to a
		// timestamp so we never hand out an empty id.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i % 8 * 8))
		}
	}
	return hex.EncodeToString(b[:])
}
