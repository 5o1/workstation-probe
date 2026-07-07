package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRateLimit_Disabled_NoOp(t *testing.T) {
	called := 0
	h := RateLimit(context.Background(), RateLimitConfig{Enabled: false}, newQuietLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }),
	)
	for i := 0; i < 100; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:5555"
		h.ServeHTTP(rr, req)
	}
	if called != 100 {
		t.Errorf("expected all requests through, got %d", called)
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	cfg := RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             2,
		ExemptPaths:       nil,
	}
	h := RateLimit(context.Background(), cfg, newQuietLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	)
	statuses := make([]int, 10)
	for i := range statuses {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:5555"
		h.ServeHTTP(rr, req)
		statuses[i] = rr.Code
	}
	// expect first 2 OK, rest 429
	if statuses[0] != 200 || statuses[1] != 200 {
		t.Errorf("first two should pass, got %v", statuses[:2])
	}
	has429 := false
	for _, s := range statuses[2:] {
		if s == http.StatusTooManyRequests {
			has429 = true
		}
	}
	if !has429 {
		t.Errorf("expected 429 in remaining statuses, got %v", statuses[2:])
	}
}

func TestRateLimit_ExemptPath(t *testing.T) {
	cfg := RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 0.5,
		Burst:             1,
		ExemptPaths:       []string{"/health"},
	}
	h := RateLimit(context.Background(), cfg, newQuietLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	for i := 0; i < 20; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/health", nil)
		req.RemoteAddr = "1.2.3.4:5555"
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("/health call %d got %d, want 200", i, rr.Code)
		}
	}
}

func TestRateLimit_ClientIPFromRemoteAddr(t *testing.T) {
	cfg := RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             1,
	}
	h := RateLimit(context.Background(), cfg, newQuietLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	)
	// Two different IPs should each get their own bucket
	for i, ip := range []string{"1.1.1.1:1111", "2.2.2.2:2222"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Errorf("ip %d: code = %d, want 200", i, rr.Code)
		}
	}
}

func TestRateLimit_RetryAfterHeader(t *testing.T) {
	cfg := RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 0.1, // 1 every 10s
		Burst:             1,
	}
	h := RateLimit(context.Background(), cfg, newQuietLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, reqFromIP("1.2.3.4"))
	if rr1.Code != 200 {
		t.Fatalf("first call code = %d", rr1.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, reqFromIP("1.2.3.4"))
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second call code = %d, want 429", rr2.Code)
	}
	if rr2.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429")
	}
}

func reqFromIP(ip string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = ip + ":1234"
	return r
}
