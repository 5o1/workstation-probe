package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig drives the per-IP token-bucket middleware.
type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond float64
	Burst             int
	TrustProxyHeaders bool
	ExemptPaths       []string
	IdleEvictAfter    time.Duration // default 10m
	CleanupInterval   time.Duration // default 1m
}

// ipEntry holds one limiter and its last-seen timestamp.
type ipEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// RateLimit returns a per-IP token-bucket middleware. The returned handler
// is safe for concurrent use; the background cleanup goroutine exits when
// ctx is cancelled.
//
// When cfg.Enabled is false the middleware is a no-op.
func RateLimit(ctx context.Context, cfg RateLimitConfig, logger *slog.Logger) func(http.Handler) http.Handler {
	if !cfg.Enabled || cfg.RequestsPerSecond <= 0 || cfg.Burst <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if cfg.IdleEvictAfter <= 0 {
		cfg.IdleEvictAfter = 10 * time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = time.Minute
	}
	exemptSet := make(map[string]struct{}, len(cfg.ExemptPaths))
	for _, p := range cfg.ExemptPaths {
		exemptSet[p] = struct{}{}
	}

	rl := &rateLimiter{
		rps:    rate.Limit(cfg.RequestsPerSecond),
		burst:  cfg.Burst,
		trust:  cfg.TrustProxyHeaders,
		exempt: exemptSet,
		byIP:   make(map[string]*ipEntry),
		logger: logger,
	}
	go rl.cleanupLoop(ctx, cfg.CleanupInterval, cfg.IdleEvictAfter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := rl.exempt[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			ip := rl.clientIP(r)
			lim := rl.limiterFor(ip)
			// Use Reserve() so we can return a precise Retry-After. We
			// cancel immediately to give the token back; if the caller is
			// denied the caller was never charged.
			rsv := lim.Reserve()
			if !rsv.OK() {
				rl.deny(w)
				return
			}
			delay := rsv.Delay()
			if delay > 0 {
				rsv.Cancel()
				w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
				rl.deny(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type rateLimiter struct {
	rps    rate.Limit
	burst  int
	trust  bool
	exempt map[string]struct{}

	mu   sync.Mutex
	byIP map[string]*ipEntry

	logger *slog.Logger
}

func (rl *rateLimiter) limiterFor(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if e, ok := rl.byIP[ip]; ok {
		e.lastSeen = time.Now()
		return e.lim
	}
	lim := rate.NewLimiter(rl.rps, rl.burst)
	rl.byIP[ip] = &ipEntry{lim: lim, lastSeen: time.Now()}
	return lim
}

func (rl *rateLimiter) deny(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	//nolint:errcheck // status + headers already on the wire; nothing useful to do with an encoder error.
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
}

func (rl *rateLimiter) cleanupLoop(ctx context.Context, interval, idle time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cutoff := time.Now().Add(-idle)
			rl.mu.Lock()
			for ip, e := range rl.byIP {
				if e.lastSeen.Before(cutoff) {
					delete(rl.byIP, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *rateLimiter) clientIP(r *http.Request) string {
	if rl.trust {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// first entry is the originating client
			if comma := strings.IndexByte(xff, ','); comma >= 0 {
				xff = xff[:comma]
			}
			xff = strings.TrimSpace(xff)
			if xff != "" {
				return xff
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
