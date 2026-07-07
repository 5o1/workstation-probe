package handlers

import (
	"net/http"
	"time"

	"github.com/assaneko/workstation-probe/internal/metrics"
)

// moduleHealth is the per-module entry in the /health response.
// last_sample_age_ms is omitted when the module is disabled.
type moduleHealth struct {
	Enabled         bool   `json:"enabled"`
	LastSampleAgeMs *int64 `json:"last_sample_age_ms,omitempty"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
}

// HealthResponse is the full /health payload.
type HealthResponse struct {
	Status        string                  `json:"status"` // "ok" or "degraded"
	UptimeSeconds int64                   `json:"uptime_seconds"`
	Modules       map[string]moduleHealth `json:"modules"`
}

// HealthHandler aggregates LastSampleAge() across every module and reports
// overall status. 503 is returned when any enabled module's most recent
// sample is older than maxStaleness.
type HealthHandler struct {
	start        time.Time
	modules      []metrics.Module
	maxStaleness time.Duration
}

// NewHealth builds the /health handler. maxStaleness is typically
// 2 × sampler interval.
func NewHealth(mods []metrics.Module, maxStaleness time.Duration) *HealthHandler {
	return &HealthHandler{
		start:        time.Now().UTC(),
		modules:      mods,
		maxStaleness: maxStaleness,
	}
}

// ServeHTTP implements http.Handler.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status:        "ok",
		UptimeSeconds: int64(time.Since(h.start).Seconds()),
		Modules:       make(map[string]moduleHealth, len(h.modules)),
	}

	for _, m := range h.modules {
		entry := moduleHealth{Enabled: m.Enabled()}
		if !m.Enabled() {
			entry.DisabledReason = m.DisabledReason()
		} else {
			ageMs := m.LastSampleAge().Milliseconds()
			entry.LastSampleAgeMs = &ageMs
			if m.LastSampleAge() > h.maxStaleness {
				resp.Status = "degraded"
			}
		}
		resp.Modules[m.Name()] = entry
	}

	writeJSON(w, healthStatus(resp.Status), resp, false)
}

// healthStatus maps the aggregate status string to the HTTP status code.
// Extracted so the inline ServeHTTP stays readable.
func healthStatus(s string) int {
	if s == "degraded" {
		return http.StatusServiceUnavailable
	}
	return http.StatusOK
}
