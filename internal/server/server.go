// Package server assembles the HTTP listener: middleware chain, mux, and
// graceful-shutdown wiring. Concrete route handlers live under
// internal/server/handlers and the per-module packages.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/assaneko/workstation-probe/internal/config"
	"github.com/assaneko/workstation-probe/internal/cors"
	"github.com/assaneko/workstation-probe/internal/middleware"
)

// Addr formats a validated TCP listen address from the configured host/port.
func Addr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Build wraps mux with the standard middleware chain defined in the plan:
//
//	recovery → request-id → access-log → cors → ratelimit → mux
//
// Wrap order is innermost-last, so the runtime execution order is left to
// right. CORS therefore runs before the rate limiter, which means preflight
// OPTIONS requests are short-circuited at CORS and never reach the token
// bucket. Each middleware is a no-op when its config is disabled.
//
// ctx is used by long-lived middleware (rate-limit cleanup) to exit their
// background goroutines on shutdown. Pass the root context that is cancelled
// when the process should stop.
func Build(ctx context.Context, mux http.Handler, cfg *config.Config, logger *slog.Logger) http.Handler {
	h := mux
	h = middleware.RateLimit(ctx, middleware.RateLimitConfig{
		Enabled:           cfg.Security.RateLimit.Enabled,
		RequestsPerSecond: cfg.Security.RateLimit.RequestsPerSecond,
		Burst:             cfg.Security.RateLimit.Burst,
		TrustProxyHeaders: cfg.Security.RateLimit.TrustProxyHeaders,
		ExemptPaths:       cfg.Security.RateLimit.ExemptPaths,
	}, logger)(h)
	h = cors.Middleware(cors.Config{
		Enabled:        cfg.Security.CORS.Enabled,
		AllowedOrigins: cfg.Security.CORS.AllowedOrigins,
		AllowMethods:   cfg.Security.CORS.AllowMethods,
		AllowHeaders:   cfg.Security.CORS.AllowHeaders,
		MaxAgeSeconds:  cfg.Security.CORS.MaxAgeSeconds,
	})(h)
	h = middleware.AccessLog(logger)(h)
	h = middleware.RequestID()(h)
	h = middleware.Recovery(logger)(h)
	return h
}
