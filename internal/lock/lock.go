// Package lock provides a PID file lock for single-instance enforcement.
// Acquire writes the current PID to a file and returns an error if another
// running process already holds the lock. Stale PID files (process no longer
// running) are silently overwritten.
package lock

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Lock represents an acquired PID file lock.
type Lock struct {
	path string
}

// Acquire writes the current PID to path. It returns an error if another
// running process already holds the lock. Stale PID files are overwritten
// after checking that the recorded PID is no longer alive.
func Acquire(path string) (*Lock, error) {
	ourPid := os.Getpid()

	if data, err := os.ReadFile(path); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if pidRunning(pid) {
				return nil, fmt.Errorf("another monitor instance is already running (pid %d, lock file %s)", pid, path)
			}
		}
		// Stale PID or unparseable — overwrite.
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(ourPid)+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("write pid file %s: %w", path, err)
	}
	return &Lock{path: path}, nil
}

// Release removes the PID file. Safe to call multiple times.
func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	l.path = ""
	return err
}

// pidRunning returns true if a process with the given PID exists.
func pidRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check liveness.
	return process.Signal(syscall.Signal(0)) == nil
}
