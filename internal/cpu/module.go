package cpu

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// Module is the cpu sub-module's public type. It implements metrics.Module.
type Module struct {
	collector Collector
	base      *metrics.Base[Sample]
	logger    *slog.Logger
	profile   ProfileInfo
}

// New builds a CPU module. The collector is warmed up synchronously so the
// first /metrics/cpu call already has a real reading instead of zeros.
// On warmup error the module still starts (Enabled()==true) — the error
// surfaces on the first sample's Error field. This matches the design where
// "enabled" means "running", not "perfectly healthy".
func New(interval time.Duration, historyCapacity int, logger *slog.Logger) (*Module, error) {
	return NewWithCollector(NewGopsutilCollector(), interval, historyCapacity, logger)
}

// NewWithCollector is the testable constructor: it accepts an arbitrary
// Collector implementation. Production code should use New().
func NewWithCollector(c Collector, interval time.Duration, historyCapacity int, logger *slog.Logger) (*Module, error) {
	if err := c.Warmup(interval); err != nil {
		logger.Warn("cpu warmup failed; first sample will report error", "err", err)
	}
	m := &Module{
		collector: c,
		base:      metrics.NewBase[Sample](interval, historyCapacity),
		logger:    logger,
		profile:   Info(),
	}
	m.collectOnce()
	return m, nil
}

func (m *Module) Name() string           { return "cpu" }
func (m *Module) Enabled() bool          { return true }
func (m *Module) DisabledReason() string { return "" }

// Shutdown is a no-op for CPU since there are no external resources to release.
func (m *Module) Shutdown(_ context.Context) error { return nil }

// Start launches the sampling goroutine and returns immediately. The
// goroutine exits when ctx is cancelled.
func (m *Module) Start(ctx context.Context) error {
	go metrics.RunLoop(ctx, m.base.Interval, m.collectOnce)
	return nil
}

// Latest returns the most recent sample or nil if none has been published
// yet. Returned as `any` to satisfy metrics.Module.
func (m *Module) Latest() any {
	if m.base == nil {
		return nil
	}
	return m.base.Latest()
}

// History returns samples collected within the trailing duration d, oldest first.
func (m *Module) History(d time.Duration) []any {
	if m.base == nil {
		return nil
	}
	return metrics.HistoryAny(m.base.Buffer(), d, func(s Sample, cutoff time.Time) bool {
		return !s.Timestamp.Before(cutoff)
	})
}

// Peak returns a sample whose stress fields are the maximum observed over
// the trailing duration d. OverallPercent and per-core PerCorePercent are
// maxed across the window; the static fields (CoreCount, Timestamp) come
// from the most recent sample in the window. Returns nil when the window
// contains no samples.
func (m *Module) Peak(d time.Duration) any {
	if m.base == nil {
		return nil
	}
	cutoff := time.Now().Add(-d)
	snap := m.base.Buffer().Snapshot()
	// Snapshot is oldest-first; iterate newest-first so the first
	// in-window sample we see is the latest, which we use as the seed
	// for non-stress fields. Older samples only contribute to the
	// per-field max.
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
		if s.OverallPercent > peak.OverallPercent {
			peak.OverallPercent = s.OverallPercent
		}
		// Per-core max. Length differences across samples are extremely
		// unlikely post-warmup; if they ever differ, the longer slice
		// wins (we don't shrink the result).
		if len(s.PerCorePercent) > len(peak.PerCorePercent) {
			peak.PerCorePercent = append([]float64(nil), s.PerCorePercent...)
		}
		for i, v := range s.PerCorePercent {
			if i < len(peak.PerCorePercent) && v > peak.PerCorePercent[i] {
				peak.PerCorePercent[i] = v
			}
		}
	}
	return peak
}

// LastSampleAge returns the wall-clock time since the most recent sample.
func (m *Module) LastSampleAge() time.Duration { return m.base.LastSampleAge() }

// Profile returns the static metadata captured at startup.
func (m *Module) Profile() any { return m.profile }

// RegisterRoutes adds /metrics/cpu and /metrics/cpu/history to mux.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	metrics.RegisterRoutes(mux, m.base, "cpu", m.base.Latest, m.History, nil)
}

// collectOnce takes one sample and publishes it.
func (m *Module) collectOnce() {
	m.base.Publish(m.collector.Collect())
}
