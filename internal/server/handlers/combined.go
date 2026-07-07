// Package handlers contains the HTTP handlers that aggregate data across
// every sub-module: the merged /metrics view, the /profile snapshot, and
// the /health summary.
package handlers

import (
	"net/http"
	"time"

	"github.com/assaneko/workstation-probe/internal/cpu"
	"github.com/assaneko/workstation-probe/internal/gpu"
	"github.com/assaneko/workstation-probe/internal/memory"
	"github.com/assaneko/workstation-probe/internal/metrics"
	"github.com/assaneko/workstation-probe/internal/storage"
)

var nowLocal = time.Now

// CombinedSample is the merged /metrics payload. Each field is a pointer
// to the module-specific Sample type so disabled modules disappear from
// the JSON entirely (json:",omitempty" semantics).
//
// When the request sets ?mode=peak, the top-level Mode and
// WindowSeconds fields are populated and per-module stress fields hold
// the maximum value observed over the trailing window. Mode is empty
// (and therefore omitted) for the default latest-mode response.
type CombinedSample struct {
	ServerTimeLocal       string          `json:"server_time_local"`
	ServerTimeUnixSeconds int64           `json:"server_time_unix_seconds"`
	Mode                  string          `json:"mode,omitempty"`
	WindowSeconds         int             `json:"window_seconds,omitempty"`
	CPU                   *cpu.Sample     `json:"cpu,omitempty"`
	Memory                *memory.Sample  `json:"memory,omitempty"`
	GPU                   *gpu.Sample     `json:"gpu,omitempty"`
	Storage               *storage.Sample `json:"storage,omitempty"`
}

// CombinedHandler aggregates Latest() from every enabled module into a
// single JSON document. Construction takes the slice once and we keep
// only the references we need — handlers remain stateless.
type CombinedHandler struct {
	modules []metrics.Module
}

// NewCombined builds the merged /metrics handler.
func NewCombined(mods []metrics.Module) *CombinedHandler {
	return &CombinedHandler{
		modules: mods,
	}
}

// ServeHTTP implements http.Handler.
func (h *CombinedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("mode")

	now := nowLocal()
	out := CombinedSample{
		ServerTimeLocal:       now.Format("2006-01-02 15:04:05"),
		ServerTimeUnixSeconds: now.Unix(),
	}

	// Default: latest. Peak: type-assert each module to metrics.Peakable
	// and dispatch; modules that don't implement Peakable (storage) fall
	// through to Latest. An empty / unrecognised mode also stays on
	// latest so the response shape is byte-identical to the pre-feature
	// behaviour.
	if mode == "peak" {
		// Window defaults to 60s, matching the per-module history
		// default. Invalid windows → 400. Empty / unrecognised mode
		// stays on the latest branch so the response shape is
		// byte-identical to pre-peak behaviour.
		window, ok := metrics.ParseDurationParam(q.Get("window"), 60*time.Second)
		if !ok {
			http.Error(w, `{"error":"invalid_duration"}`, http.StatusBadRequest)
			return
		}
		out.Mode = "peak"
		out.WindowSeconds = int(window.Seconds())
		for _, m := range h.modules {
			if !m.Enabled() {
				continue
			}
			var v any
			if p, ok := m.(metrics.Peakable); ok {
				v = p.Peak(window)
				if v == nil {
					v = m.Latest() // empty window: fall back
				}
			} else {
				v = m.Latest()
			}
			if v == nil {
				continue
			}
			assignSample(&out, v)
		}
	} else {
		for _, m := range h.modules {
			if !m.Enabled() {
				continue
			}
			v := m.Latest()
			if v == nil {
				continue
			}
			assignSample(&out, v)
		}
	}
	writeJSON(w, 0, out, true)
}

// assignSample is a typed switch extracted from ServeHTTP so the
// latest-mode and peak-mode branches share the dispatch logic. New
// modules only need to add a case here.
func assignSample(out *CombinedSample, v any) {
	switch s := v.(type) {
	case *cpu.Sample:
		if s != nil {
			out.CPU = s
		}
	case *memory.Sample:
		if s != nil {
			out.Memory = s
		}
	case *gpu.Sample:
		if s != nil {
			out.GPU = s
		}
	case *storage.Sample:
		if s != nil {
			out.Storage = s
		}
	}
}
