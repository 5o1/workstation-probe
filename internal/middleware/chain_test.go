package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecovery_PanicReturns500(t *testing.T) {
	h := Recovery(newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"error":"internal"`) {
		t.Errorf("body = %q, want internal error JSON", rr.Body.String())
	}
}

func TestRequestID_GeneratesAndPropagates(t *testing.T) {
	var seenInCtx string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInCtx = RequestIDFromContext(r.Context())
	}))

	// 1) no incoming id → middleware mints one
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, httptest.NewRequest("GET", "/", nil))
	if seenInCtx == "" {
		t.Errorf("expected generated id on ctx, got empty")
	}
	if got := rr1.Header().Get("X-Request-Id"); got == "" || got != seenInCtx {
		t.Errorf("response header = %q, want %q", got, seenInCtx)
	}

	// 2) incoming id is echoed
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Request-Id", "abc123")
	h.ServeHTTP(rr2, req2)
	if seenInCtx != "abc123" {
		t.Errorf("ctx id = %q, want abc123", seenInCtx)
	}
	if got := rr2.Header().Get("X-Request-Id"); got != "abc123" {
		t.Errorf("response header = %q, want abc123", got)
	}

	// 3) oversized / unsafe incoming ids are replaced, not logged or echoed.
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("X-Request-Id", strings.Repeat("x", maxRequestIDLen+1))
	h.ServeHTTP(rr3, req3)
	if got := rr3.Header().Get("X-Request-Id"); got == req3.Header.Get("X-Request-Id") || got == "" {
		t.Errorf("oversized request id should be replaced, got %q", got)
	}

	rr4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("GET", "/", nil)
	req4.Header.Set("X-Request-Id", "bad/id")
	h.ServeHTTP(rr4, req4)
	if got := rr4.Header().Get("X-Request-Id"); got == "bad/id" || got == "" {
		t.Errorf("unsafe request id should be replaced, got %q", got)
	}
}

func TestAccessLog_DoesNotPanic(t *testing.T) {
	h := AccessLog(newTestLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/foo", nil))
	if rr.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rr.Code)
	}
}

func TestAccessLog_TruncatesLongPath(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	longPath := "/" + strings.Repeat("x", maxLogPathLen+100)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", longPath, nil))

	if strings.Contains(logs.String(), strings.Repeat("x", maxLogPathLen+1)) {
		t.Fatalf("access log contains an unbounded path: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "...(truncated)") {
		t.Fatalf("access log did not mark truncated path: %s", logs.String())
	}
}

func TestResponseWriter_HijackUnsupported(t *testing.T) {
	rw := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	if _, _, err := rw.Hijack(); err == nil {
		t.Errorf("expected error from unsupported hijacker")
	}
	_ = context.Background()
}
