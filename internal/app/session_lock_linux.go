//go:build linux

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The persistent lock inode is never unlinked: another process may already be
// waiting on it. The kernel releases ownership on normal exit or a crash.
func lockSession(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fmt.Errorf("open workspace lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path+".lock")
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("workspace lock is not a regular file")
	}
	if err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("workspace is already open or cannot be locked: %w", err)
	}
	return file, nil
}
