//go:build linux && cgo

package apps

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This tiny wire client exercises the actual socket and libwayland dispatch,
// without adding a client library or generated test bindings to normal builds.
type cursorWireEvent struct {
	id     uint32
	opcode uint16
	data   []byte
}
type cursorWireClient struct {
	t                                                                    *testing.T
	s                                                                    *Server
	c                                                                    *net.UnixConn
	next                                                                 uint32
	buffer                                                               []byte
	events                                                               []cursorWireEvent
	preserveFDs                                                          bool
	receivedFDs                                                          []int
	compositor, subcompositor, shm, seat, wm, pointer, surface, xdg, top uint32
	wmVersion                                                            uint32
}

func (c *cursorWireClient) id() uint32 { c.next++; return c.next }
func cursorWireString(s string) []byte {
	v := make([]byte, 4+(len(s)+1+3)&^3)
	binary.LittleEndian.PutUint32(v, uint32(len(s)+1))
	copy(v[4:], s)
	return v
}
func (c *cursorWireClient) send(id uint32, opcode uint16, fd int, values ...any) {
	c.t.Helper()
	b := make([]byte, 8)
	for _, value := range values {
		switch v := value.(type) {
		case uint32:
			b = binary.LittleEndian.AppendUint32(b, v)
		case int:
			b = binary.LittleEndian.AppendUint32(b, uint32(v))
		case string:
			b = append(b, cursorWireString(v)...)
		default:
			c.t.Fatalf("unsupported wire value %T", value)
		}
	}
	binary.LittleEndian.PutUint32(b, id)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b))<<16|uint32(opcode))
	var rights []byte
	if fd >= 0 {
		rights = unix.UnixRights(fd)
	}
	if _, _, err := c.c.WriteMsgUnix(b, rights, nil); err != nil {
		c.t.Fatal(err)
	}
}
func (c *cursorWireClient) sync() error {
	callback := c.id()
	c.send(1, 0, -1, callback)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.s.Poll(); err != nil {
			return err
		}
		c.c.SetReadDeadline(time.Now().Add(time.Millisecond))
		var b [32768]byte
		var oob [1024]byte
		n, noob, _, _, err := c.c.ReadMsgUnix(b[:], oob[:])
		if noob > 0 {
			messages, parseErr := unix.ParseSocketControlMessage(oob[:noob])
			if parseErr != nil {
				return parseErr
			}
			for _, m := range messages {
				fds, _ := unix.ParseUnixRights(&m)
				for _, fd := range fds {
					if c.preserveFDs {
						c.receivedFDs = append(c.receivedFDs, fd)
					} else {
						unix.Close(fd)
					}
				}
			}
		}
		if n > 0 {
			c.buffer = append(c.buffer, b[:n]...)
		}
		for len(c.buffer) >= 8 {
			id := binary.LittleEndian.Uint32(c.buffer)
			header := binary.LittleEndian.Uint32(c.buffer[4:])
			size, opcode := int(header>>16), uint16(header)
			if size < 8 || size&3 != 0 {
				return fmt.Errorf("invalid wire event length %d", size)
			}
			if len(c.buffer) < size {
				break
			}
			e := cursorWireEvent{id, opcode, append([]byte(nil), c.buffer[8:size]...)}
			c.buffer = c.buffer[size:]
			c.events = append(c.events, e)
			if id == 1 && opcode == 0 {
				return fmt.Errorf("client protocol error: %x", e.data)
			}
			if id == callback && opcode == 0 {
				return nil
			}
		}
		if err != nil {
			if e, ok := err.(net.Error); !ok || !e.Timeout() {
				return err
			}
		}
	}
	return fmt.Errorf("cursor client roundtrip timed out")
}
func (c *cursorWireClient) roundtrip() {
	c.t.Helper()
	if err := c.sync(); err != nil {
		c.t.Fatal(err)
	}
}
func newCursorWireClient(t *testing.T, s *Server) *cursorWireClient {
	t.Helper()
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: s.Socket(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	c := &cursorWireClient{t: t, s: s, c: connection, next: 1}
	t.Cleanup(func() { connection.Close() })
	registry := c.id()
	c.send(1, 1, -1, registry)
	c.roundtrip()
	for _, e := range c.events {
		if e.id != registry || e.opcode != 0 {
			continue
		}
		name, length := binary.LittleEndian.Uint32(e.data), int(binary.LittleEndian.Uint32(e.data[4:]))
		kind := string(e.data[8 : 8+length-1])
		advertised := binary.LittleEndian.Uint32(e.data[8+(length+3)&^3:])
		version := uint32(1)
		var target *uint32
		switch kind {
		case "wl_compositor":
			target, version = &c.compositor, 4
		case "wl_subcompositor":
			target = &c.subcompositor
		case "wl_shm":
			target = &c.shm
		case "wl_seat":
			target, version = &c.seat, 5
		case "xdg_wm_base":
			target, version = &c.wm, 3
		}
		if target != nil {
			if version > advertised {
				version = advertised
			}
			*target = c.id()
			c.send(registry, 0, -1, name, kind, version, *target)
			if kind == "xdg_wm_base" {
				c.wmVersion = version
			}
		}
	}
	if c.compositor == 0 || c.shm == 0 || c.seat == 0 || c.wm == 0 {
		t.Fatal("missing required globals")
	}
	c.pointer = c.id()
	c.send(c.seat, 0, -1, c.pointer)
	c.surface = c.id()
	c.send(c.compositor, 0, -1, c.surface)
	c.xdg = c.id()
	c.send(c.wm, 2, -1, c.xdg, c.surface)
	c.top = c.id()
	c.send(c.xdg, 1, -1, c.top)
	c.send(c.top, 2, -1, "cursor-test")
	c.send(c.surface, 6, -1)
	c.roundtrip()
	for _, e := range c.events {
		if e.id == c.xdg && e.opcode == 0 {
			c.send(c.xdg, 4, -1, binary.LittleEndian.Uint32(e.data))
		}
	}
	buffer := c.image(32, 24, 0xff183050)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	return c
}
func (c *cursorWireClient) image(w, h int, pixel uint32, formats ...uint32) uint32 {
	c.t.Helper()
	f, err := os.CreateTemp(c.t.TempDir(), "cursor-shm-")
	if err != nil {
		c.t.Fatal(err)
	}
	defer f.Close()
	data := make([]byte, w*h*4)
	for i := 0; i < len(data); i += 4 {
		binary.LittleEndian.PutUint32(data[i:], pixel)
	}
	if _, err := f.Write(data); err != nil {
		c.t.Fatal(err)
	}
	pool, buffer := c.id(), c.id()
	c.send(c.shm, 0, int(f.Fd()), pool, len(data))
	format := uint32(0)
	if len(formats) > 0 {
		format = formats[0]
	}
	c.send(pool, 0, -1, buffer, 0, w, h, w*4, format)
	c.send(pool, 1, -1)
	return buffer
}
func (c *cursorWireClient) cursor() uint32 { id := c.id(); c.send(c.compositor, 0, -1, id); return id }
func (c *cursorWireClient) enter(root uint64) uint32 {
	c.t.Helper()
	if err := c.s.Pointer(root, 10, 10); err != nil {
		c.t.Fatal(err)
	}
	c.roundtrip()
	for i := len(c.events) - 1; i >= 0; i-- {
		e := c.events[i]
		if e.id == c.pointer && e.opcode == 0 {
			return binary.LittleEndian.Uint32(e.data)
		}
	}
	c.t.Fatal("no pointer enter")
	return 0
}
func cursorMappedID(t *testing.T, s *Server, index int) uint64 {
	t.Helper()
	v, err := s.Poll()
	if err != nil || len(v) <= index {
		t.Fatalf("mapped cursor fixture: %v %v", v, err)
	}
	return v[index].ID
}

func TestCursorSHMScaleHotspotHiddenAndLifetime(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	root := cursorMappedID(t, s, 0)
	serial := c.enter(root)
	if v := s.Cursor(); v.Set || v.SurfaceID != root {
		t.Fatalf("initial cursor is not the workspace default: %+v", v)
	}
	cursor := c.cursor()
	buffer := c.image(8, 4, 0x80402010)
	c.send(cursor, 8, -1, 2)
	c.send(cursor, 1, -1, buffer, 0, 0)
	c.send(cursor, 6, -1)
	c.send(c.pointer, 0, -1, serial-1, cursor, 3, 1)
	c.roundtrip()
	if s.Cursor().Set {
		t.Fatal("stale enter serial changed cursor")
	}
	c.send(c.pointer, 0, -1, serial, cursor, 3, 1)
	c.roundtrip()
	first := s.Cursor()
	if !first.Set || first.Hidden || first.Width != 8 || first.Height != 4 || first.Scale != 2 || first.LogicalWidth != 4 || first.LogicalHeight != 2 || first.HotspotX != 3 || first.HotspotY != 1 || !bytes.Equal(first.Pixels[:4], []byte{64, 32, 16, 128}) {
		t.Fatalf("lost cursor image/alpha/scale/hotspot: %+v", first)
	}
	if again := s.Cursor(); again.Revision != first.Revision || &again.Pixels[0] != &first.Pixels[0] {
		t.Fatal("unchanged cursor copied retained image")
	}
	buffer = c.image(8, 4, 0xff204080)
	c.send(cursor, 1, -1, buffer, 1, -2)
	c.send(cursor, 6, -1)
	c.roundtrip()
	second := s.Cursor()
	if second.Revision <= first.Revision || second.HotspotX != 2 || second.HotspotY != 3 || !bytes.Equal(second.Pixels[:4], []byte{32, 64, 128, 255}) || !bytes.Equal(first.Pixels[:4], []byte{64, 32, 16, 128}) {
		t.Fatal("cursor commit lost offset or mutated previous snapshot")
	}
	c.send(buffer, 0, -1)
	c.roundtrip()
	if s.Cursor().Hidden {
		t.Fatal("released buffer destruction removed copied cursor image")
	}
	c.send(c.pointer, 0, -1, serial, uint32(0), 0, 0)
	c.roundtrip()
	if v := s.Cursor(); !v.Set || !v.Hidden || v.Pixels != nil {
		t.Fatalf("null cursor did not hide image: %+v", v)
	}
	c.send(c.pointer, 0, -1, serial, cursor, 0, 0)
	c.send(cursor, 1, -1, uint32(0), 0, 0)
	c.send(cursor, 6, -1)
	c.roundtrip()
	if v := s.Cursor(); !v.Set || !v.Hidden {
		t.Fatal("null buffer did not hide cursor")
	}
	c.send(cursor, 0, -1)
	c.roundtrip()
	if s.Cursor().Set {
		t.Fatal("destroyed cursor surface remained selected")
	}
	if err := s.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if v := s.Cursor(); v.Set || v.SurfaceID != 0 {
		t.Fatal("pointer leave retained client cursor")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if v := s.Cursor(); v.Set || v.Pixels != nil {
		t.Fatal("closed server retained cursor image")
	}
}

func TestCursorFocusIsolationAndPermanentRoles(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := newCursorWireClient(t, s)
	a := cursorMappedID(t, s, 0)
	second := newCursorWireClient(t, s)
	b := cursorMappedID(t, s, 1)
	serial := first.enter(a)
	cursor := first.cursor()
	first.send(first.pointer, 0, -1, serial, cursor, 0, 0)
	first.roundtrip()
	if !s.Cursor().Set {
		t.Fatal("first client cursor not set")
	}
	second.enter(b)
	first.send(first.pointer, 0, -1, serial, cursor, 0, 0)
	first.roundtrip()
	if v := s.Cursor(); v.Set || v.SurfaceID != b {
		t.Fatal("unfocused client changed new client's cursor")
	}
	serial = first.enter(a)
	first.send(first.pointer, 0, -1, serial, cursor, 0, 0)
	first.roundtrip()
	if err := s.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	xdg := first.id()
	first.send(first.wm, 2, -1, xdg, cursor)
	if err := first.sync(); err == nil {
		t.Fatal("cursor role was reusable as an xdg surface after pointer leave")
	}
	if v := s.Cursor(); v.Set || v.SurfaceID != 0 {
		t.Fatal("protocol error retained cursor")
	}
	if _, err := s.Poll(); err != nil {
		t.Fatal("one invalid client killed server", err)
	}
}

func TestCursorSubsurfaceCompositionRetainsPremultipliedAlpha(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	serial := c.enter(cursorMappedID(t, s, 0))
	cursor := c.cursor()
	buffer := c.image(4, 4, 0x80402010)
	c.send(cursor, 1, -1, buffer, 0, 0)
	c.send(cursor, 6, -1)
	c.send(c.pointer, 0, -1, serial, cursor, 0, 0)
	c.roundtrip()
	before := s.Cursor()
	child, sub := c.cursor(), c.id()
	c.send(c.subcompositor, 1, -1, sub, child, cursor)
	c.send(sub, 1, -1, 1, 1)
	buffer = c.image(2, 2, 0x80100804)
	c.send(child, 1, -1, buffer, 0, 0)
	c.send(child, 6, -1)
	c.send(cursor, 6, -1)
	c.roundtrip()
	after := s.Cursor()
	if after.Revision <= before.Revision || !bytes.Equal(after.Pixels[20:24], []byte{48, 24, 12, 192}) || !bytes.Equal(after.Pixels[:4], []byte{64, 32, 16, 128}) || !bytes.Equal(before.Pixels[20:24], []byte{64, 32, 16, 128}) {
		t.Fatalf("cursor subsurface lost premultiplied composition or snapshot immutability: %v", after.Pixels)
	}
}

func TestCursorFinalReleaseRetargetsChildWithinSameRoot(t *testing.T) {
	s, err := Open(32, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newCursorWireClient(t, s)
	root := cursorMappedID(t, s, 0)
	child, sub := c.cursor(), c.id()
	c.send(c.subcompositor, 1, -1, sub, child, c.surface)
	buffer := c.image(16, 16, 0xff102030)
	c.send(child, 1, -1, buffer, 0, 0)
	c.send(child, 6, -1)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	serial := c.enter(root) // (10,10) enters the child.
	c.send(c.pointer, 0, -1, serial, uint32(0), 0, 0)
	c.roundtrip()
	if cursor := s.Cursor(); !cursor.Set || !cursor.Hidden {
		t.Fatal("child did not set hidden cursor")
	}
	for _, code := range []uint32{272, 273} {
		if err := s.Button(code, true, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Pointer(root, 24, 20); err != nil {
		t.Fatal(err)
	} // Parent, still grabbed by child.
	if err := s.Button(272, false, 2); err != nil {
		t.Fatal(err)
	}
	if cursor := s.Cursor(); !cursor.Set || !cursor.Hidden {
		t.Fatal("partial release lost child grab/cursor")
	}
	before := len(c.events)
	if err := s.Button(273, false, 3); err != nil {
		t.Fatal(err)
	}
	if cursor := s.Cursor(); cursor.Set || cursor.SurfaceID != root {
		t.Fatal("final release retained child cursor without further motion")
	}
	c.roundtrip()
	var parentSerial uint32
	for _, event := range c.events[before:] {
		if event.id == c.pointer && event.opcode == 0 && binary.LittleEndian.Uint32(event.data[4:]) == c.surface {
			parentSerial = binary.LittleEndian.Uint32(event.data)
		}
	}
	if parentSerial == 0 || parentSerial == serial {
		t.Fatal("final release did not enter parent with a new serial")
	}
	c.send(c.pointer, 0, -1, serial, uint32(0), 0, 0)
	c.roundtrip()
	if s.Cursor().Set {
		t.Fatal("child enter serial remained valid after retarget")
	}
	c.send(c.pointer, 0, -1, parentSerial, uint32(0), 0, 0)
	c.roundtrip()
	if cursor := s.Cursor(); !cursor.Set || !cursor.Hidden {
		t.Fatal("parent could not choose its cursor after retarget")
	}

	// Explicit input cancellation releases all buttons, but must not briefly
	// enter another surface while unwinding that cancelled grab.
	serial = c.enter(root)
	if err := s.Button(272, true, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Pointer(root, 24, 20); err != nil {
		t.Fatal(err)
	}
	c.roundtrip()
	before = len(c.events)
	if err := s.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	c.roundtrip()
	for _, event := range c.events[before:] {
		if event.id == c.pointer && event.opcode == 0 {
			t.Fatal("cancellation spuriously entered another surface")
		}
	}
	if cursor := s.Cursor(); cursor.Set || cursor.SurfaceID != 0 {
		t.Fatal("cancellation retained pointer cursor")
	}
}

func TestCursorRejectsWindowRoleAndOversizedImages(t *testing.T) {
	for _, kind := range []string{"window-role", "large-before-role", "large-after-role"} {
		t.Run(kind, func(t *testing.T) {
			s, err := Open(32, 24)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			c := newCursorWireClient(t, s)
			serial := c.enter(cursorMappedID(t, s, 0))
			if kind == "window-role" {
				c.send(c.pointer, 0, -1, serial, c.surface, 0, 0)
			} else {
				cursor := c.cursor()
				if kind == "large-after-role" {
					c.send(c.pointer, 0, -1, serial, cursor, 0, 0)
				}
				buffer := c.image(513, 1, 0xffffffff)
				c.send(cursor, 1, -1, buffer, 0, 0)
				c.send(cursor, 6, -1)
				if kind == "large-before-role" {
					c.send(c.pointer, 0, -1, serial, cursor, 0, 0)
				}
			}
			if err := c.sync(); err == nil {
				t.Fatal("invalid cursor request was accepted")
			}
			if _, err := s.Poll(); err != nil {
				t.Fatal(err)
			}
			if s.Cursor().Set {
				t.Fatal("invalid client retained cursor")
			}
		})
	}
}

func TestCursorClearsWhenFocusedWindowUnmapsOrClientDisconnects(t *testing.T) {
	for _, mode := range []string{"unmap", "destroy", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			s, err := Open(32, 24)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			c := newCursorWireClient(t, s)
			serial := c.enter(cursorMappedID(t, s, 0))
			cursor := c.cursor()
			buffer := c.image(4, 4, 0xffffffff)
			c.send(cursor, 1, -1, buffer, 0, 0)
			c.send(cursor, 6, -1)
			c.send(c.pointer, 0, -1, serial, cursor, 0, 0)
			c.roundtrip()
			if v := s.Cursor(); !v.Set || v.Hidden {
				t.Fatal("fixture cursor not visible")
			}
			switch mode {
			case "unmap":
				c.send(c.surface, 1, -1, uint32(0), 0, 0)
				c.send(c.surface, 6, -1)
				c.roundtrip()
			case "destroy":
				c.send(c.top, 0, -1)
				c.send(c.xdg, 0, -1)
				c.send(c.surface, 0, -1)
				c.roundtrip()
			case "disconnect":
				c.c.Close()
				if _, err := s.Poll(); err != nil {
					t.Fatal(err)
				}
			}
			if v := s.Cursor(); v.Set || v.SurfaceID != 0 || v.Pixels != nil {
				t.Fatal("cursor outlived focused window/client")
			}
		})
	}
}

func TestFootClientCursorSHMAndLeave(t *testing.T) {
	c := startToolkit(t, "foot", func(string) []string {
		return []string{"--config=/dev/null", "--title=foot cursor test", "/bin/sh", "-c", "printf 'cursor fixture\\n'; IFS= read -r line"}
	})
	c.until("foot surface", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "foot cursor test" })
	id := c.latest[0].ID
	if err := c.server.Pointer(id, 100, 80); err != nil {
		t.Fatal(err)
	}
	c.until("foot cursor image", func([]Surface) bool {
		v := c.server.Cursor()
		return v.SurfaceID == id && v.Set && !v.Hidden && len(v.Pixels) > 0
	})
	v := c.server.Cursor()
	transparent, visible := false, false
	for i := 3; i < len(v.Pixels); i += 4 {
		transparent = transparent || v.Pixels[i] == 0
		visible = visible || v.Pixels[i] != 0
	}
	if !transparent || !visible {
		t.Fatal("foot cursor lost transparent silhouette")
	}
	if err := c.server.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if c.server.Cursor().Set {
		t.Fatal("foot cursor remained after pointer left")
	}
}

func TestChromiumClientCursorChangesAndHides(t *testing.T) {
	c := startToolkit(t, "chromium", func(dir string) []string {
		page := filepath.Join(dir, "cursors.html")
		if err := os.WriteFile(page, []byte(`<title>cursor ready</title><style>body{margin:0}.area{position:absolute;left:0;width:900px;height:100px}#a{top:0;cursor:crosshair;background:#15354a}#b{top:100px;cursor:text;background:#25453a}#c{top:200px;cursor:none;background:#35254a}</style><div class=area id=a></div><div class=area id=b></div><div class=area id=c></div>`), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{"--ozone-platform=wayland", "--disable-gpu", "--user-data-dir=" + filepath.Join(dir, "profile"), "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-default-apps", "--disable-features=MediaRouter", "--app=file://" + page}
	})
	c.until("local cursor page", func(v []Surface) bool { return len(v) == 1 && v[0].Title == "cursor ready" })
	id := c.latest[0].ID
	if err := c.server.Pointer(id, 100, 50); err != nil {
		t.Fatal(err)
	}
	c.until("crosshair cursor", func([]Surface) bool { v := c.server.Cursor(); return v.Set && !v.Hidden && len(v.Pixels) > 0 })
	first := c.server.Cursor()
	if err := c.server.Pointer(id, 100, 150); err != nil {
		t.Fatal(err)
	}
	c.until("text cursor", func([]Surface) bool {
		v := c.server.Cursor()
		return v.Set && !v.Hidden && v.Revision > first.Revision && !bytes.Equal(v.Pixels, first.Pixels)
	})
	if err := c.server.Pointer(id, 100, 250); err != nil {
		t.Fatal(err)
	}
	c.until("hidden CSS cursor", func([]Surface) bool { v := c.server.Cursor(); return v.Set && v.Hidden })
	if err := c.server.Pointer(0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if c.server.Cursor().Set {
		t.Fatal("browser cursor escaped its pointer focus")
	}
}
