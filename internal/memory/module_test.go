package memory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeCollector struct {
	vm  VirtualMemory
	sw  SwapMemory
	err error

	// seq, when non-empty, returns successive VirtualMemory snapshots
	// on each Collect() call so tests can drive Peak() across multiple
	// samples. The single-value vm field is used as a fallback when
	// seq is empty (preserves the original single-shot test contract).
	seq []VirtualMemory
	idx int
}

func (f *fakeCollector) Collect() (VirtualMemory, SwapMemory, error) {
	if len(f.seq) > 0 {
		i := f.idx
		if i >= len(f.seq) {
			i = len(f.seq) - 1
		}
		f.idx++
		return f.seq[i], f.sw, f.err
	}
	return f.vm, f.sw, f.err
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMemory_PublishesSample(t *testing.T) {
	fc := &fakeCollector{
		vm: VirtualMemory{TotalBytes: 1000, UsedBytes: 400, AvailableBytes: 600, UsedPercent: 40},
		sw: SwapMemory{TotalBytes: 200, UsedBytes: 50},
	}
	m := NewWithCollector(fc, 50*time.Millisecond, 4, newTestLogger())
	if !m.Enabled() || m.Name() != "memory" {
		t.Errorf("module flags wrong")
	}
	p := m.Latest().(*Sample)
	if p.TotalBytes != 1000 || p.UsedBytes != 400 || p.UsedPercent != 40 {
		t.Errorf("unexpected sample: %+v", *p)
	}
	if p.SwapTotalBytes != 200 || p.SwapUsedBytes != 50 {
		t.Errorf("swap fields wrong: %+v", *p)
	}
	if m.profile.TotalBytes != 1000 {
		t.Errorf("profile total = %d, want 1000", m.profile.TotalBytes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	hist := m.History(5 * time.Second)
	if len(hist) == 0 {
		t.Errorf("expected non-empty history")
	}
}

func TestMemory_PropagatesError(t *testing.T) {
	fc := &fakeCollector{err: errors.New("boom")}
	m := NewWithCollector(fc, time.Second, 4, newTestLogger())
	p := m.Latest().(*Sample)
	if p.Error == "" {
		t.Errorf("expected error in sample")
	}
	if p.TotalBytes != 0 {
		t.Errorf("expected zero values on error, got %+v", *p)
	}
	if m.profile.StartupError == "" {
		t.Errorf("expected startup_error in profile")
	}
}

func TestMemory_Peak(t *testing.T) {
	// Total stays constant at 1000 across the window. The "real used"
	// (Total - Free - Buffers - Cached) goes 250 → 450 → 150. The peak
	// should report 450 and a recomputed UsedPercent of 45.
	fc := &fakeCollector{
		sw: SwapMemory{TotalBytes: 200, UsedBytes: 50},
		seq: []VirtualMemory{
			{TotalBytes: 1000, UsedBytes: 400, AvailableBytes: 600, UsedPercent: 40, BuffersBytes: 100, CachedBytes: 50},
			{TotalBytes: 1000, UsedBytes: 600, AvailableBytes: 400, UsedPercent: 60, BuffersBytes: 100, CachedBytes: 50},
			{TotalBytes: 1000, UsedBytes: 300, AvailableBytes: 700, UsedPercent: 30, BuffersBytes: 100, CachedBytes: 50},
		},
	}
	m := NewWithCollector(fc, 30*time.Millisecond, 8, newTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	time.Sleep(120 * time.Millisecond)

	peak := m.Peak(5 * time.Second)
	if peak == nil {
		t.Fatal("expected non-nil peak")
	}
	p := peak.(*Sample)
	if p.UsedNoCacheBytes != 450 {
		t.Errorf("peak used_no_cache = %d, want 450", p.UsedNoCacheBytes)
	}
	// UsedPercent is recomputed from the peaked used_no_cache, not taken
	// from any one sample.
	if p.UsedPercent != 45 {
		t.Errorf("peak used_percent = %f, want 45", p.UsedPercent)
	}
	// Non-stress fields come from the latest in-window sample (300 used).
	if p.UsedBytes != 300 {
		t.Errorf("latest used = %d, want 300", p.UsedBytes)
	}
	if p.TotalBytes != 1000 {
		t.Errorf("total = %d, want 1000", p.TotalBytes)
	}
}
