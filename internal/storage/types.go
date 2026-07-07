// Package storage implements the per-mount-point disk-usage sub-module.
//
// At startup the module reads the system mount table and validates that every
// path listed in the configuration is a real mount point; an unmatched path
// is a fatal configuration error (the user explicitly opted in to tracking
// that path).
//
// At runtime each configured mount point is polled via gopsutil's disk.Usage
// with a per-call timeout, so a slow or unresponsive NFS mount cannot block
// the sampler.
package storage

import "time"

// Disk is one mount point's usage snapshot.
type Disk struct {
	Path        string  `json:"path"`
	Alias       string  `json:"alias"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	FSType      string  `json:"fs_type"`
	Error       string  `json:"error,omitempty"`
}

// Sample is a snapshot of every configured mount point.
type Sample struct {
	Timestamp time.Time `json:"timestamp"`
	Disks     []Disk    `json:"disks"`
	Error     string    `json:"error,omitempty"`
}

// ProfileInfo describes the configured mount points for /profile.
// Mount-table details (device, fstype) come from the startup snapshot.
type ProfileInfo struct {
	MountPoints []MountPoint `json:"mount_points"`
}

// MountPoint is one entry in the /profile endpoint's storage section.
type MountPoint struct {
	Path   string `json:"path"`
	Alias  string `json:"alias"`
	Device string `json:"device"`
	FSType string `json:"fstype"`
}
