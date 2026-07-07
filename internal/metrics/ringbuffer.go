package metrics

import (
	"sync"
)

// RingBuffer is a fixed-capacity, thread-safe FIFO of T. Once full, the oldest
// element is overwritten on the next Push. All read methods return copies or
// values that the caller may freely mutate without affecting future reads.
//
// The zero value is not usable; call NewRingBuffer.
type RingBuffer[T any] struct {
	mu   sync.RWMutex
	cap  int
	data []T
	head int  // index where the next Push will write
	full bool // true once data has wrapped past cap at least once
}

// NewRingBuffer returns a ring buffer with the given capacity. Panics if cap
// is non-positive; this is a programming error caught at startup, not a
// runtime condition.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity <= 0 {
		panic("metrics: RingBuffer capacity must be > 0")
	}
	return &RingBuffer[T]{
		cap:  capacity,
		data: make([]T, capacity),
	}
}

// Cap returns the configured capacity.
func (r *RingBuffer[T]) Cap() int { return r.cap }

// Len returns the number of valid samples currently stored (<= cap).
func (r *RingBuffer[T]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return r.cap
	}
	return r.head
}

// Push appends v, overwriting the oldest element when full.
func (r *RingBuffer[T]) Push(v T) {
	r.mu.Lock()
	r.data[r.head] = v
	r.head = (r.head + 1) % r.cap
	if r.head == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Latest returns the most recently pushed element. The second return is false
// if no element has been pushed yet.
func (r *RingBuffer[T]) Latest() (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var zero T
	if !r.full && r.head == 0 {
		return zero, false
	}
	idx := r.head - 1
	if idx < 0 {
		idx = r.cap - 1
	}
	return r.data[idx], true
}

// Snapshot returns a copy of all valid samples in chronological order
// (oldest first). The returned slice is independent of the buffer.
func (r *RingBuffer[T]) Snapshot() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := r.head
	if r.full {
		n = r.cap
	}
	out := make([]T, n)
	if r.full {
		// data is laid out head..end then 0..head-1 in chronological order
		copy(out, r.data[r.head:])
		copy(out[r.cap-r.head:], r.data[:r.head])
	} else {
		copy(out, r.data[:r.head])
	}
	return out
}
