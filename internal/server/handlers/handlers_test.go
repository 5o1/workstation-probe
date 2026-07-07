package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/assaneko/workstation-probe/internal/cpu"
	"github.com/assaneko/workstation-probe/internal/memory"
	"github.com/assaneko/workstation-probe/internal/metrics"
	"github.com/assaneko/workstation-probe/internal/storage"
)

// fakeModule is the minimum stub that satisfies metrics.Module for these
// tests. We only exercise fields the JSON contract requires.
type fakeModule struct {
	name        string
	enabled     bool
	reason      string
	latest      any
	lastAge     time.Duration
	profile     any
	histSamples []any
}

func (m *fakeModule) Name() string                     { return m.name }
func (m *fakeModule) Enabled() bool                    { return m.enabled }
func (m *fakeModule) DisabledReason() string           { return m.reason }
func (m *fakeModule) Start(_ context.Context) error    { return nil }
func (m *fakeModule) Shutdown(_ context.Context) error { return nil }
func (m *fakeModule) Latest() any                      { return m.latest }
func (m *fakeModule) History(_ time.Duration) []any {
	return m.histSamples
}
func (m *fakeModule) LastSampleAge() time.Duration    { return m.lastAge }
func (m *fakeModule) Profile() any                    { return m.profile }
func (m *fakeModule) RegisterRoutes(_ *http.ServeMux) {}

// (the fake above is intentionally minimal; tests below only use the
// fields the JSON handlers actually inspect.)

func TestCombinedHandler_SetsCacheControlNoStore(t *testing.T) {
	// The combined handler type-asserts each module's Latest() to a concrete
	// sample type, so the fakes must return real types to make it through
	// assignSample into the JSON output.
	oldNowLocal := nowLocal
	nowLocal = func() time.Time {
		return time.Date(2026, 7, 8, 1, 2, 3, 0, time.FixedZone("CST", 8*60*60))
	}
	t.Cleanup(func() {
		nowLocal = oldNowLocal
	})

	cpuMod := &fakeModule{name: "cpu", enabled: true, latest: &cpu.Sample{OverallPercent: 12.3}}
	memMod := &fakeModule{name: "memory", enabled: true, latest: (*memory.Sample)(nil)}
	stoMod := &fakeModule{name: "storage", enabled: true, latest: (*storage.Sample)(nil)}
	h := NewCombined([]metrics.Module{cpuMod, memMod, stoMod})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (raw=%q)", err, rr.Body.String())
	}
	if got := body["server_time_local"]; got != "2026-07-08 01:02:03" {
		t.Errorf("server_time_local = %v, want fixed local time", got)
	}
	if got := body["server_time_unix_seconds"]; got != float64(1783443723) {
		t.Errorf("server_time_unix_seconds = %v, want 1783443723", got)
	}
	for _, key := range []string{"hostname", "server_timezone", "server_timezone_offset_seconds", "host_uptime_seconds"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s should live in /profile, not /metrics: %v", key, body)
		}
	}
	if _, ok := body["cpu"]; !ok {
		t.Errorf("missing cpu: %v", body)
	}
	// memory and storage fake modules returned nil Latest(); they must be
	// omitted from the merged JSON (omitempty), not serialized as null.
	if _, ok := body["memory"]; ok {
		t.Errorf("memory should be omitted when latest is nil: %v", body)
	}
	if _, ok := body["storage"]; ok {
		t.Errorf("storage should be omitted when latest is nil: %v", body)
	}
}

func TestProfileHandler_ExposesStaticHostFacts(t *testing.T) {
	oldNowLocal := nowLocal
	oldHostBootTime := hostBootTime
	nowLocal = func() time.Time {
		return time.Date(2026, 7, 8, 1, 2, 3, 0, time.FixedZone("CST", 8*60*60))
	}
	hostBootTime = func() (uint64, error) { return 1783350000, nil }
	t.Cleanup(func() {
		nowLocal = oldNowLocal
		hostBootTime = oldHostBootTime
	})

	h := NewProfile("test-host", ProfileSamplerConfig{IntervalMs: 1000, HistoryCapacity: 60}, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/profile", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got := body["hostname"]; got != "test-host" {
		t.Errorf("hostname = %v, want test-host", got)
	}
	if got := body["server_timezone"]; got != "CST" {
		t.Errorf("server_timezone = %v, want CST", got)
	}
	if got := body["server_timezone_offset_seconds"]; got != float64(8*60*60) {
		t.Errorf("server_timezone_offset_seconds = %v, want 28800", got)
	}
	if got := body["host_boot_time_unix_seconds"]; got != float64(1783350000) {
		t.Errorf("host_boot_time_unix_seconds = %v, want 1783350000", got)
	}
	if got := body["host_boot_time_local"]; got != "2026-07-06 23:00:00" {
		t.Errorf("host_boot_time_local = %v, want fixed local boot time", got)
	}
}

func TestCombinedHandler_InvalidPeakWindow(t *testing.T) {
	h := NewCombined(nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics?mode=peak&window=banana", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid_duration") {
		t.Errorf("body = %q, want invalid_duration", rr.Body.String())
	}
}

func TestHealthHandler_OKStatus(t *testing.T) {
	cpu := &fakeModule{name: "cpu", enabled: true, lastAge: 100 * time.Millisecond}
	h := NewHealth([]metrics.Module{cpu}, 1*time.Second)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	// /health intentionally does NOT set Cache-Control: no-store (clients
	// may want to cache the "ok" verdict for a short time).
	if got := rr.Header().Get("Cache-Control"); got == "no-store" {
		t.Errorf("Cache-Control = %q, want unset on /health", got)
	}
	var body HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
}

func TestHealthHandler_DegradedStatus(t *testing.T) {
	cpu := &fakeModule{name: "cpu", enabled: true, lastAge: 10 * time.Second}
	h := NewHealth([]metrics.Module{cpu}, 1*time.Second)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}
