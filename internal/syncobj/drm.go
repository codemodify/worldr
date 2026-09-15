//go:build linux

package syncobj

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const (
	drmCapSyncobj         = 0x13
	drmCapSyncobjTimeline = 0x14

	// _IOWR('d', nr, sizeof)
	ioctlGetCap                = 0xC010640C
	ioctlSyncobjFDToHandle     = 0xC01064C8
	ioctlSyncobjDestroy        = 0xC00864C6
	ioctlSyncobjTimelineWait   = 0xC02864CA
	ioctlSyncobjTimelineSignal = 0xC01864CD

	waitFlagWaitForSubmit = 1 << 1
)

type drmGetCap struct {
	Capability uint64
	Value      uint64
}

type drmSyncobjHandle struct {
	Handle uint32
	Flags  uint32
	FD     int32
	Pad    uint32
}

type drmSyncobjDestroy struct {
	Handle uint32
	Pad    uint32
}

type drmSyncobjTimelineWait struct {
	Handles       uint64
	Points        uint64
	TimeoutNsec   int64
	CountHandles  uint32
	Flags         uint32
	FirstSignaled uint32
	Pad           uint32
}

type drmSyncobjTimelineArray struct {
	Handles      uint64
	Points       uint64
	CountHandles uint32
	Flags        uint32
}

func closeFD(fd int) error {
	return syscall.Close(fd)
}

// TimelineAvailable is true when a local DRM node reports SYNCOBJ_TIMELINE.
func TimelineAvailable() bool {
	fd, err := openDRM()
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	return hasCap(fd, drmCapSyncobjTimeline)
}

func openDRM() (int, error) {
	matches, _ := filepath.Glob("/dev/dri/renderD*")
	cards, _ := filepath.Glob("/dev/dri/card*")
	matches = append(matches, cards...)
	for _, p := range matches {
		fd, err := syscall.Open(p, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
	}
	if _, err := os.Stat("/dev/dri"); err != nil {
		return -1, err
	}
	return -1, fmt.Errorf("no drm node")
}

func hasCap(fd int, cap uint64) bool {
	c := drmGetCap{Capability: cap}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlGetCap, uintptr(unsafe.Pointer(&c)))
	return errno == 0 && c.Value != 0
}

// WaitFence waits for an acquire timeline point. Failure is safe: the
// caller must fall back to implicit sync (Intel).
func WaitFence(f Fence, timeout time.Duration) error {
	if !f.Valid() {
		return fmt.Errorf("no syncobj fd")
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	drm, err := openDRM()
	if err != nil {
		return err
	}
	defer syscall.Close(drm)
	if !hasCap(drm, drmCapSyncobjTimeline) {
		return fmt.Errorf("no SYNCOBJ_TIMELINE")
	}
	h, err := fdToHandle(drm, f.FD)
	if err != nil {
		return err
	}
	defer destroyHandle(drm, h)
	handles := []uint32{h}
	points := []uint64{f.Point}
	req := drmSyncobjTimelineWait{
		Handles:      uint64(uintptr(unsafe.Pointer(&handles[0]))),
		Points:       uint64(uintptr(unsafe.Pointer(&points[0]))),
		TimeoutNsec:  time.Now().Add(timeout).UnixNano(),
		CountHandles: 1,
		Flags:        waitFlagWaitForSubmit,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(drm), ioctlSyncobjTimelineWait, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return errno
	}
	return nil
}

// SignalFence signals a release timeline point. Failure is a no-op for
// the caller (client may hang on that fence — documented).
func SignalFence(f Fence) error {
	if !f.Valid() {
		return fmt.Errorf("no syncobj fd")
	}
	drm, err := openDRM()
	if err != nil {
		return err
	}
	defer syscall.Close(drm)
	h, err := fdToHandle(drm, f.FD)
	if err != nil {
		return err
	}
	defer destroyHandle(drm, h)
	handles := []uint32{h}
	points := []uint64{f.Point}
	req := drmSyncobjTimelineArray{
		Handles:      uint64(uintptr(unsafe.Pointer(&handles[0]))),
		Points:       uint64(uintptr(unsafe.Pointer(&points[0]))),
		CountHandles: 1,
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(drm), ioctlSyncobjTimelineSignal, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return errno
	}
	return nil
}

func fdToHandle(drm, fd int) (uint32, error) {
	req := drmSyncobjHandle{FD: int32(fd)}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(drm), ioctlSyncobjFDToHandle, uintptr(unsafe.Pointer(&req)))
	if errno != 0 {
		return 0, errno
	}
	return req.Handle, nil
}

func destroyHandle(drm int, handle uint32) {
	req := drmSyncobjDestroy{Handle: handle}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(drm), ioctlSyncobjDestroy, uintptr(unsafe.Pointer(&req)))
}
