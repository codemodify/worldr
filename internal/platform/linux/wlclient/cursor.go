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
// Safe from the shell loop (takes mu). Must not be called from handle()
// while readLoop already holds mu.
func (w *Window) EnsureHostCursor() {
	w.SetHostCursorHidden(false)
}

// HideHostCursor is wl_pointer.set_cursor with a null surface (software
// cursor inside the nest framebuffer is showing a client image).
func (w *Window) HideHostCursor() {
	w.SetHostCursorHidden(true)
}

// SetHostCursorHidden toggles the host pointer image. No-ops when the
// visibility is already applied so the frame loop does not flood set_shape.
func (w *Window) SetHostCursorHidden(hide bool) {
	if w == nil || w.ptrID == 0 {
		return
	}
	w.mu.Lock()
	if w.hostCursorSet && w.hostCursorHidden == hide {
		w.mu.Unlock()
		return
	}
	w.hostCursorHidden = hide
	w.hostCursorSet = true
	ser, dev, surf := w.ptrSerial, w.cursorDev, w.cursorSurf
	w.mu.Unlock()
	if ser == 0 {
		return
	}
	w.sendHostCursor(ser, dev, surf, hide)
}

func encodeHostCursorShape(serial, shape uint32) []byte {
	p := wayland.PutU32(nil, serial)
	return wayland.PutU32(p, shape)
}

func encodeHostSetCursor(serial, surface uint32, hx, hy int32) []byte {
	p := wayland.PutU32(nil, serial)
	p = wayland.PutU32(p, surface)
	p = wayland.PutI32(p, hx)
	return wayland.PutI32(p, hy)
}

func (w *Window) sendHostCursor(serial, shapeDev, surf uint32, hide bool) {
	if hide {
		_ = w.send(w.ptrID, 0, encodeHostSetCursor(serial, 0, 0, 0), nil)
		return
	}
	if shapeDev != 0 {
		_ = w.send(shapeDev, 1, encodeHostCursorShape(serial, hostCursorShapeDefault), nil)
		return
	}
	if surf != 0 {
		_ = w.send(w.ptrID, 0, encodeHostSetCursor(serial, surf, 1, 1), nil)
	}
}
