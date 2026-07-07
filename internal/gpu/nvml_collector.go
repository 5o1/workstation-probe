//go:build nvml

// This file implements the NVML-backed collector. It is only compiled when
// the build tag `nvml` is set; otherwise the stub collector in collector.go
// is used and the module reports itself as disabled.
package gpu

import (
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type nvmlCollector struct {
	inited  bool
	devices []nvml.Device
	profiles []ProfileDevice
}

// NewNVMLCollector returns the production NVML-backed collector. Built
// only when the `nvml` tag is set; otherwise the stub in stub_collector.go
// takes its place.
func NewNVMLCollector() Collector { return &nvmlCollector{} }

func (c *nvmlCollector) Init() (bool, string) {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return false, fmt.Sprintf("nvml init failed: %v", nvml.ErrorString(ret))
	}
	c.inited = true

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		nvml.Shutdown()
		c.inited = false
		return false, fmt.Sprintf("nvml device count failed: %v", nvml.ErrorString(ret))
	}

	c.devices = make([]nvml.Device, 0, count)
	c.profiles = make([]ProfileDevice, 0, count)
	for i := 0; i < count; i++ {
		dev, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}
		uuid, _ := dev.GetUUID()
		name, _ := dev.GetName()
		mem, _ := dev.GetMemoryInfo()
		c.devices = append(c.devices, dev)
		c.profiles = append(c.profiles, ProfileDevice{
			Index:           i,
			UUID:            uuid,
			Name:            name,
			MemoryTotalBytes: mem.Total,
		})
	}
	return true, ""
}

func (c *nvmlCollector) Shutdown() {
	if c.inited {
		nvml.Shutdown()
		c.inited = false
	}
}

func (c *nvmlCollector) Profile() ProfileInfo {
	return ProfileInfo{
		DisabledReason: "",
		DeviceCount:    len(c.profiles),
		Devices:        c.profiles,
	}
}

func (c *nvmlCollector) Collect() Sample {
	out := Sample{Timestamp: nowUTC(), Devices: make([]Device, 0, len(c.devices))}
	for i, dev := range c.devices {
		// Use the NVML index captured at Init() rather than the range index,
		// so /metrics/gpu and /profile report the same index for the same
		// physical GPU even when a lower-indexed device failed to enumerate.
		d := Device{Index: c.profiles[i].Index, UUID: c.profiles[i].UUID, Name: c.profiles[i].Name}

		if util, ret := dev.GetUtilizationRates(); ret == nvml.SUCCESS {
			d.UtilizationGPUPercent = float64(util.Gpu)
			d.UtilizationMemPercent = float64(util.Memory)
		} else {
			d.Error = fmt.Sprintf("util: %v", nvml.ErrorString(ret))
		}
		if mem, ret := dev.GetMemoryInfo(); ret == nvml.SUCCESS {
			d.MemoryTotalBytes = mem.Total
			d.MemoryUsedBytes = mem.Used
		}
		if temp, ret := dev.GetTemperature(nvml.TEMPERATURE_GPU); ret == nvml.SUCCESS {
			d.TemperatureC = float64(temp)
		}
		if power, ret := dev.GetPowerUsage(); ret == nvml.SUCCESS {
			d.PowerDrawWatts = float64(power) / 1000.0 // mW → W
		}
		// PowerManagementLimit is NVML_ERROR_NOT_SUPPORTED on most
		// consumer GeForce cards; failure is silent and d.PowerLimitWatts
		// stays at its zero value, which the frontend interprets as
		// "no cap available — use memory occupancy only".
		if cap, ret := dev.GetPowerManagementLimit(); ret == nvml.SUCCESS {
			d.PowerLimitWatts = float64(cap) / 1000.0 // mW → W
		}
		out.Devices = append(out.Devices, d)
	}
	return out
}
