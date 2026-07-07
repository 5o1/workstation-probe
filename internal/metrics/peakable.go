package metrics

import "time"

// Peakable is an optional capability a module may implement to return a
// "peak over window" sample. The combined /metrics handler type-asserts
// to this interface when ?mode=peak is requested; modules that do not
// implement it are passed through as latest. This is the opt-in seam
// for new statistical views without bloating metrics.Module for modules
// that don't have a "more stressed is higher" metric (e.g. storage).
type Peakable interface {
	// Peak returns a sample whose stress fields are the maximum observed
	// over the trailing duration d. Non-stress fields are taken from
	// the most recent sample in the window. The concrete return type is
	// the module-specific Sample (e.g. *cpu.Sample). Returns nil when
	// the window is empty (handler falls back to Latest()).
	Peak(d time.Duration) any
}
