// Package metrics provides the shared types every collector module builds on:
// the Module interface and a generic thread-safe ring buffer.
package metrics

import (
	"context"
	"net/http"
	"time"
)

// Module is the contract every collector implements. The four metric sources
// (CPU/GPU/memory/storage) each satisfy it; the central server iterates over
// a []Module to compose the merged /metrics view and to register routes.
type Module interface {
	// Name returns the short identifier used in URLs (e.g. "cpu", "memory").
	Name() string
	// Enabled reports whether the module is actually running. A module may
	// be configured-on but report false because required hardware is missing
	// (e.g. NVML absent on a non-NVIDIA machine).
	Enabled() bool
	// DisabledReason returns a human-readable explanation when Enabled()
	// is false; empty when the module is enabled.
	DisabledReason() string
	// Start launches the background sampling goroutine. It must return
	// promptly; long-running work belongs in the goroutine itself.
	Start(ctx context.Context) error
	// Shutdown releases any resources the module holds. Idempotent.
	Shutdown(ctx context.Context) error
	// Latest returns the most recent sample. The concrete type is the
	// module-specific Sample type (e.g. *cpu.Sample). Returning nil means
	// no sample has been produced yet, which combined with Enabled()==true
	// indicates startup latency.
	Latest() any
	// History returns samples collected within the trailing duration d, in
	// chronological order. An empty slice (not nil) is returned if there
	// are no matching samples.
	History(d time.Duration) []any
	// LastSampleAge returns time since the most recent sample was produced.
	// Returns +Inf when no sample has been taken yet.
	LastSampleAge() time.Duration
	// Profile returns the static metadata captured at startup. The concrete
	// type is module-specific (e.g. cpu.ProfileInfo).
	Profile() any
	// RegisterRoutes wires module-specific HTTP routes onto the provided mux.
	// Each module is responsible for /metrics/<name> and /metrics/<name>/history.
	RegisterRoutes(mux *http.ServeMux)
}
