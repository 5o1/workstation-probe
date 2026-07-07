package memory

import (
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// VirtualMemory mirrors the relevant gopsutil struct so tests can supply
// canned data without touching the real /proc/meminfo.
//
// Buffers / Cached / Shared are Linux-specific kernel breakdowns; they are
// zero on platforms where gopsutil doesn't expose them.
type VirtualMemory struct {
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	UsedPercent    float64
	BuffersBytes   uint64
	CachedBytes    uint64
	SharedBytes    uint64
}

// SwapMemory mirrors the relevant gopsutil swap struct.
type SwapMemory struct {
	TotalBytes uint64
	UsedBytes  uint64
}

// Collector abstracts the underlying calls so tests can swap in a fake.
type Collector interface {
	Collect() (VirtualMemory, SwapMemory, error)
}

type gopsutilCollector struct{}

// NewGopsutilCollector returns the production Collector backed by
// github.com/shirou/gopsutil. Tests inject their own implementation via
// NewWithCollector.
func NewGopsutilCollector() Collector { return &gopsutilCollector{} }

func (gopsutilCollector) Collect() (VirtualMemory, SwapMemory, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return VirtualMemory{}, SwapMemory{}, err
	}
	sw, err := mem.SwapMemory()
	if err != nil {
		return VirtualMemory{}, SwapMemory{}, err
	}
	return VirtualMemory{
			TotalBytes:     vm.Total,
			AvailableBytes: vm.Available,
			UsedBytes:      vm.Used,
			UsedPercent:    vm.UsedPercent,
			BuffersBytes:   vm.Buffers,
			CachedBytes:    vm.Cached,
			SharedBytes:    vm.Shared,
		}, SwapMemory{
			TotalBytes: sw.Total,
			UsedBytes:  sw.Used,
		}, nil
}

// Info returns the static memory metadata for /profile. We only need Total;
// the rest is per-sample.
func Info() ProfileInfo {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return ProfileInfo{StartupError: err.Error()}
	}
	return ProfileInfo{TotalBytes: vm.Total}
}

// nowUTC is a package-level indirection so tests can swap it. Every metric
// sub-module exposes the same hook so cross-module tests can freeze time
// uniformly.
var nowUTC = func() time.Time { return time.Now().UTC() }
