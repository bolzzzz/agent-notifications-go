//go:build !windows

package teamstate

import (
	"fmt"
	"os"
	"syscall"
)

// withFileLock executes fn while holding an exclusive file lock (flock).
// This ensures cross-process safety between Stop and TeammateIdle hooks.
func withFileLock(teamName string, fn func() error) error {
	path := lockPath(teamName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}
