//go:build linux

package wlclient

import (
	"syscall"

	"github.com/codemodify/worldr/internal/decorations"
	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

const (
	hostCursorW = 16
	hostCursorH = 24
	// wp_cursor_shape_device_v1.shape "default"
	hostCursorShapeDefault uint32 = 1
)

func (w *Window) setupHostCursor() error {
	if w.cursorMgr != 0 && w.ptrID != 0 {
		w.cursorDev = w.alloc()
		p := wayland.PutU32(nil, w.cursorDev)
		p = wayland.PutU32(p, w.ptrID)
		if err := w.send(w.cursorMgr, 1, p, nil); err != nil { // get_pointer
			return err
		}
		logClient("cursor_shape device id=%d pointer=%d", w.cursorDev, w.ptrID)
		return nil
	}
	return w.allocHostCursorSHM()
}

func (w *Window) allocHostCursorSHM() error {
	if w.comp == 0 || w.shm == 0 {
		return nil
	}
	stride := hostCursorW * 4
	size := stride * hostCursorH
	fd, err := unix.MemfdCreate("worldr-host-cursor", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		_ = syscall.Close(fd)
		return err
	}
	mem, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Close(fd)
		return err
	}
	for i := range mem {
		mem[i] = 0
	}
	decorations.DrawPointer(mem, stride, hostCursorW, hostCursorH, 1, 1)

	w.cursorPool = w.alloc()
	p := wayland.PutU32(nil, w.cursorPool)
	p = wayland.PutI32(p, int32(size))
	if err := w.send(w.shm, 0, p, []int{fd}); err != nil {
		_ = syscall.Munmap(mem)
		_ = syscall.Close(fd)
		return err
	}
	_ = syscall.Close(fd)

	w.cursorBuf = w.alloc()
	p = wayland.PutU32(nil, w.cursorBuf)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, hostCursorW)
	p = wayland.PutI32(p, hostCursorH)
	p = wayland.PutI32(p, int32(stride))
	p = wayland.PutU32(p, 0) // ARGB8888
	if err := w.send(w.cursorPool, 0, p, nil); err != nil {
		return err
	}

	w.cursorSurf = w.alloc()
	if err := w.send(w.comp, 0, wayland.PutU32(nil, w.cursorSurf), nil); err != nil {
		return err
	}
	p = wayland.PutU32(nil, w.cursorBuf)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, 0)
	if err := w.send(w.cursorSurf, 1, p, nil); err != nil { // attach
		return err
	}
	if err := w.send(w.cursorSurf, 6, nil, nil); err != nil { // commit
		return err
	}
	w.cursorMem = mem
	logClient("host shm cursor surface=%d %dx%d", w.cursorSurf, hostCursorW, hostCursorH)
	return nil
}

// EnsureHostCursor shows a default arrow on the nest surface (cursor-shape
// or shm set_cursor). Plasma hides the pointer until the client sets one.
func (w *Window) EnsureHostCursor() {
	if w == nil || w.ptrID == 0 {
		return
	}
	w.mu.Lock()
	w.hostCursorHidden = false
	ser := w.ptrSerial
	dev := w.cursorDev
	surf := w.cursorSurf
	w.mu.Unlock()
	w.sendHostCursor(ser, dev, surf, false)
}

// HideHostCursor is wl_pointer.set_cursor with a null surface (software
// cursor inside the nest framebuffer is showing a client image).
func (w *Window) HideHostCursor() {
	if w == nil || w.ptrID == 0 {
		return
	}
	w.mu.Lock()
	w.hostCursorHidden = true
	ser := w.ptrSerial
	w.mu.Unlock()
	w.sendHostCursor(ser, 0, 0, true)
}

func (w *Window) sendHostCursor(serial, shapeDev, surf uint32, hide bool) {
	if hide {
		p := wayland.PutU32(nil, serial)
		p = wayland.PutU32(p, 0)
		p = wayland.PutI32(p, 0)
		p = wayland.PutI32(p, 0)
		_ = w.send(w.ptrID, 0, p, nil)
		return
	}
	if shapeDev != 0 {
		p := wayland.PutU32(nil, serial)
		p = wayland.PutU32(p, hostCursorShapeDefault)
		_ = w.send(shapeDev, 1, p, nil) // set_shape
		return
	}
	if surf != 0 {
		p := wayland.PutU32(nil, serial)
		p = wayland.PutU32(p, surf)
		p = wayland.PutI32(p, 1)
		p = wayland.PutI32(p, 1)
		_ = w.send(w.ptrID, 0, p, nil)
	}
}
