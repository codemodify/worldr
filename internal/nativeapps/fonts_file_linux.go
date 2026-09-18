//go:build linux

package nativeapps

import (
	"os"

	"golang.org/x/sys/unix"
)

func openFallbackFont(path string) (*os.File, error) {
	// Reject non-regular files after opening without waiting on a named pipe.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
