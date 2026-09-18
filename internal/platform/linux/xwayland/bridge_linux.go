//go:build linux && cgo

package xwayland

/*
#cgo pkg-config: xcb xcb-composite xcb-res xcb-xfixes
#include "xwm.h"
#include <stdlib.h>
*/
import "C"

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"golang.org/x/sys/unix"
)

type command struct {
	kind          byte
	id            uint32
	width, height int
	transferID    uint64
	offerID       uint64
	externalID    uint64
	target        uint32
	timestamp     uint32
	fd            int
	mime          string
	mimes         []string
	data          []byte
	ok            bool
	done          chan error
}

type clipboardRequestMeta struct {
	id, externalID uint64
	mime           string
}

// Bridge belongs to the application's host goroutine. Its XCB connection has a
// separate single owner; no apps.Server method runs on that thread.
type Bridge struct {
	server                     *apps.Server
	cmd                        *exec.Cmd
	dir, authority, display    string
	control                    *os.File
	commands                   chan command
	stop, ownerDone, childDone chan struct{}
	ready                      chan error
	mu                         sync.Mutex
	snapshot                   []Window
	clipboard                  ClipboardOffer
	drag                       XDNDOffer
	clipboardRequests          []clipboardRequestMeta
	clipboardSerial            uint64
	ownerErr, childErr         error
	windows                    map[uint32]Window
	accelerated                bool
	renderNode                 string
	closed                     bool
	closeErr                   error
	log                        boundedLog
}

type xwaylandTransport uint8

const (
	transportSHM xwaylandTransport = iota
	transportGlamor
)

// Open starts an isolated Xwayland. It selects glamor only when Xwayland
// supports it and the application server advertises importable ARGB/XRGB
// DMA-BUFs on an accessible render node. A failed glamor startup is retried
// once with shared memory. Startup pumps the supplied Wayland server on this
// caller's goroutine and has an eight-second deadline.
func Open(server *apps.Server, output io.Writer) (*Bridge, error) {
	if server == nil || server.Socket() == "" {
		return nil, errors.New("Xwayland requires a live private application server")
	}
	if _, err := server.Poll(); err != nil {
		return nil, fmt.Errorf("Xwayland application server: %w", err)
	}
	path, err := exec.LookPath("Xwayland")
	if err != nil {
		return nil, fmt.Errorf("find Xwayland: %w", err)
	}
	formats, renderNode := server.DMABufCapabilities()
	transport := chooseXwaylandTransport(formats, renderNode,
		xwaylandSupportsGlamor(path), renderNodeUsable(renderNode))
	b, acceleratedErr := openXwayland(server, path, transport, renderNode)
	if transport == transportGlamor && b != nil && glamorFellBack(b.log.String()) {
		acceleratedErr = fmt.Errorf("Xwayland disabled glamor: %s", b.log.String())
		_ = b.Close()
		b = nil
	}
	if transport == transportGlamor && acceleratedErr != nil {
		b, err = openXwayland(server, path, transportSHM, "")
		if err != nil {
			return nil, errors.Join(acceleratedErr, fmt.Errorf("software Xwayland fallback: %w", err))
		}
		if output != nil {
			fmt.Fprintf(output, "Xwayland: private %s · software shared-memory buffers (glamor unavailable)\n", b.display)
		}
		return b, nil
	}
	if acceleratedErr != nil {
		return nil, acceleratedErr
	}
	if output != nil {
		if b.accelerated {
			fmt.Fprintf(output, "Xwayland: private %s · GPU DMA-BUF buffers (glamor on %s)\n", b.display, b.renderNode)
		} else {
			fmt.Fprintf(output, "Xwayland: private %s · software shared-memory buffers\n", b.display)
		}
	}
	return b, nil
}

func openXwayland(server *apps.Server, path string, transport xwaylandTransport, renderNode string) (_ *Bridge, err error) {
	dir, err := os.MkdirTemp("", "worldr-x11-")
	if err != nil {
		return nil, err
	}
	b := &Bridge{server: server, dir: dir, authority: filepath.Join(dir, "authority"), commands: make(chan command, 64), stop: make(chan struct{}), ownerDone: make(chan struct{}), childDone: make(chan struct{}), ready: make(chan error, 1), windows: make(map[uint32]Window), accelerated: transport == transportGlamor, renderNode: renderNode}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	var cookie [16]byte
	if _, err = rand.Read(cookie[:]); err != nil {
		return nil, err
	}
	// Xserver loads the cookie by protocol name, independently of the record's
	// address/display fields (os/auth.c:LoadAuthorization). Client matching gets
	// the actual display number atomically once -displayfd reports readiness.
	if err = writeAuthority(b.authority, "", cookie[:]); err != nil {
		return nil, err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	defer writer.Close()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fds[0]), "XWM")
	defer parent.Close()
	child := os.NewFile(uintptr(fds[1]), "Xwayland-WM")
	defer child.Close()
	controlFD, err := unix.FcntlInt(parent.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	b.control = os.NewFile(uintptr(controlFD), "XWM-cancel")
	ownerFD, err := unix.FcntlInt(parent.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	b.cmd = exec.Command(path, xwaylandArguments(b.authority, transport)...)
	b.cmd.ExtraFiles = []*os.File{writer, child}
	b.cmd.Env = xwaylandEnvironment(os.Environ(), server.Socket(), "", b.authority, b.accelerated)
	b.cmd.Stdout, b.cmd.Stderr = &b.log, &b.log
	if err = b.cmd.Start(); err != nil {
		unix.Close(ownerFD)
		b.cmd = nil
		return nil, fmt.Errorf("start Xwayland: %w", err)
	}
	writer.Close()
	child.Close()
	parent.Close()
	go func() { b.childErr = b.cmd.Wait(); close(b.childDone) }()
	go b.ownXWM(ownerFD)
	type displayResult struct {
		number string
		err    error
	}
	displayReady := make(chan displayResult, 1)
	go func() { n, e := readDisplay(reader); displayReady <- displayResult{n, e} }()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	wmReady := false
	for b.display == "" || !wmReady {
		select {
		case r := <-displayReady:
			if r.err != nil {
				return nil, fmt.Errorf("Xwayland readiness: %w: %s", r.err, b.log.String())
			}
			b.display = ":" + r.number
			if err = writeAuthority(b.authority, r.number, cookie[:]); err != nil {
				return nil, err
			}
		case e := <-b.ready:
			if e != nil {
				return nil, e
			}
			wmReady = true
		case <-b.childDone:
			return nil, fmt.Errorf("Xwayland exited during startup: %v: %s", b.childErr, b.log.String())
		case <-timer.C:
			return nil, fmt.Errorf("Xwayland startup timed out: %s", b.log.String())
		case <-tick.C:
			if _, err = server.Poll(); err != nil {
				return nil, fmt.Errorf("Xwayland startup dispatch: %w", err)
			}
		}
	}
	return b, nil
}

func xwaylandArguments(authority string, transport xwaylandTransport) []string {
	args := []string{"-rootless", "-noreset", "-nolisten", "tcp"}
	if transport == transportGlamor {
		args = append(args, "-glamor", "gl")
	} else {
		args = append(args, "-shm")
	}
	return append(args, "-auth", authority, "-displayfd", "3", "-wm", "4")
}

func chooseXwaylandTransport(formats []dmabuf.Format, renderNode string, glamorSupported, nodeUsable bool) xwaylandTransport {
	if renderNode == "" || !glamorSupported || !nodeUsable || !x11DMABufFormats(formats) {
		return transportSHM
	}
	return transportGlamor
}

func x11DMABufFormats(formats []dmabuf.Format) bool {
	xrgb := make(map[uint64]bool)
	for _, format := range formats {
		if format.FourCC == dmabuf.XRGB8888 && format.Modifier != dmabuf.Invalid {
			xrgb[format.Modifier] = true
		}
	}
	for _, format := range formats {
		if format.FourCC == dmabuf.ARGB8888 && xrgb[format.Modifier] {
			return true
		}
	}
	return false
}

func xwaylandSupportsGlamor(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, path, "-help").CombinedOutput()
	return ctx.Err() == nil && bytes.Contains(out, []byte("-glamor"))
}

func renderNodeUsable(path string) bool {
	if path == "" {
		return false
	}
	var stat unix.Stat_t
	if unix.Stat(path, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR {
		return false
	}
	if unix.Major(uint64(stat.Rdev)) != 226 {
		return false
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	unix.Close(fd)
	return true
}

func glamorFellBack(log string) bool {
	log = strings.ToLower(log)
	for _, marker := range []string{"disabling glamor", "failed to initialize glamor", "falling back to sw", "gbm wayland interfaces not available", "no main linux-dmabuf device advertised", "main linux-dmabuf device has no render node"} {
		if strings.Contains(log, marker) {
			return true
		}
	}
	return false
}

func xwaylandEnvironment(parent []string, socket, display, authority string, accelerated bool) []string {
	env := privateEnvironment(parent, socket, display, authority)
	if !accelerated {
		return env
	}
	out := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name != "DRI_PRIME" {
			out = append(out, entry)
		}
	}
	return out
}

func readDisplay(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 32)).ReadString('\n')
	if err != nil {
		return "", err
	}
	number := strings.TrimSuffix(line, "\n")
	if number == "" || len(number) > 5 {
		return "", errors.New("invalid display number")
	}
	for _, r := range number {
		if r < '0' || r > '9' {
			return "", errors.New("invalid display number")
		}
	}
	n, err := strconv.Atoi(number)
	if err != nil || n > 65535 {
		return "", errors.New("invalid display number")
	}
	return strconv.Itoa(n), nil
}

func writeAuthority(path, number string, cookie []byte) error {
	var data bytes.Buffer
	binary.Write(&data, binary.BigEndian, uint16(65535)) // FamilyWild, private file.
	for _, p := range [][]byte{nil, []byte(number), []byte("MIT-MAGIC-COOKIE-1"), cookie} {
		binary.Write(&data, binary.BigEndian, uint16(len(p)))
		data.Write(p)
	}
	if err := os.WriteFile(path+".new", data.Bytes(), 0600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

func (b *Bridge) ownXWM(fd int) {
	defer close(b.ownerDone)
	var message [256]C.char
	x := C.worldr_xwm_open(C.int(fd), &message[0], 256)
	if x == nil {
		b.ready <- fmt.Errorf("Xwayland XWM: %s", C.GoString(&message[0]))
		return
	}
	defer C.worldr_xwm_close(x)
	b.ready <- nil
	tick := time.NewTicker(4 * time.Millisecond)
	defer tick.Stop()
	var receiveQueue []command
	var active command
	var activeSince time.Time
	activeReceive := false
	var dndDroppedAt time.Time
	defer func() {
		if activeReceive {
			closeClipboardFD(active.fd)
		}
		for _, pending := range receiveQueue {
			closeClipboardFD(pending.fd)
		}
		for {
			select {
			case pending := <-b.commands:
				if pending.kind == 'g' {
					closeClipboardFD(pending.fd)
				}
			default:
				return
			}
		}
	}()
	for {
		poll := false
		select {
		case <-b.stop:
			return
		case c := <-b.commands:
			switch c.kind {
			case 'f':
				C.worldr_xwm_focus(x, C.uint32_t(c.id))
			case 'r':
				C.worldr_xwm_resize(x, C.uint32_t(c.id), C.int(c.width), C.int(c.height))
			case 'c':
				C.worldr_xwm_delete(x, C.uint32_t(c.id))
			case 'p':
				var names []*C.char
				for _, mime := range c.mimes {
					names = append(names, C.CString(mime))
				}
				var raw **C.char
				if len(names) > 0 {
					raw = (**C.char)(unsafe.Pointer(&names[0]))
				}
				status := C.worldr_xwm_clipboard_publish(x, C.uint64_t(c.externalID), raw, C.int(len(names)))
				for _, name := range names {
					C.free(unsafe.Pointer(name))
				}
				if status != 0 {
					c.done <- errors.New("private XWM rejected clipboard offer")
					break
				}
				var offer C.worldr_xwm_clipboard_offer
				C.worldr_xwm_get_clipboard_offer(x, &offer)
				b.mu.Lock()
				b.clipboard = clipboardOfferFromC(&offer)
				b.mu.Unlock()
				c.done <- nil
			case 'g':
				if len(receiveQueue) >= maxClipboardMIMEs {
					closeClipboardFD(c.fd)
				} else {
					receiveQueue = append(receiveQueue, c)
				}
			case 'd':
				var data *C.uint8_t
				if len(c.data) > 0 {
					data = (*C.uint8_t)(unsafe.Pointer(&c.data[0]))
				}
				ok := C.int(0)
				if c.ok {
					ok = 1
				}
				C.worldr_xwm_clipboard_reply(x, C.uint64_t(c.transferID), data, C.uint32_t(len(c.data)), ok)
			case 'M':
				if C.worldr_xwm_dnd_motion(x, C.uint32_t(c.id), C.uint32_t(c.target), C.int(c.width), C.int(c.height), C.uint32_t(c.timestamp)) != 0 {
					c.done <- errors.New("X11 XDND source or destination is unavailable")
				} else {
					c.done <- nil
				}
			case 'U':
				if C.worldr_xwm_dnd_drop(x, C.uint32_t(c.id), C.uint32_t(c.target), C.uint32_t(c.timestamp)) != 0 {
					c.done <- errors.New("X11 XDND destination did not accept copy")
				} else {
					dndDroppedAt = time.Now()
					c.done <- nil
				}
			case 'X':
				C.worldr_xwm_dnd_cancel(x)
				dndDroppedAt = time.Time{}
				c.done <- nil
			}
		case <-tick.C:
			poll = true
		}
		if !activeReceive && len(receiveQueue) > 0 {
			active = receiveQueue[0]
			receiveQueue = receiveQueue[1:]
			name := C.CString(active.mime)
			status := C.worldr_xwm_clipboard_receive(x, C.uint64_t(active.transferID), C.uint64_t(active.offerID), name)
			C.free(unsafe.Pointer(name))
			if status != 0 {
				closeClipboardFD(active.fd)
			} else {
				activeReceive = true
				activeSince = time.Now()
			}
		}
		if poll {
			var raw [64]C.worldr_xwm_window
			n := int(C.worldr_xwm_poll(x, &raw[0], 64))
			b.mu.Lock()
			if n < 0 {
				b.ownerErr = errors.New("private XWM connection closed")
				b.mu.Unlock()
				return
			}
			b.snapshot = b.snapshot[:0]
			for i := 0; i < n; i++ {
				r := &raw[i]
				b.snapshot = append(b.snapshot, Window{ID: uint32(r.id), ObjectID: uint32(r.object_id), TransientFor: uint32(r.transient_for), PID: uint32(r.pid), Title: strings.ToValidUTF8(C.GoString(&r.title[0]), "�"), AppID: strings.ToValidUTF8(C.GoString(&r.app_id[0]), "�"), X: int(r.x), Y: int(r.y), Width: int(r.width), Height: int(r.height), OverrideRedirect: r.override_redirect != 0})
			}
			var offer C.worldr_xwm_clipboard_offer
			C.worldr_xwm_get_clipboard_offer(x, &offer)
			b.clipboard = clipboardOfferFromC(&offer)
			var drag C.worldr_xwm_dnd_offer
			C.worldr_xwm_get_dnd_offer(x, &drag)
			if drag.dropped != 0 {
				if dndDroppedAt.IsZero() {
					dndDroppedAt = time.Now()
				} else if time.Since(dndDroppedAt) >= 2*time.Second {
					C.worldr_xwm_dnd_cancel(x)
					dndDroppedAt = time.Time{}
					C.worldr_xwm_get_dnd_offer(x, &drag)
				}
			} else {
				dndDroppedAt = time.Time{}
			}
			b.drag = dndOfferFromC(&drag)
			available := maxClipboardMIMEs - len(b.clipboardRequests)
			if available > 0 {
				var requests [maxClipboardMIMEs]C.worldr_xwm_clipboard_request
				count := int(C.worldr_xwm_clipboard_requests(x, &requests[0], C.int(available)))
				for i := 0; i < count; i++ {
					b.clipboardRequests = append(b.clipboardRequests, clipboardRequestMeta{id: uint64(requests[i].id), externalID: uint64(requests[i].external_id), mime: C.GoString(&requests[i].mime[0])})
				}
			}
			b.mu.Unlock()
			if activeReceive {
				var id C.uint64_t
				var ok C.int
				var data *C.uint8_t
				var length C.uint32_t
				if C.worldr_xwm_clipboard_result(x, &id, &ok, &data, &length) != 0 {
					var payload []byte
					if ok != 0 && length > 0 {
						payload = C.GoBytes(unsafe.Pointer(data), C.int(length))
					}
					C.worldr_xwm_clipboard_consume_result(x)
					if uint64(id) == active.transferID && ok != 0 {
						go writeClipboard(active.fd, payload)
					} else {
						closeClipboardFD(active.fd)
					}
					activeReceive = false
				} else if time.Since(activeSince) >= 2*time.Second {
					C.worldr_xwm_clipboard_cancel(x, C.uint64_t(active.transferID))
				}
			}
		}
	}
}

func clipboardOfferFromC(raw *C.worldr_xwm_clipboard_offer) ClipboardOffer {
	offer := ClipboardOffer{Revision: uint64(raw.revision), ID: uint64(raw.id), ExternalID: uint64(raw.external_id)}
	for i := 0; i < int(raw.mime_count); i++ {
		offer.MIMEs = append(offer.MIMEs, C.GoString(&raw.mimes[i][0]))
	}
	return offer
}

func dndOfferFromC(raw *C.worldr_xwm_dnd_offer) XDNDOffer {
	offer := XDNDOffer{Revision: uint64(raw.revision), ID: uint64(raw.id), Owner: uint32(raw.owner), PID: uint32(raw.pid), Target: uint32(raw.target), Active: raw.active != 0, Accepted: raw.accepted != 0, Dropped: raw.dropped != 0}
	for i := 0; i < int(raw.mime_count); i++ {
		offer.MIMEs = append(offer.MIMEs, C.GoString(&raw.mimes[i][0]))
	}
	return offer
}

// Poll reconciles exact X11/Wayland associations. Call before Server.Poll so
// newly associated content can be published in the same host iteration.
func (b *Bridge) Poll() error {
	if b == nil || b.closed {
		return ErrClosed
	}
	select {
	case <-b.childDone:
		return fmt.Errorf("Xwayland exited: %v: %s", b.childErr, b.log.String())
	default:
	}
	b.mu.Lock()
	snapshot := append([]Window(nil), b.snapshot...)
	err := b.ownerErr
	b.mu.Unlock()
	if err != nil {
		return err
	}
	next := make(map[uint32]Window, len(snapshot))
	for _, w := range snapshot {
		previous, ok := b.windows[w.ID]
		if ok && previous.ObjectID == w.ObjectID {
			w.SurfaceID = previous.SurfaceID
		} else {
			if ok {
				if err := b.server.WithdrawX11(w.ID); err != nil {
					return err
				}
			}
			id, err := b.server.AssociateX11(uint32(b.cmd.Process.Pid), w.ObjectID, w.ID, w.Title, w.AppID)
			if errors.Is(err, apps.ErrX11SurfacePending) {
				continue
			}
			if err != nil {
				return fmt.Errorf("associate X11 window %x: %w", w.ID, err)
			}
			w.SurfaceID = id
		}
		if !ok || w.Title != previous.Title || w.AppID != previous.AppID {
			if err := b.server.UpdateX11(w.ID, w.Title, w.AppID); err != nil {
				return err
			}
		}
		next[w.ID] = w
	}
	for id := range b.windows {
		if _, ok := next[id]; !ok {
			if err := b.server.WithdrawX11(id); err != nil {
				return err
			}
		}
	}
	b.windows = next
	return nil
}
func (b *Bridge) Display() string {
	if b == nil || b.closed {
		return ""
	}
	return b.display
}
func (b *Bridge) Environment(parent []string) []string {
	if b == nil || b.closed {
		return privateEnvironment(parent, "", "", "")
	}
	return xwaylandEnvironment(parent, b.server.Socket(), b.display, b.authority, b.accelerated)
}

// Accelerated reports whether this bridge requested and retained the glamor
// DMA-BUF path. Client surfaces still prove their backing through Layers.
func (b *Bridge) Accelerated() bool {
	return b != nil && !b.closed && b.accelerated
}
func (b *Bridge) Window(surfaceID uint64) (Window, bool) {
	if b != nil && !b.closed {
		for _, w := range b.windows {
			if w.SurfaceID == surfaceID {
				return w, true
			}
		}
	}
	return Window{}, false
}

// XDNDOffer returns a copy of the current bounded X11 drag offer. Payloads are
// transferred directly through XdndSelection after an aware target accepts.
func (b *Bridge) XDNDOffer() XDNDOffer {
	if b == nil || b.closed {
		return XDNDOffer{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	offer := b.drag
	offer.MIMEs = append([]string(nil), offer.MIMEs...)
	return offer
}

// DragActive reports whether surfaceID owns the active X11 XDND selection.
func (b *Bridge) DragActive(surfaceID uint64) bool {
	window, ok := b.Window(surfaceID)
	if !ok {
		return false
	}
	offer := b.XDNDOffer()
	return offer.Active && (offer.Owner == window.ID || offer.PID != 0 && offer.PID == window.PID)
}

func (b *Bridge) xdndCommand(command command) error {
	command.done = make(chan error, 1)
	if err := b.send(command); err != nil {
		return err
	}
	select {
	case err := <-command.done:
		return err
	case <-b.ownerDone:
		return errors.New("XWM stopped during XDND")
	case <-b.stop:
		return ErrClosed
	}
}

// DragMotion routes a spatial pointer position between two managed X11
// surfaces. targetSurfaceID zero sends XDND leave.
func (b *Bridge) DragMotion(sourceSurfaceID, targetSurfaceID uint64, x, y float32, timestamp uint32) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if !b.DragActive(sourceSurfaceID) || math.IsNaN(float64(x)) || math.IsNaN(float64(y)) || math.IsInf(float64(x), 0) || math.IsInf(float64(y), 0) {
		return errors.New("invalid X11 XDND motion")
	}
	source, _ := b.Window(sourceSurfaceID)
	var target uint32
	if targetSurfaceID != 0 {
		window, ok := b.Window(targetSurfaceID)
		if !ok {
			return fmt.Errorf("unknown X11 XDND target surface %d", targetSurfaceID)
		}
		target = window.ID
	}
	return b.xdndCommand(command{kind: 'M', id: source.ID, target: target, width: int(math.Round(float64(x))), height: int(math.Round(float64(y))), timestamp: timestamp})
}

// DropXDND completes a copy only after the target returned XdndStatus accept.
func (b *Bridge) DropXDND(sourceSurfaceID, targetSurfaceID uint64, timestamp uint32) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if !b.DragActive(sourceSurfaceID) {
		return errors.New("inactive X11 XDND source")
	}
	source, _ := b.Window(sourceSurfaceID)
	target, ok := b.Window(targetSurfaceID)
	if !ok {
		return fmt.Errorf("unknown X11 XDND target surface %d", targetSurfaceID)
	}
	return b.xdndCommand(command{kind: 'U', id: source.ID, target: target.ID, timestamp: timestamp})
}

// CancelXDND sends leave or a failed post-drop completion and clears all
// routing state. It is idempotent while the bridge is live.
func (b *Bridge) CancelXDND() error {
	if b == nil || b.closed {
		return ErrClosed
	}
	return b.xdndCommand(command{kind: 'X'})
}

// ClipboardOffer returns metadata for the current private X11 CLIPBOARD
// selection. It never asks the selection owner for bytes.
func (b *Bridge) ClipboardOffer() ClipboardOffer {
	if b == nil || b.closed {
		return ClipboardOffer{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	offer := b.clipboard
	offer.MIMEs = append([]string(nil), offer.MIMEs...)
	return offer
}

func validClipboardMIME(mime string) bool {
	return len(mime) > 0 && len(mime) < int(C.WORLDR_XWM_CLIPBOARD_MIME_SIZE) && strings.IndexByte(mime, 0) < 0
}

// OfferClipboard makes an external broker offer available to X11 clients.
// Data remains lazy and is requested through PollClipboardRequests.
func (b *Bridge) OfferClipboard(externalID uint64, mimes []string) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if len(mimes) > maxClipboardMIMEs || (externalID == 0) != (len(mimes) == 0) {
		return errors.New("invalid X11 clipboard offer")
	}
	for _, mime := range mimes {
		if !validClipboardMIME(mime) {
			return errors.New("invalid X11 clipboard MIME type")
		}
	}
	done := make(chan error, 1)
	if err := b.send(command{kind: 'p', externalID: externalID, mimes: append([]string(nil), mimes...), done: done}); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-b.ownerDone:
		return errors.New("XWM stopped while publishing clipboard offer")
	case <-b.stop:
		return ErrClosed
	}
}

// ReceiveClipboard requests one MIME from an X11-owned selection and relays it
// into a duplicate of fd. The caller retains fd and may close it on return.
func (b *Bridge) ReceiveClipboard(offerID uint64, mime string, fd int) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if fd < 0 || !validClipboardMIME(mime) {
		return errors.New("invalid X11 clipboard receive request")
	}
	b.mu.Lock()
	current := b.clipboard
	available := current.ID != 0 && current.ExternalID == 0 && current.ID == offerID
	if available {
		available = false
		for _, offered := range current.MIMEs {
			available = available || offered == mime
		}
	}
	if available {
		b.clipboardSerial++
	}
	transferID := b.clipboardSerial
	b.mu.Unlock()
	if !available {
		return errors.New("X11 clipboard offer is stale or MIME type unavailable")
	}
	owned, err := unix.Dup(fd)
	if err != nil {
		return fmt.Errorf("duplicate X11 clipboard descriptor: %w", err)
	}
	if err := b.send(command{kind: 'g', transferID: transferID, offerID: offerID, mime: mime, fd: owned}); err != nil {
		closeClipboardFD(owned)
		return err
	}
	return nil
}

// PollClipboardRequests returns descriptors requested by X11 consumers. The
// caller owns each descriptor and must either relay data or close it.
func (b *Bridge) PollClipboardRequests() []ClipboardRequest {
	if b == nil || b.closed {
		return nil
	}
	b.mu.Lock()
	requests := append([]clipboardRequestMeta(nil), b.clipboardRequests...)
	b.clipboardRequests = b.clipboardRequests[:0]
	b.mu.Unlock()
	result := make([]ClipboardRequest, 0, len(requests))
	for _, request := range requests {
		reader, writer, err := os.Pipe()
		if err != nil {
			b.sendClipboardResult(command{kind: 'd', transferID: request.id})
			continue
		}
		fd, err := unix.Dup(int(writer.Fd()))
		writer.Close()
		if err != nil {
			reader.Close()
			b.sendClipboardResult(command{kind: 'd', transferID: request.id})
			continue
		}
		go b.readClipboard(request.id, reader)
		result = append(result, ClipboardRequest{ExternalID: request.externalID, MIME: request.mime, FD: fd})
	}
	return result
}

func (b *Bridge) readClipboard(id uint64, reader *os.File) {
	defer reader.Close()
	_ = reader.SetReadDeadline(time.Now().Add(2 * time.Second))
	data, err := io.ReadAll(io.LimitReader(reader, maxClipboardBytes+1))
	ok := err == nil && len(data) <= maxClipboardBytes
	if !ok {
		data = nil
	}
	b.sendClipboardResult(command{kind: 'd', transferID: id, data: data, ok: ok})
}

func (b *Bridge) sendClipboardResult(result command) {
	select {
	case b.commands <- result:
	case <-b.stop:
	case <-b.ownerDone:
	}
}

func closeClipboardFD(fd int) {
	if fd >= 0 {
		if file := os.NewFile(uintptr(fd), "X11 clipboard"); file != nil {
			_ = file.Close()
		}
	}
}

func writeClipboard(fd int, data []byte) {
	file := os.NewFile(uintptr(fd), "X11 clipboard destination")
	if file == nil {
		closeClipboardFD(fd)
		return
	}
	defer file.Close()
	_ = file.SetWriteDeadline(time.Now().Add(2 * time.Second))
	for len(data) > 0 {
		n, err := file.Write(data)
		if err != nil || n == 0 {
			return
		}
		data = data[n:]
	}
}

func (b *Bridge) send(c command) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	select {
	case <-b.ownerDone:
		return errors.New("XWM is not running")
	default:
	}
	select {
	case b.commands <- c:
		return nil
	default:
		return errors.New("XWM command queue is full")
	}
}
func (b *Bridge) Focus(surfaceID uint64) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if surfaceID == 0 {
		return b.send(command{kind: 'f'})
	}
	w, ok := b.Window(surfaceID)
	if !ok {
		return fmt.Errorf("unknown X11 surface %d", surfaceID)
	}
	return b.send(command{kind: 'f', id: w.ID})
}
func (b *Bridge) Resize(surfaceID uint64, width, height int) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if width < 1 || height < 1 || width > 4096 || height > 4096 {
		return errors.New("X11 dimensions must be between 1 and 4096")
	}
	w, ok := b.Window(surfaceID)
	if !ok {
		return fmt.Errorf("unknown X11 surface %d", surfaceID)
	}
	return b.send(command{kind: 'r', id: w.ID, width: width, height: height})
}
func (b *Bridge) CloseSurface(surfaceID uint64) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	w, ok := b.Window(surfaceID)
	if !ok {
		return fmt.Errorf("unknown X11 surface %d", surfaceID)
	}
	return b.send(command{kind: 'c', id: w.ID})
}
func (b *Bridge) Close() error {
	if b == nil {
		return nil
	}
	if b.closed {
		return b.closeErr
	}
	b.closed = true
	for id := range b.windows {
		b.closeErr = errors.Join(b.closeErr, b.server.WithdrawX11(id))
	}
	b.windows = nil
	close(b.stop)
	if b.control != nil {
		unix.Shutdown(int(b.control.Fd()), unix.SHUT_RDWR)
		b.control.Close()
		b.control = nil
	}
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-b.childDone:
		case <-time.After(2 * time.Second):
			b.cmd.Process.Kill()
			select {
			case <-b.childDone:
			case <-time.After(2 * time.Second):
				b.closeErr = errors.Join(b.closeErr, errors.New("Xwayland did not exit after kill"))
			}
		}
		select {
		case <-b.ownerDone:
		case <-time.After(2 * time.Second):
			b.closeErr = errors.Join(b.closeErr, errors.New("XWM shutdown timed out"))
		}
	}
	b.closeErr = errors.Join(b.closeErr, os.RemoveAll(b.dir))
	return b.closeErr
}

type boundedLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *boundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	if len(p) >= 8192 {
		l.data = append(l.data[:0], p[len(p)-8192:]...)
	} else {
		if len(l.data)+len(p) > 8192 {
			l.data = l.data[len(l.data)+len(p)-8192:]
		}
		l.data = append(l.data, p...)
	}
	return n, nil
}
func (l *boundedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(string(l.data))
}
