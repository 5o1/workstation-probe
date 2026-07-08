// Package memory implements the memory-usage sub-module. It samples
// virtual-memory and swap statistics on each tick.
package memory

import "time"

// Sample is a single memory snapshot. Error is non-empty when one of the
// underlying gopsutil calls failed; in that case all numeric fields are
// zero values.
//
// UsedBytes is gopsutil's human-consumable used memory. On Linux that is
// Total - Available, not Total - Free. htop's "used" bar shows a narrower
// concept: Total - Free - Buffers - Cached. That value is exposed as
// UsedNoCacheBytes so the webview can render the used / buffers / cached
// segments without double-counting reclaimable cache.
//
// The kernel breakdown fields (BuffersBytes / CachedBytes / SharedBytes)
// are populated on Linux from /proc/meminfo via gopsutil; they are zero on
// other platforms.
type Sample struct {
	Timestamp        time.Time `json:"timestamp"`
	TotalBytes       uint64    `json:"total_bytes"`
	UsedBytes        uint64    `json:"used_bytes"`
	UsedNoCacheBytes uint64    `json:"used_no_cache_bytes"`
	AvailableBytes   uint64    `json:"available_bytes"`
	UsedPercent      float64   `json:"used_percent"`
	SwapTotalBytes   uint64    `json:"swap_total_bytes"`
	SwapUsedBytes    uint64    `json:"swap_used_bytes"`
	BuffersBytes     uint64    `json:"buffers_bytes,omitempty"`
	CachedBytes      uint64    `json:"cached_bytes,omitempty"`
	SharedBytes      uint64    `json:"shared_bytes,omitempty"`
	Error            string    `json:"error,omitempty"`
}

// ProfileInfo is the static metadata the /profile endpoint exposes.
type ProfileInfo struct {
	TotalBytes   uint64 `json:"total_bytes"`
	StartupError string `json:"startup_error,omitempty"`
}
