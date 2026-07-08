package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "monitor.pid")
}

func TestAcquireRelease(t *testing.T) {
	path := tmpPath(t)

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if l == nil {
		t.Fatal("expected non-nil lock")
	}

	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// File should be removed after release.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected pid file to be removed, got err=%v", err)
	}
}

func TestAcquireTwiceFails(t *testing.T) {
	path := tmpPath(t)

	l1, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer l1.Release()

	_, err = Acquire(path)
	if err == nil {
		t.Fatal("expected second acquire to fail")
	}
}

func TestAcquireStalePID(t *testing.T) {
	path := tmpPath(t)

	// Write a PID that is almost certainly not running.
	if err := os.WriteFile(path, []byte("99999\n"), 0644); err != nil {
		t.Fatalf("write stale pid: %v", err)
	}

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire with stale pid: %v", err)
	}
	l.Release()
}

func TestReleaseNil(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Fatalf("release nil: %v", err)
	}
}
