package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

type fakeCollector struct {
	byPath map[string]Usage
	err    error
}

func (f *fakeCollector) Usage(_ context.Context, path string) (Usage, error) {
	if f.err != nil {
		return Usage{}, f.err
	}
	return f.byPath[path], nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStorage_ValidateMountPoints_OK(t *testing.T) {
	table := []MountEntry{
		{Device: "/dev/sda1", Path: "/", FSType: "ext4"},
		{Device: "/dev/sdb1", Path: "/data", FSType: "ext4"},
	}
	cfg := []MountConfig{{Path: "/", Alias: "root"}, {Path: "/data", Alias: "data"}}
	got, err := validateMountPoints(cfg, table)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Device != "/dev/sda1" || got[1].FSType != "ext4" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestStorage_ValidateMountPoints_NotMounted(t *testing.T) {
	table := []MountEntry{{Path: "/"}}
	_, err := validateMountPoints([]MountConfig{{Path: "/nope", Alias: "x"}}, table)
	if err == nil {
		t.Errorf("expected error for unmatched path")
	}
}

func TestStorage_ModuleHappyPath(t *testing.T) {
	table := []MountEntry{
		{Device: "/dev/sda1", Path: "/", FSType: "ext4"},
		{Device: "/dev/sdb1", Path: "/data", FSType: "ext4"},
	}
	fc := &fakeCollector{byPath: map[string]Usage{
		"/":     {TotalBytes: 1000, UsedBytes: 400, FreeBytes: 600, UsedPercent: 40, FSType: "ext4"},
		"/data": {TotalBytes: 2000, UsedBytes: 1000, FreeBytes: 1000, UsedPercent: 50, FSType: "ext4"},
	}}
	cfg := []MountConfig{{Path: "/", Alias: "root"}, {Path: "/data", Alias: "data"}}
	m, err := NewWithDeps(cfg, 50*time.Millisecond, 4, newTestLogger(), Deps{
		Table:     func() ([]MountEntry, error) { return table, nil },
		Collector: fc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Enabled() || m.Name() != "storage" {
		t.Errorf("module flags wrong")
	}
	if p := m.Profile().(ProfileInfo); len(p.MountPoints) != 2 {
		t.Errorf("profile mount points = %d, want 2", len(p.MountPoints))
	}
	s := m.Latest().(*Sample)
	if len(s.Disks) != 2 || s.Disks[0].TotalBytes != 1000 || s.Disks[1].TotalBytes != 2000 {
		t.Errorf("unexpected disks: %+v", s.Disks)
	}
	if s.Error != "" {
		t.Errorf("top-level error should be empty, got %q", s.Error)
	}
}

func TestStorage_ModuleAllDisksError(t *testing.T) {
	table := []MountEntry{{Path: "/", FSType: "ext4"}}
	fc := &fakeCollector{err: errors.New("disk gone")}
	cfg := []MountConfig{{Path: "/", Alias: "root"}}
	m, err := NewWithDeps(cfg, time.Second, 4, newTestLogger(), Deps{
		Table:     func() ([]MountEntry, error) { return table, nil },
		Collector: fc,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := m.Latest().(*Sample)
	if s.Error == "" {
		t.Errorf("expected top-level error when all disks fail")
	}
}

func TestStorage_FSTypeMismatchDoesNotFailDisk(t *testing.T) {
	table := []MountEntry{{Path: "/", FSType: "ext4"}}
	fc := &fakeCollector{byPath: map[string]Usage{
		"/": {TotalBytes: 1000, UsedBytes: 250, FreeBytes: 750, UsedPercent: 25, FSType: "ext2/ext3"},
	}}
	cfg := []MountConfig{{Path: "/", Alias: "root"}}
	m, err := NewWithDeps(cfg, time.Second, 4, newTestLogger(), Deps{
		Table:     func() ([]MountEntry, error) { return table, nil },
		Collector: fc,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := m.Latest().(*Sample)
	if s.Error != "" {
		t.Fatalf("top-level error should be empty, got %q", s.Error)
	}
	if got := s.Disks[0].Error; got != "" {
		t.Fatalf("disk error should be empty, got %q", got)
	}
	if got := s.Disks[0].FSType; got != "ext2/ext3" {
		t.Fatalf("runtime fstype = %q, want ext2/ext3", got)
	}
}

func TestStorage_ModulePartialFailure(t *testing.T) {
	table := []MountEntry{
		{Path: "/", FSType: "ext4"},
		{Path: "/data", FSType: "ext4"},
	}
	fc := &fakeCollector{byPath: map[string]Usage{
		"/":     {TotalBytes: 100, UsedBytes: 50, UsedPercent: 50, FSType: "ext4"},
		"/data": {}, // will fail because no entry, default error = nil → usage returns zero value (success)
	}}
	cfg := []MountConfig{{Path: "/", Alias: "root"}, {Path: "/data", Alias: "data"}}
	m, _ := NewWithDeps(cfg, time.Second, 4, newTestLogger(), Deps{
		Table:     func() ([]MountEntry, error) { return table, nil },
		Collector: fc,
	})
	s := m.Latest().(*Sample)
	// /data should have all-zero values, no error (because collector didn't fail)
	if s.Disks[1].Error != "" {
		t.Errorf("unexpected /data error: %q", s.Disks[1].Error)
	}
	if s.Disks[1].TotalBytes != 0 {
		t.Errorf("expected zero usage for /data")
	}
	if s.Error != "" {
		t.Errorf("top-level error should be empty for partial failure")
	}
}

func TestStorage_RequiresMountPoints(t *testing.T) {
	_, err := NewWithDeps(nil, time.Second, 4, newTestLogger(), Deps{
		Table:     func() ([]MountEntry, error) { return nil, nil },
		Collector: &fakeCollector{},
	})
	if err == nil {
		t.Errorf("expected error for empty mount list")
	}
}

func TestStorage_TableReadError(t *testing.T) {
	_, err := NewWithDeps(
		[]MountConfig{{Path: "/", Alias: "root"}},
		time.Second, 4, newTestLogger(), Deps{
			Table:     func() ([]MountEntry, error) { return nil, errors.New("no /proc") },
			Collector: &fakeCollector{},
		})
	if err == nil {
		t.Errorf("expected error from table provider")
	}
}

func TestGopsutilDiskCollector_ReusesHungInFlightCall(t *testing.T) {
	orig := diskUsage
	t.Cleanup(func() { diskUsage = orig })

	started := make(chan string, 2)
	release := make(chan struct{})
	diskUsage = func(path string) (*disk.UsageStat, error) {
		started <- path
		<-release
		return &disk.UsageStat{
			Total:       1000,
			Used:        400,
			Free:        600,
			UsedPercent: 40,
			Fstype:      "ext4",
		}, nil
	}

	c := NewGopsutilDiskCollector()
	ctx1, cancel1 := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Usage(ctx1, "/slow")
		errCh <- err
	}()

	select {
	case got := <-started:
		if got != "/slow" {
			t.Fatalf("diskUsage path = %q, want /slow", got)
		}
	case <-time.After(time.Second):
		t.Fatal("diskUsage was not started")
	}

	cancel1()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Usage error = %v, want context.Canceled", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := c.Usage(ctx2, "/slow"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Usage error = %v, want context.Canceled", err)
	}

	select {
	case got := <-started:
		t.Fatalf("started a second worker for in-flight path %q", got)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
}
