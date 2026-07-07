package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_Disabled_NoOp(t *testing.T) {
	called := false
	h := Middleware(Config{Enabled: false})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	h.ServeHTTP(rr, req)
	if !called {
		t.Errorf("downstream not called")
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS headers when disabled, got %q", got)
	}
}

func TestCORS_AllowedOrigin_HeadersAttached(t *testing.T) {
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://dashboard.example.com"},
		AllowMethods:   []string{"GET", "OPTIONS"},
		AllowHeaders:   []string{"Content-Type"},
		MaxAgeSeconds:  300,
	})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
		t.Errorf("Allow-Methods = %q", got)
	}
	if got := rr.Header().Get("Access-Control-Max-Age"); got != "300" {
		t.Errorf("Max-Age = %q", got)
	}
}

func TestCORS_WildcardMatch(t *testing.T) {
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://*.example.com"},
	})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {}))

	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://app.example.com", true},
		{"https://a.b.example.com", false}, // nested subdomain not allowed
		{"http://app.example.com", false},  // scheme mismatch
		{"https://example.com", false},     // no subdomain
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", c.origin)
		h.ServeHTTP(rr, req)
		got := rr.Header().Get("Access-Control-Allow-Origin")
		if c.ok && got != c.origin {
			t.Errorf("origin %q: expected header, got %q", c.origin, got)
		}
		if !c.ok && got != "" {
			t.Errorf("origin %q: expected no header, got %q", c.origin, got)
		}
	}
}

func TestCORS_DisallowedOrigin_NoHeader(t *testing.T) {
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://allowed.example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no header for disallowed origin, got %q", got)
	}
}

func TestCORS_DisallowedPreflight_403(t *testing.T) {
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://allowed.example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not be called on denied preflight")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/metrics", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("preflight status = %d, want 403", rr.Code)
	}
}

func TestCORS_AllowedPreflight_204(t *testing.T) {
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://allowed.example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("downstream should not be called on preflight")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/metrics", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rr.Code)
	}
}

func TestCORS_NoOriginHeader_Passthrough(t *testing.T) {
	called := false
	h := Middleware(Config{
		Enabled:        true,
		AllowedOrigins: []string{"https://allowed.example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil) // no Origin
	h.ServeHTTP(rr, req)

	if !called {
		t.Errorf("downstream not called")
	}
}
