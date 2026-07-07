// Package cpu implements the CPU usage sub-module: periodic sampling of
// overall + per-core utilization via gopsutil, a thread-safe ring buffer of
// recent samples, and the corresponding HTTP handlers.
package cpu

import "time"

// Sample is a single CPU-usage snapshot. Error is non-empty when the
// underlying gopsutil call failed; in that case all numeric fields are
// zero values, per the error convention documented in the plan.
type Sample struct {
	Timestamp      time.Time `json:"timestamp"`
	OverallPercent float64   `json:"overall_percent"`
	PerCorePercent []float64 `json:"per_core_percent"`
	CoreCount      int       `json:"core_count"`
	Error          string    `json:"error,omitempty"`
}

// ProfileInfo is the static metadata the /profile endpoint exposes for this
// module. It is computed once at startup; it does not change between samples.
type ProfileInfo struct {
	ModelName    string `json:"model_name,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	CoreCount    int    `json:"core_count"`
	StartupError string `json:"startup_error,omitempty"`
}
