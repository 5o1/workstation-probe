package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Base is the shared state every sampling module uses: a per-module ring
// buffer, the latest sample published lock-free, and the timestamp of that
// sample. Concrete modules embed *Base[S] and add module-specific Name,
// Profile, and collect logic.
//
// The zero value is not usable; call NewBase.
type Base[S any] struct {
	Interval       time.Duration
	buf            *RingBuffer[S]
	latest         atomic.Pointer[S]
	lastSampleUnix atomic.Int64
}

// NewBase builds a Base with the given sampling interval and ring-buffer
// capacity.
func NewBase[S any](interval time.Duration, historyCapacity int) *Base[S] {
	return &Base[S]{
		Interval: interval,
		buf:      NewRingBuffer[S](historyCapacity),
	}
}

// Buffer exposes the ring buffer (for the History helper and tests).
func (b *Base[S]) Buffer() *RingBuffer[S] { return b.buf }

// Publish stores a copy of s into the latest pointer and pushes it onto the
// ring buffer. The copy protects callers from mutating published samples
// after the fact; struct value semantics ensure the struct itself is
// independent, but slices within S share their backing arrays (collectors
// are expected to allocate fresh slices per call).
func (b *Base[S]) Publish(s S) {
	dup := s
	b.latest.Store(&dup)
	b.buf.Push(dup)
	b.lastSampleUnix.Store(time.Now().UnixNano())
}

// Latest returns a pointer to the most recent sample, or nil if none has
// been published yet.
func (b *Base[S]) Latest() *S { return b.latest.Load() }

// LastSampleAge returns the wall-clock time since the most recent sample
// was published, or +Inf when no sample has been published yet.
func (b *Base[S]) LastSampleAge() time.Duration {
	ns := b.lastSampleUnix.Load()
	if ns == 0 {
		return time.Duration(1<<63 - 1)
	}
	return time.Since(time.Unix(0, ns))
}

// RunLoop runs the background sampling ticker until ctx is cancelled,
// calling collect on every tick. The first call is performed synchronously
// by the caller via Publish before RunLoop is invoked, so this method only
// handles steady-state ticks.
func RunLoop(ctx context.Context, interval time.Duration, collect func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			collect()
		}
	}
}

// HistoryWithin returns samples from buf for which keep returns true,
// oldest first. The caller is expected to use d (typically as a Timestamp
// cutoff) inside keep. Returns the typed slice; callers box it into []any
// if needed for the Module interface.
func HistoryWithin[S any](buf *RingBuffer[S], d time.Duration, keep func(s S, cutoff time.Time) bool) []S {
	cutoff := time.Now().Add(-d)
	snap := buf.Snapshot()
	out := make([]S, 0, len(snap))
	for _, s := range snap {
		if keep(s, cutoff) {
			out = append(out, s)
		}
	}
	return out
}

// HistoryAny is HistoryWithin boxed into []any for the Module interface.
// Prefer HistoryWithin when the caller can stay typed.
func HistoryAny[S any](buf *RingBuffer[S], d time.Duration, keep func(s S, cutoff time.Time) bool) []any {
	typed := HistoryWithin(buf, d, keep)
	out := make([]any, len(typed))
	for i, s := range typed {
		out[i] = s
	}
	return out
}

// WriteJSON encodes v as JSON with the right Content-Type. Encoding errors
// are logged but otherwise swallowed (the response is already partially on
// the wire).
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	//nolint:errcheck // partial response already on the wire by the time Encode errors; recovery is impossible.
	_ = enc.Encode(v)
}

// ParseDurationParam parses an optional duration string; falls back to def
// when empty. Returns false if the input is present but unparseable.
func ParseDurationParam(s string, def time.Duration) (time.Duration, bool) {
	if s == "" {
		return def, true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// ParseLimitParam reads the optional ?limit=N query param. 0 means no limit.
func ParseLimitParam(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// HistoryResponse is the shared response shape for /metrics/<m>/history.
// Modules build one with BuildHistoryResponse so the JSON keys stay
// consistent across collectors.
type HistoryResponse struct {
	WindowSeconds int   `json:"window_seconds"`
	Count         int   `json:"count"`
	Samples       []any `json:"samples"`
}

// BuildHistoryResponse trims samples to the last `limit` entries (when
// limit > 0) and wraps them into a HistoryResponse.
func BuildHistoryResponse(window time.Duration, samples []any, limit int) HistoryResponse {
	if limit > 0 && len(samples) > limit {
		samples = samples[len(samples)-limit:]
	}
	return HistoryResponse{
		WindowSeconds: int(window.Seconds()),
		Count:         len(samples),
		Samples:       samples,
	}
}

// DisabledFunc reports whether the module is currently unavailable. The
// returned (status, body) pair is sent as the response when the module is
// disabled; status 0 means "enabled".
type DisabledFunc func() (status int, body string)

// RegisterRoutes wires the standard /metrics/<name> and
// /metrics/<name>/history routes onto mux. Both endpoints:
//
//   - Short-circuit with disabled() when the module is unavailable (e.g.
//     NVML absent on a non-NVIDIA host).
//   - Return 503 `{"error":"not_ready"}` when base is nil or no sample has
//     been published yet.
//
// latest returns the current sample (or nil when none); history returns
// samples within the requested window as []any (caller is responsible for
// boxing from the typed ring buffer).
//
// All four metric sub-modules (cpu, memory, gpu, storage) use this helper;
// the per-module logic that actually varies — disabled checks, error
// propagation, peak-mode math — lives on the module itself, not here.
func RegisterRoutes[S any](
	mux *http.ServeMux,
	base *Base[S],
	name string,
	latest func() *S,
	history func(d time.Duration) []any,
	disabled DisabledFunc,
) {
	latestPath := "/metrics/" + name
	historyPath := latestPath + "/history"

	mux.HandleFunc("GET "+latestPath, func(w http.ResponseWriter, r *http.Request) {
		if code, body := callDisabled(disabled); code != 0 {
			http.Error(w, body, code)
			return
		}
		if base == nil {
			http.Error(w, `{"error":"not_ready"}`, http.StatusServiceUnavailable)
			return
		}
		p := latest()
		if p == nil {
			http.Error(w, `{"error":"not_ready"}`, http.StatusServiceUnavailable)
			return
		}
		WriteJSON(w, *p)
	})
	mux.HandleFunc("GET "+historyPath, func(w http.ResponseWriter, r *http.Request) {
		if code, body := callDisabled(disabled); code != 0 {
			http.Error(w, body, code)
			return
		}
		if base == nil {
			http.Error(w, `{"error":"not_ready"}`, http.StatusServiceUnavailable)
			return
		}
		d, ok := ParseDurationParam(r.URL.Query().Get("duration"), base.Interval*60)
		if !ok {
			http.Error(w, `{"error":"invalid_duration"}`, http.StatusBadRequest)
			return
		}
		limit := ParseLimitParam(r.URL.Query().Get("limit"))
		WriteJSON(w, BuildHistoryResponse(d, history(d), limit))
	})
}

func callDisabled(fn DisabledFunc) (status int, body string) {
	if fn == nil {
		return 0, ""
	}
	return fn()
}
