package gpu

import (
	"sync"
)

// Collector abstracts the underlying NVML access. The interface lets tests
// (and the no-NVML build) substitute a stub without dragging in cgo.
type Collector interface {
	// Init is called once at startup. It should perform any expensive
	// initialization (NVML init, device enumeration). The bool return
	// reports whether the collector is actually usable; if false, the
	// module disables itself and surfaces the error as disabled_reason.
	Init() (ok bool, disabledReason string)

	// Shutdown releases any resources held by the collector. Implementations
	// must make this safe to call multiple times.
	Shutdown()

	// Profile returns static per-device metadata captured at Init time.
	Profile() ProfileInfo

	// Collect returns a fresh sample. May return an empty sample if the
	// collector is unavailable; the module handles that case.
	Collect() Sample
}

// nopCollector is the fallback used when NVML is not compiled in or when
// Init fails. It always reports "unavailable" and returns empty samples.
type nopCollector struct {
	mu     sync.Mutex
	reason string
}
