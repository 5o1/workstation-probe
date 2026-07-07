package gpu

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// nowUTC is a package-level indirection so tests can swap it.
var nowUTC = func() time.Time { return time.Now().UTC() }

// Module is the GPU sub-module.
type Module struct {
	collector Collector
	base      *metrics.Base[Sample]
	enabled   atomic.Bool
	logger    *slog.Logger
	profile   ProfileInfo

	once sync.Once // guarantees Shutdown runs exactly once
}

// New constructs the module, initialises NVML (if compiled in), and primes
// an initial sample. If the collector is unavailable the module starts in
// the disabled state; routes still exist but return 404 and /profile marks
// the GPU section as disabled.
func New(interval time.Duration, historyCapacity int, logger *slog.Logger) *Module {
	return NewWithCollector(NewNVMLCollector(), interval, historyCapacity, logger)
}

// NewWithCollector is the testable constructor.
func NewWithCollector(c Collector, interval time.Duration, historyCapacity int, logger *slog.Logger) *Module {
	m := &Module{
		collector: c,
		base:      metrics.NewBase[Sample](interval, historyCapacity),
		logger:    logger,
	}
	ok, reason := c.Init()
	if !ok {
		logger.Warn("gpu module disabled", "reason", reason)
		m.profile = ProfileInfo{DisabledReason: reason, DeviceCount: 0}
		m.enabled.Store(false)
		// still publish one sample so Latest()/History() have a defined value
		m.collectOnce()
		return m
	}
	m.profile = c.Profile()
	m.enabled.Store(true)
	m.collectOnce()
	return m
}

// Shutdown releases NVML resources. Safe to call multiple times.
func (m *Module) Shutdown(_ context.Context) error {
	m.once.Do(func() { m.collector.Shutdown() })
	return nil
}

func (m *Module) Name() string  { return "gpu" }
func (m *Module) Enabled() bool { return m.enabled.Load() }

// DisabledReason returns the reason given by the collector when Init
// failed, or "" when the module is enabled.
func (m *Module) DisabledReason() string {
	if m.Enabled() {
		return ""
	}
	return m.profile.DisabledReason
}

func (m *Module) Profile() any { return m.profile }

func (m *Module) Start(ctx context.Context) error {
	if !m.Enabled() {
		// nothing to do; the disabled sample is published once at New()
		return nil
	}
	go metrics.RunLoop(ctx, m.base.Interval, m.collectOnce)
	return nil
}

func (m *Module) Latest() any {
	if m.base == nil {
		return nil
	}
	return m.base.Latest()
}

func (m *Module) History(d time.Duration) []any {
	if m.base == nil {
		return nil
	}
	return metrics.HistoryAny(m.base.Buffer(), d, func(s Sample, cutoff time.Time) bool {
		return !s.Timestamp.Before(cutoff)
	})
}

// Peak returns a sample whose per-device stress fields are the maximum
// observed over the trailing duration d. Devices are merged by Index
// across the window (matching the same identifier the rest of the
// module uses, per the comment in nvml_collector.go). Per-device
// UtilizationGPUPercent, UtilizationMemPercent, MemoryUsedBytes,
// TemperatureC, and PowerDrawWatts are maxed; other fields are copied
// from the most recent in-window sample for that device. Devices that
// appear in some samples but not others are kept when they have any
// history; devices never seen are dropped. Returns nil when the window
// is empty.
func (m *Module) Peak(d time.Duration) any {
	if m.base == nil {
		return nil
	}
	cutoff := time.Now().Add(-d)
	snap := m.base.Buffer().Snapshot()
	if len(snap) == 0 {
		return nil
	}
	latest := Sample{}
	byIdx := make(map[int]int) // device index → position in latest.Devices
	seen := false
	for i := range snap {
		s := &snap[i]
		if s.Timestamp.Before(cutoff) {
			continue
		}
		seen = true
		latest.Timestamp = s.Timestamp
		for _, src := range s.Devices {
			pos, ok := byIdx[src.Index]
			if !ok {
				cp := src
				byIdx[src.Index] = len(latest.Devices)
				latest.Devices = append(latest.Devices, cp)
				continue
			}
			dst := &latest.Devices[pos]
			base := src
			if src.UtilizationGPUPercent > dst.UtilizationGPUPercent {
				base.UtilizationGPUPercent = src.UtilizationGPUPercent
			} else {
				base.UtilizationGPUPercent = dst.UtilizationGPUPercent
			}
			if src.UtilizationMemPercent > dst.UtilizationMemPercent {
				base.UtilizationMemPercent = src.UtilizationMemPercent
			} else {
				base.UtilizationMemPercent = dst.UtilizationMemPercent
			}
			if src.MemoryUsedBytes > dst.MemoryUsedBytes {
				base.MemoryUsedBytes = src.MemoryUsedBytes
			} else {
				base.MemoryUsedBytes = dst.MemoryUsedBytes
			}
			if src.TemperatureC > dst.TemperatureC {
				base.TemperatureC = src.TemperatureC
			} else {
				base.TemperatureC = dst.TemperatureC
			}
			if src.PowerDrawWatts > dst.PowerDrawWatts {
				base.PowerDrawWatts = src.PowerDrawWatts
			} else {
				base.PowerDrawWatts = dst.PowerDrawWatts
			}
			latest.Devices[pos] = base
		}
	}
	if !seen {
		return nil
	}
	return &latest
}

func (m *Module) LastSampleAge() time.Duration {
	if m.base == nil {
		return time.Duration(1<<63 - 1)
	}
	return m.base.LastSampleAge()
}

// RegisterRoutes adds /metrics/gpu and /metrics/gpu/history to mux. Both
// endpoints short-circuit with 404 `{"error":"gpu_disabled"}` when NVML is
// not available on this host.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	disabled := func() (int, string) {
		if !m.Enabled() {
			return http.StatusNotFound, `{"error":"gpu_disabled"}`
		}
		return 0, ""
	}
	metrics.RegisterRoutes(mux, m.base, "gpu", m.base.Latest, m.History, disabled)
}

func (m *Module) collectOnce() {
	m.base.Publish(m.collector.Collect())
}
