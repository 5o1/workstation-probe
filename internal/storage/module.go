package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// nowUTC is a package-level indirection so tests can swap it. Mirrors the
// same pattern in internal/gpu and internal/memory.
var nowUTC = func() time.Time { return time.Now().UTC() }

// Module implements the metrics.Module contract for per-mount-point disk usage.
type Module struct {
	mounts    []MountPoint // resolved at startup; never changes
	collector DiskCollector
	base      *metrics.Base[Sample]
	logger    *slog.Logger
}

// MountTableProvider returns the current mount table. Tests can swap it.
type MountTableProvider func() ([]MountEntry, error)

// Deps groups the optional, test-injectable dependencies of New. Production
// callers should use New; tests pass a Deps value to NewWithDeps.
type Deps struct {
	Table     MountTableProvider
	Collector DiskCollector
}

// New builds the production storage module: it reads the real mount table,
// validates each configured path, and primes an initial sample.
func New(mounts []MountConfig, interval time.Duration, historyCapacity int, logger *slog.Logger) (*Module, error) {
	return NewWithDeps(mounts, interval, historyCapacity, logger, Deps{
		Table:     readMountTable,
		Collector: NewGopsutilDiskCollector(),
	})
}

// NewWithDeps is the testable constructor.
func NewWithDeps(
	mounts []MountConfig,
	interval time.Duration,
	historyCapacity int,
	logger *slog.Logger,
	deps Deps,
) (*Module, error) {
	if len(mounts) == 0 {
		return nil, errors.New("storage: at least one mount point is required")
	}
	rawTable, err := deps.Table()
	if err != nil {
		return nil, fmt.Errorf("read mount table: %w", err)
	}
	resolved, err := validateMountPoints(mounts, rawTable)
	if err != nil {
		return nil, err
	}
	m := &Module{
		mounts:    resolved,
		collector: deps.Collector,
		base:      metrics.NewBase[Sample](interval, historyCapacity),
		logger:    logger,
	}
	m.collectOnce()
	return m, nil
}

	// Name returns "storage".
func (m *Module) Name() string           { return "storage" }
	// Enabled reports that the storage module is always active.
func (m *Module) Enabled() bool          { return true }
	// DisabledReason returns ""; the storage module is never disabled.
func (m *Module) DisabledReason() string { return "" }

// Profile returns the static metadata captured at startup. The slice is
// precomputed once and returned by reference; callers must not mutate it.
func (m *Module) Profile() any { return m.profile() }

// profile returns a freshly-constructed ProfileInfo snapshot of the mount
// table. Used at construction to populate the internal cache, and on every
// /profile call so external mutation of the returned slice can't reach us.
func (m *Module) profile() ProfileInfo {
	out := make([]MountPoint, len(m.mounts))
	copy(out, m.mounts)
	return ProfileInfo{MountPoints: out}
}

// Shutdown is a no-op; storage does not hold external resources.
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

	// LastSampleAge returns the time since the most recent sample.
func (m *Module) LastSampleAge() time.Duration {
	if m.base == nil {
		return time.Duration(1<<63 - 1)
	}
	return m.base.LastSampleAge()
}

	// RegisterRoutes adds /metrics/storage and /metrics/storage/history to mux.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	metrics.RegisterRoutes(mux, m.base, "storage", m.base.Latest, m.History, nil)
}

func (m *Module) collectOnce() {
	disks := make([]Disk, 0, len(m.mounts))
	topLevelErr := ""

	for _, mp := range m.mounts {
		ctx, cancel := context.WithTimeout(context.Background(), perDiskTimeout)
		usage, err := m.collector.Usage(ctx, mp.Path)
		cancel()

		d := Disk{
			Path:   mp.Path,
			Alias:  mp.Alias,
			FSType: usage.FSType,
		}
		if err != nil {
			d.Error = err.Error()
		} else {
			// disk.Usage may report a filesystem family rather than the
			// exact mount-table type, e.g. ext2/ext3 for an ext4 mount.
			// Treat that as display metadata, not as a failed sample.
			if usage.FSType != "" {
				d.FSType = usage.FSType
			} else {
				d.FSType = mp.FSType
			}
			d.TotalBytes = usage.TotalBytes
			d.UsedBytes = usage.UsedBytes
			d.FreeBytes = usage.FreeBytes
			d.UsedPercent = usage.UsedPercent
		}
		disks = append(disks, d)
	}

	// Top-level Error is set when every disk failed — a useful signal that
	// the whole module is degraded rather than a single bad mount.
	if len(disks) > 0 {
		allErr := true
		for _, d := range disks {
			if d.Error == "" {
				allErr = false
				break
			}
		}
		if allErr {
			topLevelErr = "all mount points failed"
		}
	}

	m.base.Publish(Sample{
		Timestamp: nowUTC(),
		Disks:     disks,
		Error:     topLevelErr,
	})
}
