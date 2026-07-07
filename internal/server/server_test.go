package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/assaneko/workstation-probe/internal/config"
)

func TestBuild_AccessLogIncludesRequestID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := Build(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), &config.Config{}, logger)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Request-Id", "test-request-id")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(logs.String(), `"request_id":"test-request-id"`) {
		t.Fatalf("access log did not include request id: %s", logs.String())
	}
}

func TestAddr_FormatsIPv4AndIPv6(t *testing.T) {
	if got := Addr("127.0.0.1", 19090); got != "127.0.0.1:19090" {
		t.Fatalf("IPv4 addr = %q", got)
	}
	if got := Addr("::1", 19090); got != "[::1]:19090" {
		t.Fatalf("IPv6 addr = %q", got)
	}
}
