package handlers

import (
	"net/http"
	"runtime"
	"time"

	gopsutilhost "github.com/shirou/gopsutil/v4/host"

	"github.com/assaneko/workstation-probe/internal/cpu"
	"github.com/assaneko/workstation-probe/internal/gpu"
	"github.com/assaneko/workstation-probe/internal/memory"
	"github.com/assaneko/workstation-probe/internal/metrics"
	"github.com/assaneko/workstation-probe/internal/storage"
)

var hostBootTime = gopsutilhost.BootTime

// ProfileSamplerConfig mirrors the YAML sampler section, exposed in the
// /profile response so clients know the current sampling cadence.
type ProfileSamplerConfig struct {
	IntervalMs      int `json:"interval_ms"`
	HistoryCapacity int `json:"history_capacity"`
}

// ProfileCPUInfo is the per-CPU static block.
type ProfileCPUInfo struct {
	Enabled      bool   `json:"enabled"`
	ModelName    string `json:"model_name,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	CoreCount    int    `json:"core_count"`
	StartupError string `json:"startup_error,omitempty"`
}

// ProfileMemoryInfo is the per-memory static block.
type ProfileMemoryInfo struct {
	Enabled      bool   `json:"enabled"`
	TotalBytes   uint64 `json:"total_bytes"`
	StartupError string `json:"startup_error,omitempty"`
}

// ProfileGPUInfo is the GPU static block.
type ProfileGPUInfo struct {
	Enabled        bool                `json:"enabled"`
	DisabledReason string              `json:"disabled_reason,omitempty"`
	DeviceCount    int                 `json:"device_count"`
	Devices        []gpu.ProfileDevice `json:"devices"`
}

// ProfileStorageInfo is the storage static block.
type ProfileStorageInfo struct {
	Enabled     bool                 `json:"enabled"`
	MountPoints []storage.MountPoint `json:"mount_points"`
}

// ProfileResponse is the full /profile payload.
type ProfileResponse struct {
	Hostname                    string               `json:"hostname"`
	GoVersion                   string               `json:"go_version"`
	StartedAt                   string               `json:"started_at"`
	ServerTimezone              string               `json:"server_timezone"`
	ServerTimezoneOffsetSeconds int                  `json:"server_timezone_offset_seconds"`
	HostBootTimeUnixSeconds     uint64               `json:"host_boot_time_unix_seconds,omitempty"`
	HostBootTimeLocal           string               `json:"host_boot_time_local,omitempty"`
	Sampler                     ProfileSamplerConfig `json:"sampler"`
	Modules                     struct {
		CPU     ProfileCPUInfo     `json:"cpu"`
		Memory  ProfileMemoryInfo  `json:"memory"`
		GPU     ProfileGPUInfo     `json:"gpu"`
		Storage ProfileStorageInfo `json:"storage"`
	} `json:"modules"`
}

// ProfileHandler serves the static /profile snapshot, captured once at
// construction. It does not refresh at request time.
type ProfileHandler struct {
	start                       time.Time
	hostname                    string
	serverTimezone              string
	serverTimezoneOffsetSeconds int
	hostBootTimeUnixSeconds     uint64
	hostBootTimeLocal           string
	sampler                     ProfileSamplerConfig
	mods                        map[string]metrics.Module
}

// NewProfile builds the /profile handler.
func NewProfile(hostname string, sampler ProfileSamplerConfig, mods []metrics.Module) *ProfileHandler {
	idx := make(map[string]metrics.Module, len(mods))
	for _, m := range mods {
		idx[m.Name()] = m
	}
	if hostname == "" {
		hostname = "unknown"
	}
	now := nowLocal()
	zone, offset := now.Zone()
	var bootUnix uint64
	var bootLocal string
	if boot, err := hostBootTime(); err == nil {
		bootUnix = boot
		bootLocal = time.Unix(int64(boot), 0).In(now.Location()).Format("2006-01-02 15:04:05")
	}
	return &ProfileHandler{
		start:                       time.Now().UTC(),
		hostname:                    hostname,
		serverTimezone:              zone,
		serverTimezoneOffsetSeconds: offset,
		hostBootTimeUnixSeconds:     bootUnix,
		hostBootTimeLocal:           bootLocal,
		sampler:                     sampler,
		mods:                        idx,
	}
}

// ServeHTTP implements http.Handler.
func (h *ProfileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := ProfileResponse{
		Hostname:                    h.hostname,
		GoVersion:                   runtime.Version(),
		StartedAt:                   h.start.Format(time.RFC3339Nano),
		ServerTimezone:              h.serverTimezone,
		ServerTimezoneOffsetSeconds: h.serverTimezoneOffsetSeconds,
		HostBootTimeUnixSeconds:     h.hostBootTimeUnixSeconds,
		HostBootTimeLocal:           h.hostBootTimeLocal,
		Sampler:                     h.sampler,
	}
	if m, ok := h.mods["cpu"]; ok {
		pi := m.Profile().(cpu.ProfileInfo)
		resp.Modules.CPU = ProfileCPUInfo{
			Enabled:      m.Enabled(),
			ModelName:    pi.ModelName,
			Vendor:       pi.Vendor,
			Architecture: pi.Architecture,
			CoreCount:    pi.CoreCount,
			StartupError: pi.StartupError,
		}
	}
	if m, ok := h.mods["memory"]; ok {
		pi := m.Profile().(memory.ProfileInfo)
		resp.Modules.Memory = ProfileMemoryInfo{
			Enabled:      m.Enabled(),
			TotalBytes:   pi.TotalBytes,
			StartupError: pi.StartupError,
		}
	}
	if m, ok := h.mods["gpu"]; ok {
		pi := m.Profile().(gpu.ProfileInfo)
		resp.Modules.GPU = ProfileGPUInfo{
			Enabled:        m.Enabled(),
			DisabledReason: pi.DisabledReason,
			DeviceCount:    pi.DeviceCount,
			Devices:        pi.Devices,
		}
	}
	if m, ok := h.mods["storage"]; ok {
		pi := m.Profile().(storage.ProfileInfo)
		resp.Modules.Storage = ProfileStorageInfo{
			Enabled:     m.Enabled(),
			MountPoints: pi.MountPoints,
		}
	}
	writeJSON(w, 0, resp, true)
}
