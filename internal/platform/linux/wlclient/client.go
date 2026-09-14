//go:build linux

// Package wlclient is a debug-only nested Wayland *client* (wl_shm color fill).
//
// This is not the compositor path. It exists so worldr-shell can be tried
// inside an existing session without taking DRM master.
package wlclient

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

// Window is a nested xdg_toplevel filled with a solid color.
type Window struct {
	conn   *net.UnixConn
	rd     *wayland.Reader
	wr     *wayland.Writer
	nextID uint32

	reg, comp, shm, xdg, surf, xdgS, top, pool, buf uint32
	configured                                      bool
	ack                                             uint32
	w, h                                            int
	mem                                             []byte
	fd                                              int
	stride                                          int
}

// Open connects to WAYLAND_DISPLAY and maps a colored toplevel.
func Open(title string, w, h int, fullscreen bool) (*Window, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		return nil, fmt.Errorf("WAYLAND_DISPLAY is empty (nested debug backend needs a running compositor such as Hyprland/Sway)")
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return nil, fmt.Errorf("XDG_RUNTIME_DIR is empty (expected /run/user/%d)", os.Getuid())
	}
	addr := filepath.Join(dir, name)
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w (is the host compositor running?)", addr, err)
	}
	win := &Window{conn: c, rd: wayland.NewReader(c), wr: wayland.NewWriter(c), nextID: 1, w: w, h: h}
	if err := win.setup(title, fullscreen); err != nil {
		_ = c.Close()
		return nil, err
	}
	return win, nil
}

func (w *Window) alloc() uint32 {
	w.nextID++
	return w.nextID
}

func (w *Window) send(obj uint32, op uint16, payload []byte, fds []int) error {
	if payload == nil {
		payload = []byte{}
	}
	return w.wr.Send(obj, op, payload, fds)
}

func (w *Window) setup(title string, fullscreen bool) error {
	w.reg = w.alloc()
	if err := w.send(1, 1, wayland.PutU32(nil, w.reg), nil); err != nil { // get_registry
		return err
	}
	// roundtrip
	cb := w.alloc()
	if err := w.send(1, 0, wayland.PutU32(nil, cb), nil); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	var bound bool
	for time.Now().Before(deadline) {
		_ = w.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := w.rd.Next()
		if err != nil {
			if err == io.EOF {
				return err
			}
			continue
		}
		if msg.Object == w.reg && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			ver, _ := cur.U32()
			if ver > 4 {
				ver = 4
			}
			id := w.alloc()
			p := wayland.PutU32(nil, name)
			p = wayland.PutString(p, iface)
			p = wayland.PutU32(p, ver)
			p = wayland.PutU32(p, id)
			switch iface {
			case "wl_compositor":
				w.comp = id
				_ = w.send(w.reg, 0, p, nil)
			case "wl_shm":
				w.shm = id
				_ = w.send(w.reg, 0, p, nil)
			case "xdg_wm_base":
				w.xdg = id
				_ = w.send(w.reg, 0, p, nil)
			}
		}
		if msg.Object == cb && msg.Opcode == 0 {
			bound = true
			break
		}
		if msg.Object == w.xdg && msg.Opcode == 0 { // ping
			cur := wayland.NewCursor(msg.Payload, nil)
			ser, _ := cur.U32()
			_ = w.send(w.xdg, 3, wayland.PutU32(nil, ser), nil)
		}
	}
	if !bound || w.comp == 0 || w.shm == 0 || w.xdg == 0 {
		return fmt.Errorf("nested compositor missing wl_compositor/wl_shm/xdg_wm_base")
	}
	w.surf = w.alloc()
	if err := w.send(w.comp, 0, wayland.PutU32(nil, w.surf), nil); err != nil {
		return err
	}
	w.xdgS = w.alloc()
	p := wayland.PutU32(nil, w.xdgS)
	p = wayland.PutU32(p, w.surf)
	if err := w.send(w.xdg, 2, p, nil); err != nil {
		return err
	}
	w.top = w.alloc()
	if err := w.send(w.xdgS, 1, wayland.PutU32(nil, w.top), nil); err != nil {
		return err
	}
	if err := w.send(w.top, 2, wayland.PutString(nil, title), nil); err != nil {
		return err
	}
	if fullscreen {
		_ = w.send(w.top, 11, wayland.PutU32(nil, 0), nil)
	}
	if err := w.send(w.surf, 6, nil, nil); err != nil { // commit to get configure
		return err
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !w.configured {
		_ = w.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := w.rd.Next()
		if err != nil {
			continue
		}
		w.handle(msg)
	}
	if !w.configured {
		// some compositors configure immediately; proceed anyway
		w.ack = 1
	}
	if err := w.allocSHM(); err != nil {
		return err
	}
	if err := w.send(w.xdgS, 4, wayland.PutU32(nil, w.ack), nil); err != nil {
		return err
	}
	p = wayland.PutU32(nil, w.buf)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, 0)
	if err := w.send(w.surf, 1, p, nil); err != nil {
		return err
	}
	return w.send(w.surf, 6, nil, nil)
}

func (w *Window) allocSHM() error {
	w.stride = w.w * 4
	size := w.stride * w.h
	fd, err := unix.MemfdCreate("worldr-wlclient", 0)
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
	w.fd, w.mem = fd, mem
	w.pool = w.alloc()
	p := wayland.PutU32(nil, w.pool)
	p = wayland.PutI32(p, int32(size))
	if err := w.send(w.shm, 0, p, []int{fd}); err != nil {
		return err
	}
	w.buf = w.alloc()
	p = wayland.PutU32(nil, w.buf)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, int32(w.w))
	p = wayland.PutI32(p, int32(w.h))
	p = wayland.PutI32(p, int32(w.stride))
	p = wayland.PutU32(p, 1) // XRGB8888
	return w.send(w.pool, 0, p, nil)
}

func (w *Window) handle(msg wayland.Message) {
	if msg.Object == w.xdg && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		ser, _ := cur.U32()
		_ = w.send(w.xdg, 3, wayland.PutU32(nil, ser), nil)
	}
	if msg.Object == w.xdgS && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		ser, _ := cur.U32()
		w.ack = ser
		w.configured = true
	}
	if msg.Object == w.top && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		nw, _ := cur.I32()
		nh, _ := cur.I32()
		if nw > 0 && nh > 0 {
			w.w, w.h = int(nw), int(nh)
		}
	}
}

// Size of the client buffer.
func (w *Window) Size() (width, height, stride int) {
	return w.w, w.h, w.stride
}

// Present copies BGRA into the shm buffer and commits.
func (w *Window) Present(bgra []byte, stride int) error {
	if w.mem == nil {
		return fmt.Errorf("no shm")
	}
	h := w.h
	for y := 0; y < h; y++ {
		src := y * stride
		dst := y * w.stride
		n := w.w * 4
		if src+n > len(bgra) || dst+n > len(w.mem) {
			break
		}
		copy(w.mem[dst:dst+n], bgra[src:src+n])
	}
	p := wayland.PutU32(nil, w.buf)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, 0)
	if err := w.send(w.surf, 1, p, nil); err != nil {
		return err
	}
	if err := w.send(w.surf, 6, nil, nil); err != nil {
		return err
	}
	_ = w.conn.SetReadDeadline(time.Now().Add(2 * time.Millisecond))
	for {
		msg, err := w.rd.Next()
		if err != nil {
			return nil
		}
		w.handle(msg)
	}
}

func (w *Window) Close() {
	if w == nil {
		return
	}
	if w.mem != nil {
		_ = syscall.Munmap(w.mem)
	}
	if w.fd > 0 {
		_ = syscall.Close(w.fd)
	}
	if w.conn != nil {
		_ = w.conn.Close()
	}
}
