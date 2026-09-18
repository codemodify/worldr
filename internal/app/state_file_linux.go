//go:build linux

package app

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openStateFile(path string) (*os.File, error) {
	// Nonblocking open also protects --fresh and files replaced after the
	// recovery preflight from blocking the host on a FIFO.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("state must be a regular file")
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
