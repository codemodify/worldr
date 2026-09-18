//go:build linux

package app

import "golang.org/x/sys/unix"

func duplicateClipboardFD(fd int) (int, error) {
	owned, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if err := unix.SetNonblock(owned, true); err != nil {
		unix.Close(owned)
		return -1, err
	}
	return owned, nil
}
