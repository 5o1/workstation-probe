//go:build !nvml

// Package gpu provides GPU metric collection via NVML or a build-time stub.
//
// When the `nvml` build tag is absent, NewNVMLCollector returns a stub
// collector that always reports "NVML support not compiled in". This keeps
// the default build runnable on machines without libnvidia-ml.so and on
// non-NVIDIA hosts.
package gpu

// NewNVMLCollector returns the no-NVML stub. When the binary is built
// without the `nvml` tag, libnvidia-ml is absent and the stub is the only
// collector that links successfully. See nvml_collector.go for the
// NVML-backed counterpart built with -tags nvml.
func NewNVMLCollector() Collector {
	return &nopCollector{reason: "nvml support not compiled in (rebuild with -tags nvml)"}
}

func (n *nopCollector) Init() (ok bool, disabledReason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return false, n.reason
}

func (n *nopCollector) Shutdown() {}

func (n *nopCollector) Profile() ProfileInfo {
	return ProfileInfo{DisabledReason: n.reason, DeviceCount: 0, Devices: nil}
}

func (n *nopCollector) Collect() Sample {
	return Sample{Timestamp: nowUTC(), Devices: []Device{}, Error: n.reason}
}
