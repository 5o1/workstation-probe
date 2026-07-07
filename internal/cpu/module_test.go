package cpu

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// fakeCollector returns canned samples and records warmup calls.
type fakeCollector struct {
	mu        sync.Mutex
	warmupAt  time.Time
	warmupDur time.Duration
	seq       []Sample
	idx       int
}

func (f *fakeCollector) Warmup(d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warmupDur = d
	f.warmupAt = time.Now()
	return nil
}

func (f *fakeCollector) Collect() Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seq) == 0 {
		return Sample{Timestamp: time.Now().UTC()}
	}
	idx := f.idx
	if idx >= len(f.seq) {
		idx = len(f.seq) - 1 // repeat last sample rather than going silent
	}
	f.idx++
	s := f.seq[idx]
	return s
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }

func TestModule_StartsAndPublishesLatest(t *testing.T) {
	fc := &fakeCollector{seq: []Sample{
		{Timestamp: time.Now().UTC(), OverallPercent: 12.3, PerCorePercent: []float64{10, 20}, CoreCount: 2},
		{Timestamp: time.Now().UTC(), OverallPercent: 45.6, PerCorePercent: []float64{40, 50}, CoreCount: 2},
	}}
	m, err := NewWithCollector(fc, 50*time.Millisecond, 4, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}

	if fc.warmupDur != 50*time.Millisecond {
		t.Errorf("warmup interval = %s, want 50ms", fc.warmupDur)
	}

	if !m.Enabled() || m.Name() != "cpu" {
		t.Errorf("module flags wrong: enabled=%v name=%q", m.Enabled(), m.Name())
	}
	if p := m.Latest(); p == nil {
		t.Errorf("expected latest after construction")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond) // a couple of ticks

	p := m.Latest().(*Sample)
	if p.OverallPercent < 0 {
		t.Errorf("expected sequential samples, latest overall = %f", p.OverallPercent)
	}

	hist := m.History(5 * time.Second)
	if len(hist) == 0 {
		t.Errorf("expected non-empty history")
	}
}

func TestModule_HandleLatest503BeforeFirstSample(t *testing.T) {
	m := &Module{
		base:   nil, // not used; Latest() returns nil
		logger: newTestLogger(),
	}
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics/cpu", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not_ready") {
		t.Errorf("body = %q, want not_ready", rr.Body.String())
	}
}

func TestParseDurationParam(t *testing.T) {
	cases := []struct {
		in   string
		def  time.Duration
		want time.Duration
		ok   bool
	}{
		{"", 30 * time.Second, 30 * time.Second, true},
		{"5s", 30 * time.Second, 5 * time.Second, true},
		{"abc", 30 * time.Second, 0, false},
		{"-1s", 30 * time.Second, 0, false},
	}
	for _, c := range cases {
		got, ok := metrics.ParseDurationParam(c.in, c.def)
		if ok != c.ok || got != c.want {
			t.Errorf("metrics.ParseDurationParam(%q, %s) = (%s, %v), want (%s, %v)", c.in, c.def, got, ok, c.want, c.ok)
		}
	}
}

func TestParseLimitParam(t *testing.T) {
	if got := metrics.ParseLimitParam(""); got != 0 {
		t.Errorf("empty limit = %d, want 0", got)
	}
	if got := metrics.ParseLimitParam("42"); got != 42 {
		t.Errorf("limit 42 = %d, want 42", got)
	}
	if got := metrics.ParseLimitParam("abc"); got != 0 {
		t.Errorf("invalid limit = %d, want 0", got)
	}
}

func TestSampleJSON(t *testing.T) {
	s := Sample{Timestamp: time.Unix(0, 0).UTC(), OverallPercent: 5.5, CoreCount: 4}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"overall_percent":5.5`) {
		t.Errorf("missing overall_percent in %s", b)
	}
	if strings.Contains(string(b), `"error":`) {
		t.Errorf("unexpected error field in %s", b)
	}
}

func TestModule_Peak(t *testing.T) {
	// Sequence of three samples; stress fields go up then down so the
	// peak must come from sample 2. PerCorePercent is 2-wide and the
	// peaks per core are different (core 0 peaks at sample 2, core 1
	// peaks at sample 3) — both should be in the result.
	fc := &fakeCollector{seq: []Sample{
		{Timestamp: time.Now().UTC(), OverallPercent: 30, PerCorePercent: []float64{20, 30}, CoreCount: 2},
		{Timestamp: time.Now().UTC(), OverallPercent: 80, PerCorePercent: []float64{70, 50}, CoreCount: 2},
		{Timestamp: time.Now().UTC(), OverallPercent: 40, PerCorePercent: []float64{50, 60}, CoreCount: 2},
	}}
	m, err := NewWithCollector(fc, 30*time.Millisecond, 8, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)

	peak := m.Peak(5 * time.Second)
	if peak == nil {
		t.Fatal("expected non-nil peak")
	}
	p := peak.(*Sample)
	if p.OverallPercent != 80 {
		t.Errorf("peak overall = %f, want 80", p.OverallPercent)
	}
	if len(p.PerCorePercent) != 2 {
		t.Fatalf("per-core length = %d, want 2", len(p.PerCorePercent))
	}
	if p.PerCorePercent[0] != 70 {
		t.Errorf("peak core0 = %f, want 70", p.PerCorePercent[0])
	}
	if p.PerCorePercent[1] != 60 {
		t.Errorf("peak core1 = %f, want 60", p.PerCorePercent[1])
	}
	if p.CoreCount != 2 {
		t.Errorf("core count = %d, want 2 (from latest in window)", p.CoreCount)
	}
}
