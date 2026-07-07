// Package lock provides a PID-file based single-instance lock.
//
// Acquire writes the current PID to a lock file and returns an error if
// another running instance already holds the lock. Stale PID files (PID
// not running) are silently overwritten. Release removes the lock file.
package lock

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// Lock holds a PID-file lock.
type Lock struct {
	path string
}

// Acquire writes the current PID to path and returns a Lock. If path already
// exists and contains a PID of a running process, Acquire returns an error.
// If the PID file is stale (the PID is not running), it is overwritten.
func Acquire(path string) (*Lock, error) {
	if data, err := os.ReadFile(path); err == nil {
		pid, err := strconv.Atoi(string(data))
		if err == nil && processRunning(pid) {
			return nil, fmt.Errorf("another instance is already running (pid %d, lock %s)", pid, path)
		}
		// Stale PID file or unparseable content — fall through and overwrite.
	}

	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return nil, fmt.Errorf("write pid file %s: %w", path, err)
	}

	return &Lock{path: path}, nil
}

// Release removes the PID file. It is safe to call multiple times.
func (l *Lock) Release() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// processRunning reports whether a process with the given PID is currently
// running by sending signal 0.
func processRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
