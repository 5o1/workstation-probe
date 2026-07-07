package memory

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// Module is the memory sub-module's public type. It implements metrics.Module.
type Module struct {
	collector Collector
	base      *metrics.Base[Sample]
	logger    *slog.Logger
	profile   ProfileInfo
}

// New builds the production memory module: real gopsutil-backed collector
// and a primed initial sample.
func New(interval time.Duration, historyCapacity int, logger *slog.Logger) *Module {
	return NewWithCollector(NewGopsutilCollector(), interval, historyCapacity, logger)
}

// NewWithCollector is the testable constructor.
func NewWithCollector(c Collector, interval time.Duration, historyCapacity int, logger *slog.Logger) *Module {
	m := &Module{
		collector: c,
		base:      metrics.NewBase[Sample](interval, historyCapacity),
		logger:    logger,
		profile:   captureProfile(c),
	}
	m.collectOnce()
	return m
}

// captureProfile reads /proc/meminfo via a collector call so that tests
// don't hit the real /proc. We use the collector once (best-effort) and
// only return Total to keep things light.
func captureProfile(c Collector) ProfileInfo {
	vm, _, err := c.Collect()
	if err != nil {
		return ProfileInfo{StartupError: err.Error()}
	}
	return ProfileInfo{TotalBytes: vm.TotalBytes}
}

	// Name returns "memory".
func (m *Module) Name() string                     { return "memory" }
	// Enabled reports that the memory module is always active.
func (m *Module) Enabled() bool                    { return true }
	// DisabledReason returns ""; the memory module is never disabled.
func (m *Module) DisabledReason() string           { return "" }
	// Profile returns the static metadata captured at startup.
func (m *Module) Profile() any                     { return m.profile }
	// Shutdown is a no-op; memory does not hold external resources.
func (m *Module) Shutdown(_ context.Context) error { return nil }

	// Start launches the sampling goroutine and returns immediately.
func (m *Module) Start(ctx context.Context) error {
	go metrics.RunLoop(ctx, m.base.Interval, m.collectOnce)
	return nil
}

	// Latest returns the most recent sample or nil.
func (m *Module) Latest() any {
	if m.base == nil {
		return nil
	}
	return m.base.Latest()
}

	// History returns samples within the trailing duration d, oldest first.
func (m *Module) History(d time.Duration) []any {
	if m.base == nil {
		return nil
	}
	return metrics.HistoryAny(m.base.Buffer(), d, func(s Sample, cutoff time.Time) bool {
		return !s.Timestamp.Before(cutoff)
	})
}

// Peak returns a sample whose stress fields are the maximum observed
// over the trailing duration d. UsedNoCacheBytes is maxed across the
// window; non-stress fields come from the most recent in-window
// sample so the peaked view stays self-consistent (a peaked
// UsedNoCacheBytes larger than the latest TotalBytes would otherwise
// be confusing). UsedPercent is recomputed from the peaked
// UsedNoCacheBytes / latest TotalBytes. Returns nil when the window
// is empty.
func (m *Module) Peak(d time.Duration) any {
	if m.base == nil {
		return nil
	}
	cutoff := time.Now().Add(-d)
	snap := m.base.Buffer().Snapshot()
	// Snapshot is oldest-first; iterate newest-first so the first
	// in-window sample we see is the latest, which we use as the seed
	// for non-stress fields. Older samples only contribute to the
	// stress-field max.
	var peak *Sample
	for i := len(snap) - 1; i >= 0; i-- {
		s := &snap[i]
		if s.Timestamp.Before(cutoff) {
			continue
		}
		if peak == nil {
			cp := *s
			peak = &cp
			continue
		}
		if s.UsedNoCacheBytes > peak.UsedNoCacheBytes {
			peak.UsedNoCacheBytes = s.UsedNoCacheBytes
		}
	}
	if peak != nil && peak.TotalBytes > 0 {
		peak.UsedPercent = float64(peak.UsedNoCacheBytes) / float64(peak.TotalBytes) * 100
	}
	return peak
}

	// LastSampleAge returns the time since the most recent sample.
func (m *Module) LastSampleAge() time.Duration {
	if m.base == nil {
		return time.Duration(1<<63 - 1)
	}
	return m.base.LastSampleAge()
}

	// RegisterRoutes adds /metrics/memory and /metrics/memory/history to mux.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	metrics.RegisterRoutes(mux, m.base, "memory", m.base.Latest, m.History, nil)
}

func (m *Module) collectOnce() {
	vm, sw, err := m.collector.Collect()
	if err != nil {
		m.base.Publish(Sample{Timestamp: nowUTC(), Error: err.Error()})
		return
	}
	// htop's "used" excludes reclaimable buffers and page cache. Match
	// that here so the webview's stacked bar segments add up to ~100%
	// of total without overlap. Saturating subtraction would be wrong
	// on non-Linux platforms where the kernel fields are zero; the
	// conditional guards against that.
	var usedNoCache uint64
	if vm.BuffersBytes+vm.CachedBytes < vm.UsedBytes {
		usedNoCache = vm.UsedBytes - vm.BuffersBytes - vm.CachedBytes
	}
	m.base.Publish(Sample{
		Timestamp:        nowUTC(),
		TotalBytes:       vm.TotalBytes,
		UsedBytes:        vm.UsedBytes,
		UsedNoCacheBytes: usedNoCache,
		AvailableBytes:   vm.AvailableBytes,
		UsedPercent:      vm.UsedPercent,
		SwapTotalBytes:   sw.TotalBytes,
		SwapUsedBytes:    sw.UsedBytes,
		BuffersBytes:     vm.BuffersBytes,
		CachedBytes:      vm.CachedBytes,
		SharedBytes:      vm.SharedBytes,
	})
}
