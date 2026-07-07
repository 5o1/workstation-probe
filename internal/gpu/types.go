// Package gpu implements the NVIDIA GPU usage sub-module.
//
// The module's collector is an interface so the same module code works with
// or without the NVML library available. On machines without NVML the
// module reports Enabled()==false and /profile.devices.gpu.disabled_reason
// surfaces the cause; /metrics/gpu then returns 404.
package gpu

import "time"

// Device is one GPU's usage snapshot.
type Device struct {
	Index                 int     `json:"index"`
	UUID                  string  `json:"uuid"`
	Name                  string  `json:"name"`
	UtilizationGPUPercent float64 `json:"utilization_gpu_percent"`
	UtilizationMemPercent float64 `json:"utilization_memory_percent"`
	MemoryTotalBytes      uint64  `json:"memory_total_bytes"`
	MemoryUsedBytes       uint64  `json:"memory_used_bytes"`
	TemperatureC          float64 `json:"temperature_c"`
	PowerDrawWatts        float64 `json:"power_draw_watts"`
	// PowerLimitWatts is the current power management limit (NVML
	// GetPowerManagementLimit) in watts, used to compute power_draw as
	// a percentage for the cell-status background. Zero when the device
	// does not expose a settable cap (most consumer GeForce cards report
	// NVML_ERROR_NOT_SUPPORTED) — the frontend falls back to memory-only
	// in that case.
	PowerLimitWatts float64 `json:"power_limit_watts,omitempty"`
	Error           string  `json:"error,omitempty"`
}

// Sample is one full snapshot across all GPUs.
type Sample struct {
	Timestamp time.Time `json:"timestamp"`
	Devices   []Device  `json:"devices"`
	Error     string    `json:"error,omitempty"`
}

// ProfileInfo is the static metadata exposed via /profile.
type ProfileInfo struct {
	DisabledReason string          `json:"disabled_reason,omitempty"`
	DeviceCount    int             `json:"device_count"`
	Devices        []ProfileDevice `json:"devices"`
}

// ProfileDevice is the per-card metadata captured at startup.
type ProfileDevice struct {
	Index            int    `json:"index"`
	UUID             string `json:"uuid"`
	Name             string `json:"name"`
	MemoryTotalBytes uint64 `json:"memory_total_bytes"`
}
