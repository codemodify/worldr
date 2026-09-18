//go:build linux

package resourcepath

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func descriptorPath(file *os.File) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/self/fd/%d", file.Fd()))
}

func openResource(path string, directory bool) (*os.File, error) {
	if err := Validate(path); err != nil {
		return nil, err
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil && (directory && !info.IsDir() || !directory && !info.Mode().IsRegular()) {
		err = fmt.Errorf("saved resource has the wrong file type")
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
