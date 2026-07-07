package gpu

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeCollector struct {
	ok        bool
	reason    string
	samples   []Sample
	idx       int
	shutdownN int
}

func (f *fakeCollector) Init() (bool, string) { return f.ok, f.reason }
func (f *fakeCollector) Shutdown()            { f.shutdownN++ }
func (f *fakeCollector) Profile() ProfileInfo {
	return ProfileInfo{DisabledReason: f.reason, DeviceCount: len(f.samples)}
}
func (f *fakeCollector) Collect() Sample {
	if f.idx < len(f.samples) {
		s := f.samples[f.idx]
		f.idx++
		return s
	}
	if len(f.samples) == 0 {
		return Sample{Timestamp: time.Now().UTC()}
	}
	return f.samples[len(f.samples)-1]
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGPU_DisabledWhenCollectorUnavailable(t *testing.T) {
	fc := &fakeCollector{ok: false, reason: "no nvml"}
	m := NewWithCollector(fc, 50*time.Millisecond, 4, newTestLogger())
	if m.Enabled() {
		t.Errorf("expected disabled when Init fails")
	}
	if m.Profile().(ProfileInfo).DisabledReason == "" {
		t.Errorf("expected disabled_reason in profile")
	}

	// latest endpoint must 404
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics/gpu", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "gpu_disabled") {
		t.Errorf("body = %q, want gpu_disabled", rr.Body.String())
	}

	// Shutdown should still be safe even though Init never succeeded
	if err := m.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestGPU_EnabledProducesSamples(t *testing.T) {
	now := time.Now().UTC()
	fc := &fakeCollector{
		ok: true,
		samples: []Sample{
			{Timestamp: now, Devices: []Device{{Index: 0, Name: "gpu0", UtilizationGPUPercent: 25}}},
			{Timestamp: now.Add(time.Second), Devices: []Device{{Index: 0, Name: "gpu0", UtilizationGPUPercent: 50}}},
		},
	}
	m := NewWithCollector(fc, 30*time.Millisecond, 4, newTestLogger())
	if !m.Enabled() {
		t.Errorf("expected enabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)

	s := m.Latest().(*Sample)
	if len(s.Devices) != 1 || s.Devices[0].Name != "gpu0" {
		t.Errorf("unexpected sample: %+v", s)
	}

	hist := m.History(time.Minute)
	if len(hist) == 0 {
		t.Errorf("expected history")
	}

	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.shutdownN != 1 {
		t.Errorf("shutdown called %d times, want 1", fc.shutdownN)
	}
	// second call must be a no-op
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.shutdownN != 1 {
		t.Errorf("second shutdown called it %d times, want 1 (sync.Once)", fc.shutdownN)
	}
}

func TestGPU_PeakPerDevice(t *testing.T) {
	now := time.Now().UTC()
	// Two devices. Per-device stress fields peak at different samples.
	// Peak() must merge by device Index and take the per-device max —
	// not the global max across devices, not the max of the same index
	// in different samples.
	fc := &fakeCollector{
		ok: true,
		samples: []Sample{
			{
				Timestamp: now,
				Devices: []Device{
					{Index: 0, Name: "a100-0", UtilizationGPUPercent: 30, UtilizationMemPercent: 20, MemoryUsedBytes: 1000, MemoryTotalBytes: 8000, TemperatureC: 65, PowerDrawWatts: 100, PowerLimitWatts: 400},
					{Index: 1, Name: "a100-1", UtilizationGPUPercent: 50, UtilizationMemPercent: 25, MemoryUsedBytes: 2000, MemoryTotalBytes: 8000, TemperatureC: 63, PowerDrawWatts: 150, PowerLimitWatts: 400},
				},
			},
			{
				Timestamp: now.Add(time.Second),
				Devices: []Device{
					{Index: 0, Name: "a100-0", UtilizationGPUPercent: 70, UtilizationMemPercent: 35, MemoryUsedBytes: 4000, MemoryTotalBytes: 8000, TemperatureC: 80, PowerDrawWatts: 250, PowerLimitWatts: 400},
					{Index: 1, Name: "a100-1", UtilizationGPUPercent: 40, UtilizationMemPercent: 60, MemoryUsedBytes: 3000, MemoryTotalBytes: 8000, TemperatureC: 72, PowerDrawWatts: 180, PowerLimitWatts: 400},
				},
			},
			{
				Timestamp: now.Add(2 * time.Second),
				Devices: []Device{
					{Index: 0, Name: "a100-0", UtilizationGPUPercent: 50, UtilizationMemPercent: 40, MemoryUsedBytes: 2000, MemoryTotalBytes: 8000, TemperatureC: 70, PowerDrawWatts: 120, PowerLimitWatts: 400},
					{Index: 1, Name: "a100-1", UtilizationGPUPercent: 60, UtilizationMemPercent: 45, MemoryUsedBytes: 1000, MemoryTotalBytes: 8000, TemperatureC: 75, PowerDrawWatts: 100, PowerLimitWatts: 400},
				},
			},
		},
	}
	m := NewWithCollector(fc, 30*time.Millisecond, 8, newTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	time.Sleep(120 * time.Millisecond)

	got := m.Peak(5 * time.Second)
	if got == nil {
		t.Fatal("expected non-nil peak")
	}
	p := got.(*Sample)
	if len(p.Devices) != 2 {
		t.Fatalf("peak device count = %d, want 2", len(p.Devices))
	}
	// Per-device merged by Index. Device 0: peak GPU util=70, mem
	// util=40, memory=4000, temp=80, power=250. Device 1: peak GPU
	// util=60, mem util=60, memory=3000, temp=75, power=180.
	byIdx := map[int]Device{}
	for _, d := range p.Devices {
		byIdx[d.Index] = d
	}
	if d := byIdx[0]; d.UtilizationGPUPercent != 70 || d.UtilizationMemPercent != 40 || d.MemoryUsedBytes != 4000 {
		t.Errorf("device 0 peak wrong: %+v", d)
	}
	if d := byIdx[1]; d.UtilizationGPUPercent != 60 || d.UtilizationMemPercent != 60 || d.MemoryUsedBytes != 3000 {
		t.Errorf("device 1 peak wrong: %+v", d)
	}
	if d := byIdx[0]; d.TemperatureC != 80 || d.PowerDrawWatts != 250 {
		t.Errorf("device 0 thermal/power peak wrong: %+v", d)
	}
	if d := byIdx[1]; d.TemperatureC != 75 || d.PowerDrawWatts != 180 {
		t.Errorf("device 1 thermal/power peak wrong: %+v", d)
	}
	// PowerLimitWatts is constant; should still be present.
	if d := byIdx[0]; d.PowerLimitWatts != 400 {
		t.Errorf("device 0 power_limit = %f, want 400", d.PowerLimitWatts)
	}
}

func TestGPU_PeakEmptyWindowReturnsNil(t *testing.T) {
	fc := &fakeCollector{
		ok: true,
		samples: []Sample{
			{
				Timestamp: time.Now().Add(-time.Hour),
				Devices:   []Device{{Index: 0, Name: "old", UtilizationGPUPercent: 90}},
			},
		},
	}
	m := NewWithCollector(fc, 30*time.Millisecond, 8, newTestLogger())
	if got := m.Peak(time.Millisecond); got != nil {
		t.Fatalf("Peak outside history window = %#v, want nil", got)
	}
}
