package cpu

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// Collector abstracts the gopsutil calls so tests can swap in a fake.
// The interface returns a Sample populated from a single observation.
type Collector interface {
	// Warmup primes gopsutil's internal ticker. The first non-zero
	// Percent() call is what starts the background measurement loop; without
	// it, every subsequent Percent(0, ...) call returns zeros. Warmup is
	// blocking for at least interval and returns any error encountered.
	Warmup(interval time.Duration) error
	// Collect returns a fresh sample. After Warmup, this is fast and uses
	// the cached value from gopsutil's internal ticker.
	Collect() Sample
}

// gopsutilCollector is the production implementation backed by gopsutil.
type gopsutilCollector struct{}

// NewGopsutilCollector returns a Collector backed by github.com/shirou/gopsutil.
func NewGopsutilCollector() Collector { return &gopsutilCollector{} }

func (gopsutilCollector) Warmup(interval time.Duration) error {
	// We need two real reads so that gopsutil can compute a delta; the
	// library starts a background ticker on the first non-zero call. Run
	// them concurrently — each blocks for ≥ interval, so serial warmup
	// blocks startup for 2× interval; parallel halves it to interval.
	var wg sync.WaitGroup
	wg.Add(2)
	var overallErr, perCoreErr error
	go func() {
		defer wg.Done()
		if _, err := cpu.Percent(interval, false); err != nil {
			overallErr = err
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := cpu.Percent(interval, true); err != nil {
			perCoreErr = err
		}
	}()
	wg.Wait()
	if overallErr != nil {
		return overallErr
	}
	return perCoreErr
}

func (gopsutilCollector) Collect() Sample {
	return collectSample()
}

// collectSample does the actual gopsutil round-trip. The indirection lets
// tests pin the Timestamp field by overriding nowUTC in this package.
func collectSample() Sample {
	overall, err := cpu.Percent(0, false)
	if err != nil {
		return Sample{Error: err.Error()}
	}
	per, err := cpu.Percent(0, true)
	if err != nil {
		return Sample{Error: err.Error()}
	}
	var ov float64
	if len(overall) > 0 {
		ov = overall[0]
	}
	return Sample{
		Timestamp:      nowUTC(),
		OverallPercent: ov,
		PerCorePercent: per,
		CoreCount:      len(per),
	}
}

// nowUTC is a package-level indirection so tests can swap it. Mirrors the
// same pattern in internal/gpu and internal/memory.
var nowUTC = func() time.Time { return time.Now().UTC() }

// Info returns the static CPU metadata used by the /profile endpoint.
// On failure it returns a ProfileInfo with StartupError populated and an
// empty model name; the caller should still expose the module as enabled
// because the collector itself works fine even when metadata lookup fails.
func Info() ProfileInfo {
	infos, err := cpu.Info()
	if err != nil {
		return ProfileInfo{StartupError: err.Error()}
	}
	pi := ProfileInfo{CoreCount: len(infos)}
	if len(infos) > 0 {
		first := infos[0]
		pi.ModelName = first.ModelName
		pi.Vendor = first.VendorID
	}
	return pi
}
