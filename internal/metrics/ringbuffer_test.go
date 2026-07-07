package metrics

import (
	"sync"
	"testing"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb := NewRingBuffer[int](3)
	if got := rb.Len(); got != 0 {
		t.Errorf("empty Len = %d, want 0", got)
	}
	if _, ok := rb.Latest(); ok {
		t.Errorf("empty Latest ok = true, want false")
	}
	if s := rb.Snapshot(); len(s) != 0 {
		t.Errorf("empty Snapshot len = %d, want 0", len(s))
	}

	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	if got := rb.Len(); got != 3 {
		t.Errorf("after 3 pushes Len = %d, want 3", got)
	}
	if v, _ := rb.Latest(); v != 3 {
		t.Errorf("Latest = %d, want 3", v)
	}
	if s := rb.Snapshot(); !equal(s, []int{1, 2, 3}) {
		t.Errorf("Snapshot = %v, want [1 2 3]", s)
	}

	// wrap-around: oldest gets overwritten
	rb.Push(4)
	if got := rb.Len(); got != 3 {
		t.Errorf("after wrap Len = %d, want 3 (cap)", got)
	}
	if v, _ := rb.Latest(); v != 4 {
		t.Errorf("Latest = %d, want 4", v)
	}
	if s := rb.Snapshot(); !equal(s, []int{2, 3, 4}) {
		t.Errorf("Snapshot = %v, want [2 3 4]", s)
	}
}

func TestRingBuffer_FullWrap(t *testing.T) {
	rb := NewRingBuffer[int](3)
	for i := 1; i <= 7; i++ {
		rb.Push(i)
	}
	if s := rb.Snapshot(); !equal(s, []int{5, 6, 7}) {
		t.Errorf("Snapshot after 7 pushes (cap=3) = %v, want [5 6 7]", s)
	}
}

func TestRingBuffer_ConcurrentSafety(t *testing.T) {
	rb := NewRingBuffer[int](100)
	var wg sync.WaitGroup
	const writers = 4
	const per = 500
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				rb.Push(base*10000 + i)
			}
		}(w)
	}
	// concurrent reader — must stop before wg.Wait so writers can finish
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = rb.Snapshot()
				_, _ = rb.Latest()
				_ = rb.Len()
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-readerDone

	if got := rb.Len(); got != 100 {
		t.Errorf("final Len = %d, want 100 (cap)", got)
	}
}

func TestRingBuffer_PanicOnZeroCap(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("expected panic for cap=0")
		}
	}()
	_ = NewRingBuffer[int](0)
}

func equal[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
