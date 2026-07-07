package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) == "" {
		t.Fatal("PID file is empty")
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("PID file still exists after Release")
	}
}

func TestAcquireSecondFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	l1, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer l1.Release()

	_, err = Acquire(path)
	if err == nil {
		t.Fatal("second Acquire should have failed")
	}
}

func TestStalePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	// Write a PID that is highly unlikely to be running.
	if err := os.WriteFile(path, []byte("99999999"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire on stale PID should succeed: %v", err)
	}
	l.Release()
}

func TestReleaseNoFile(t *testing.T) {
	l := &Lock{path: filepath.Join(t.TempDir(), "nonexistent.pid")}
	if err := l.Release(); err != nil {
		t.Fatalf("Release on nonexistent file should not error: %v", err)
	}
}
