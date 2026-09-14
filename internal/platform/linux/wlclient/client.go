//go:build linux

// Package wlclient is a debug-only nested Wayland *client* (wl_shm color fill).
//
// This is not the compositor path. It exists so worldr-shell can be tried
// inside an existing session without taking DRM master.
//
// KWin/Plasma is strict: attach only after a real xdg_surface.configure,
// never reuse a busy wl_buffer, and always pong xdg_wm_base.ping. Missing
// any of those closes the socket; the next commit then fails with
// `write unix @: sendmsg: broken pipe`.
package wlclient

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

const (
	nSlots = 2

	ifaceCompositor = "wl_compositor"
	ifaceShm        = "wl_shm"
	ifaceXdg        = "xdg_wm_base"
)

// Window is a nested xdg_toplevel filled with a solid color.
type Window struct {
	mu     sync.Mutex
	conn   *net.UnixConn
	rd     *wayland.Reader
	wr     *wayland.Writer
	nextID uint32

	reg, comp, shm, xdg, surf, xdgS, top uint32
	compVer, shmVer, xdgVer              uint32

	configured bool
	needAck    bool
	ack        uint32
	closed     bool
	fatal      error

	w, h           int
	pendingW       int
	pendingH       int
	stride         int
	slots          [nSlots]shmSlot
	frameDone      bool
	frameCB        uint32
	stop           chan struct{}
	stopped        sync.Once
	readerFinished chan struct{}
}

type shmSlot struct {
	id    uint32
	pool  uint32
	fd    int
	mem   []byte
	busy  bool
	alive bool
}

// displaySocket resolves WAYLAND_DISPLAY against XDG_RUNTIME_DIR.
func displaySocket() (string, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		return "", fmt.Errorf("WAYLAND_DISPLAY is empty (nested debug backend needs a running compositor such as KWin/Hyprland/Sway)")
	}
	if strings.HasPrefix(name, "/") {
		return name, nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR is empty (expected /run/user/%d)", os.Getuid())
	}
	return filepath.Join(dir, name), nil
}

// Open connects to WAYLAND_DISPLAY and maps a colored toplevel.
func Open(title string, w, h int, fullscreen bool) (*Window, error) {
	addr, err := displaySocket()
	if err != nil {
		return nil, err
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w (is the host compositor running?)", addr, err)
	}
	win := &Window{
		conn:           c,
		rd:             wayland.NewReader(c),
		wr:             wayland.NewWriter(c),
		nextID:         1,
		w:              w,
		h:              h,
		frameDone:      true,
		stop:           make(chan struct{}),
		readerFinished: make(chan struct{}),
	}
	if err := win.setup(title, fullscreen); err != nil {
		_ = c.Close()
		return nil, err
	}
	go win.readLoop()
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
	err := w.wr.Send(obj, op, payload, fds)
	if err != nil {
		return wrapHostClose(err)
	}
	return nil
}

func (w *Window) setup(title string, fullscreen bool) error {
	w.reg = w.alloc()
	if err := w.send(1, 1, wayland.PutU32(nil, w.reg), nil); err != nil { // get_registry
		return err
	}
	globals, err := w.roundtripCollect()
	if err != nil {
		return err
	}
	if err := w.bindNeeded(globals); err != nil {
		return err
	}
	// Flush host errors (invalid bind shows up as wl_display.error) before
	// creating surfaces — otherwise the next write is a broken pipe.
	if err := w.roundtrip(); err != nil {
		return err
	}
	if w.comp == 0 || w.shm == 0 || w.xdg == 0 {
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
	if err := w.send(w.top, 3, wayland.PutString(nil, "worldr-shell"), nil); err != nil {
		return err
	}
	if fullscreen {
		_ = w.send(w.top, 11, wayland.PutU32(nil, 0), nil)
	}
	// First commit has no buffer. KWin/Plasma (and xdg_shell) reply with configure.
	if err := w.send(w.surf, 6, nil, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	_ = w.conn.SetReadDeadline(deadline)
	for !w.configured && time.Now().Before(deadline) {
		msg, err := w.rd.Next()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			return fmt.Errorf("wait xdg_surface.configure: %w", wrapHostClose(err))
		}
		if err := w.handle(msg); err != nil {
			return err
		}
	}
	_ = w.conn.SetReadDeadline(time.Time{})
	if !w.configured {
		return fmt.Errorf("host compositor sent no xdg_surface.configure before first attach (KWin/Plasma requires this). Workaround: spare TTY --backend=vk-display --duration=15s")
	}
	if w.pendingW > 0 && w.pendingH > 0 {
		w.w, w.h = w.pendingW, w.pendingH
	}
	return w.allocSHM()
}

func (w *Window) roundtripCollect() ([]registryGlobal, error) {
	var globals []registryGlobal
	cb := w.alloc()
	if err := w.send(1, 0, wayland.PutU32(nil, cb), nil); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = w.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := w.rd.Next()
		if err != nil {
			if err == io.EOF {
				return nil, wrapHostClose(err)
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return nil, wrapHostClose(err)
		}
		if err := w.handle(msg); err != nil {
			return nil, err
		}
		if msg.Object == w.reg && msg.Opcode == 0 {
			g, perr := parseRegistryGlobal(msg.Payload)
			if perr != nil {
				logClient("bad registry.global: %v", perr)
				continue
			}
			logGlobal(g)
			globals = append(globals, g)
		}
		if msg.Object == cb && msg.Opcode == 0 {
			return globals, nil
		}
	}
	return nil, fmt.Errorf("registry roundtrip timeout")
}

func (w *Window) roundtrip() error {
	cb := w.alloc()
	if err := w.send(1, 0, wayland.PutU32(nil, cb), nil); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = w.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := w.rd.Next()
		if err != nil {
			if err == io.EOF {
				return wrapHostClose(err)
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return wrapHostClose(err)
		}
		if err := w.handle(msg); err != nil {
			return err
		}
		if msg.Object == cb && msg.Opcode == 0 {
			return nil
		}
	}
	return fmt.Errorf("display.sync roundtrip timeout")
}

func (w *Window) bindNeeded(globals []registryGlobal) error {
	seen := map[string]bool{}
	for _, g := range globals {
		req, ok := clampBindVersion(g.iface, g.advertised)
		if !ok {
			continue
		}
		if seen[g.iface] {
			logClient("skip extra global name=%d iface=%s advertised=%d (already bound)", g.name, g.iface, g.advertised)
			continue
		}
		if err := w.bindOne(g, req); err != nil {
			return err
		}
		seen[g.iface] = true
	}
	return nil
}

func (w *Window) bindOne(g registryGlobal, requested uint32) error {
	id := w.alloc()
	p, err := encodeRegistryBind(g.name, g.iface, requested, id)
	if err != nil {
		return err
	}
	logBind(g, requested, id)
	if err := w.send(w.reg, 0, p, nil); err != nil {
		return err
	}
	switch g.iface {
	case ifaceCompositor:
		w.comp = id
		w.compVer = requested
	case ifaceShm:
		w.shm = id
		w.shmVer = requested
	case ifaceXdg:
		w.xdg = id
		w.xdgVer = requested
	}
	return nil
}

// BoundVersions reports the versions actually sent in wl_registry.bind.
func (w *Window) BoundVersions() (compositor, shm, xdg uint32) {
	return w.compVer, w.shmVer, w.xdgVer
}

func (w *Window) allocSHM() error {
	w.freeSlots()
	if w.w < 1 {
		w.w = 1
	}
	if w.h < 1 {
		w.h = 1
	}
	w.stride = w.w * 4
	size := w.stride * w.h
	for i := range w.slots {
		if err := w.allocSlot(i, size); err != nil {
			w.freeSlots()
			return err
		}
	}
	return nil
}

func (w *Window) allocSlot(i int, size int) error {
	fd, err := unix.MemfdCreate(fmt.Sprintf("worldr-wlclient-%d", i), unix.MFD_CLOEXEC)
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
	s := &w.slots[i]
	s.fd, s.mem = fd, mem
	s.pool = w.alloc()
	p := wayland.PutU32(nil, s.pool)
	p = wayland.PutI32(p, int32(size))
	if err := w.send(w.shm, 0, p, []int{fd}); err != nil {
		_ = syscall.Munmap(mem)
		_ = syscall.Close(fd)
		return err
	}
	s.id = w.alloc()
	p = wayland.PutU32(nil, s.id)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, int32(w.w))
	p = wayland.PutI32(p, int32(w.h))
	p = wayland.PutI32(p, int32(w.stride))
	p = wayland.PutU32(p, 1) // XRGB8888
	if err := w.send(s.pool, 0, p, nil); err != nil {
		return err
	}
	s.busy = false
	s.alive = true
	return nil
}

func (w *Window) freeSlots() {
	for i := range w.slots {
		s := &w.slots[i]
		if s.id != 0 {
			_ = w.send(s.id, 0, nil, nil)
		}
		if s.pool != 0 {
			_ = w.send(s.pool, 1, nil, nil)
		}
		if s.mem != nil {
			_ = syscall.Munmap(s.mem)
		}
		if s.fd > 0 {
			_ = syscall.Close(s.fd)
		}
		w.slots[i] = shmSlot{}
	}
}

func (w *Window) handle(msg wayland.Message) error {
	if msg.Object == 1 && msg.Opcode == 0 { // wl_display.error
		cur := wayland.NewCursor(msg.Payload, nil)
		obj, _ := cur.U32()
		code, _ := cur.U32()
		m, _ := cur.String()
		err := fmt.Errorf("wl_display.error object=%d code=%d: %s", obj, code, m)
		w.fatal = err
		return wrapHostClose(err)
	}
	if msg.Object == w.xdg && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		ser, _ := cur.U32()
		return w.send(w.xdg, 3, wayland.PutU32(nil, ser), nil)
	}
	if msg.Object == w.xdgS && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		ser, _ := cur.U32()
		w.ack = ser
		w.needAck = true
		w.configured = true
	}
	if msg.Object == w.top && msg.Opcode == 0 {
		cur := wayland.NewCursor(msg.Payload, nil)
		nw, _ := cur.I32()
		nh, _ := cur.I32()
		if nw > 0 && nh > 0 {
			w.pendingW, w.pendingH = int(nw), int(nh)
		}
	}
	if msg.Object == w.top && msg.Opcode == 1 { // close
		w.closed = true
	}
	for i := range w.slots {
		if w.slots[i].alive && msg.Object == w.slots[i].id && msg.Opcode == 0 {
			w.slots[i].busy = false
		}
	}
	if w.frameCB != 0 && msg.Object == w.frameCB && msg.Opcode == 0 {
		w.frameDone = true
		w.frameCB = 0
	}
	return w.fatal
}

func (w *Window) readLoop() {
	defer close(w.readerFinished)
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		msg, err := w.rd.Next()
		if err != nil {
			w.mu.Lock()
			if w.fatal == nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
				w.fatal = wrapHostClose(err)
			} else if w.fatal == nil && (errors.Is(err, io.EOF) || isBrokenPipe(err)) {
				w.fatal = wrapHostClose(err)
			}
			w.mu.Unlock()
			return
		}
		w.mu.Lock()
		_ = w.handle(msg)
		w.mu.Unlock()
	}
}

func (w *Window) pickSlot() *shmSlot {
	for i := range w.slots {
		if w.slots[i].alive && !w.slots[i].busy {
			return &w.slots[i]
		}
	}
	return nil
}

// Size of the client buffer.
func (w *Window) Size() (width, height, stride int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w, w.h, w.stride
}

// Present copies BGRA into a free shm buffer and commits.
func (w *Window) Present(bgra []byte, stride int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fatal != nil {
		return w.fatal
	}
	if w.closed {
		return fmt.Errorf("host compositor closed the nested window")
	}
	if !w.configured {
		return fmt.Errorf("xdg_surface not configured")
	}
	if w.pendingW > 0 && w.pendingH > 0 && (w.pendingW != w.w || w.pendingH != w.h) {
		w.w, w.h = w.pendingW, w.pendingH
		if err := w.allocSHM(); err != nil {
			return err
		}
	}
	slot := w.pickSlot()
	if slot == nil {
		// Both buffers still held by the host — skip the frame rather than
		// attaching a busy wl_buffer (KWin treats that as a protocol error).
		return nil
	}
	if !w.frameDone {
		return nil
	}
	h := w.h
	for y := 0; y < h; y++ {
		src := y * stride
		dst := y * w.stride
		n := w.w * 4
		if src+n > len(bgra) || dst+n > len(slot.mem) {
			break
		}
		copy(slot.mem[dst:dst+n], bgra[src:src+n])
	}
	if w.needAck {
		if err := w.send(w.xdgS, 4, wayland.PutU32(nil, w.ack), nil); err != nil {
			return err
		}
		w.needAck = false
	}
	p := wayland.PutU32(nil, slot.id)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, 0)
	if err := w.send(w.surf, 1, p, nil); err != nil { // attach
		return err
	}
	if w.compVer >= 4 {
		p = wayland.PutI32(nil, 0)
		p = wayland.PutI32(p, 0)
		p = wayland.PutI32(p, int32(w.w))
		p = wayland.PutI32(p, int32(w.h))
		if err := w.send(w.surf, 9, p, nil); err != nil { // damage_buffer
			return err
		}
	} else {
		p = wayland.PutI32(nil, 0)
		p = wayland.PutI32(p, 0)
		p = wayland.PutI32(p, int32(w.w))
		p = wayland.PutI32(p, int32(w.h))
		if err := w.send(w.surf, 2, p, nil); err != nil { // damage
			return err
		}
	}
	cb := w.alloc()
	if err := w.send(w.surf, 3, wayland.PutU32(nil, cb), nil); err != nil { // frame
		return err
	}
	w.frameCB = cb
	w.frameDone = false
	if err := w.send(w.surf, 6, nil, nil); err != nil {
		return err
	}
	slot.busy = true
	return nil
}

func (w *Window) Close() {
	if w == nil {
		return
	}
	w.stopped.Do(func() {
		close(w.stop)
		w.mu.Lock()
		w.freeSlots()
		w.mu.Unlock()
		if w.conn != nil {
			_ = w.conn.Close()
		}
		select {
		case <-w.readerFinished:
		case <-time.After(200 * time.Millisecond):
		}
	})
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "broken pipe") || strings.Contains(s, "connection reset")
}

func wrapHostClose(err error) error {
	if err == nil {
		return nil
	}
	if !isBrokenPipe(err) && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "wl_display.error") {
		return err
	}
	return fmt.Errorf("%w\n\nKWin/Plasma nested-window note: the host compositor closed the socket. abox saw `invalid arguments for wl_registry#2.bind` — that is a bad bind (version 0, version above advertised, or interface/name mismatch), not a random I/O flake. worldr now collects globals, binds only wl_compositor/wl_shm/xdg_wm_base at min(our_max, advertised), and logs each advertise/bind on stderr. Also waits for xdg_surface.configure and double-buffers shm. If this still happens, paste the wayland-client: global/bind lines and use a spare TTY: --backend=vk-display --duration=15s.", err)
}
