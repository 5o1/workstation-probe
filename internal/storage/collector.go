package storage

import (
	"context"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// perDiskTimeout bounds the time spent on a single disk.Usage call.
const perDiskTimeout = 500 * time.Millisecond

// Usage is the per-disk usage result populated by a collector.
type Usage struct {
	TotalBytes  uint64
	UsedBytes   uint64
	FreeBytes   uint64
	UsedPercent float64
	FSType      string
}

// DiskCollector abstracts a single disk.Usage call so tests can inject a
// fast fake. The context is honoured; the implementation must respect the
// deadline.
type DiskCollector interface {
	Usage(ctx context.Context, path string) (Usage, error)
}

// NewGopsutilDiskCollector returns the production DiskCollector backed by
// github.com/shirou/gopsutil. Tests inject their own implementation via Deps.
func NewGopsutilDiskCollector() DiskCollector { return &gopsutilDiskCollector{} }

var diskUsage = disk.Usage

type gopsutilDiskCollector struct {
	mu       sync.Mutex
	inFlight map[string]*diskUsageCall
}

type diskUsageCall struct {
	done  chan struct{}
	usage Usage
	err   error
}

func (c *gopsutilDiskCollector) Usage(ctx context.Context, path string) (Usage, error) {
	// gopsutil's disk.Usage is a blocking statvfs call that does not accept
	// a context. We run it in a goroutine so we can race it against ctx
	// and bound the *caller's* wait to perDiskTimeout. If ctx fires first,
	// the worker continues until the kernel answers, so we keep at most one
	// in-flight worker per path to avoid accumulating goroutines on a hung
	// mount. This is still best-effort cancellation for the worker itself.
	call := c.callFor(path)
	select {
	case <-ctx.Done():
		return Usage{}, ctx.Err()
	case <-call.done:
		return call.usage, call.err
	}
}

func (c *gopsutilDiskCollector) callFor(path string) *diskUsageCall {
	c.mu.Lock()
	if c.inFlight == nil {
		c.inFlight = make(map[string]*diskUsageCall)
	}
	if call, ok := c.inFlight[path]; ok {
		c.mu.Unlock()
		return call
	}
	call := &diskUsageCall{done: make(chan struct{})}
	c.inFlight[path] = call
	c.mu.Unlock()

	go func() {
		stat, err := diskUsage(path)
		if err != nil {
			call.err = err
		} else {
			call.usage = Usage{
				TotalBytes:  stat.Total,
				UsedBytes:   stat.Used,
				FreeBytes:   stat.Free,
				UsedPercent: stat.UsedPercent,
				FSType:      stat.Fstype,
			}
		}
		close(call.done)

		c.mu.Lock()
		if c.inFlight[path] == call {
			delete(c.inFlight, path)
		}
		c.mu.Unlock()
	}()
	return call
}
