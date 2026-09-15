package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestSetCursorNullKeepsDefaultVisible(t *testing.T) {
	s := &Server{cursorVisible: true, cursorShape: cursorShapeDefault}
	c := &Client{srv: s, objs: map[uint32]*object{}}
	s.setCursorFromSurface(c, 0, 4, 4)
	_, _, _, _, pix, _, _, _, shape, vis := s.Cursor()
	if !vis {
		t.Fatal("null set_cursor must keep a visible default arrow")
	}
	if pix != nil {
		t.Fatal("null set_cursor must drop the client image")
	}
	if shape != CursorShapeDefault {
		t.Fatalf("shape %d want default", shape)
	}
	if s.CursorCustom() {
		t.Fatal("default arrow is not a custom cursor")
	}
}

func TestReqPointerSetCursorNull(t *testing.T) {
	s := &Server{cursorVisible: false, cursorPix: []byte{1, 2, 3, 4}}
	c := &Client{srv: s, objs: map[uint32]*object{}}
	p := wayland.PutU32(nil, 1)
	p = wayland.PutU32(p, 0)
	p = wayland.PutI32(p, 0)
	p = wayland.PutI32(p, 0)
	if err := c.reqPointer(&object{id: 10, kind: kindPointer}, 0, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, pix, _, _, _, shape, vis := s.Cursor()
	if !vis || pix != nil || shape != CursorShapeDefault {
		t.Fatalf("set_cursor(null) vis=%v shape=%d pix=%v", vis, shape, pix != nil)
	}
}

func TestSetCursorShapeApplied(t *testing.T) {
	s := &Server{}
	s.setCursorShape(CursorShapeText)
	_, _, _, _, pix, _, _, _, shape, vis := s.Cursor()
	if !vis || pix != nil || shape != CursorShapeText {
		t.Fatalf("text shape vis=%v shape=%d pix=%v", vis, shape, pix != nil)
	}
	if !s.CursorCustom() {
		t.Fatal("text cursor is custom")
	}
	s.setCursorShape(CursorShapeDefault)
	if s.CursorCustom() {
		t.Fatal("default is not custom")
	}
	s.setCursorShape(0)
	_, _, _, _, _, _, _, _, shape, vis = s.Cursor()
	if !vis || shape != CursorShapeDefault {
		t.Fatalf("shape 0 → default vis=%v shape=%d", vis, shape)
	}
}

func TestAdvertiseAndSetCursorShape(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-cursor", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	done := make(chan struct{})
	go func() {
		tck := time.NewTicker(time.Millisecond)
		defer tck.Stop()
		for {
			select {
			case <-done:
				return
			case <-tck.C:
				s.Dispatch()
			}
		}
	}()
	defer close(done)

	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: s.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	wr := wayland.NewWriter(c)
	rd := wayland.NewReader(c)
	if err := wr.Send(1, 1, wayland.PutU32(nil, 2), nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var cursorName uint32
	for time.Now().Before(deadline) && cursorName == 0 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "wp_cursor_shape_manager_v1" {
				cursorName = name
			}
		}
	}
	if cursorName == 0 {
		t.Fatal("wp_cursor_shape_manager_v1 not advertised")
	}

	const (
		mgrID    = 3
		deviceID = 4
		ptrID    = 5
	)
	p := wayland.PutU32(nil, cursorName)
	p = wayland.PutString(p, "wp_cursor_shape_manager_v1")
	p = wayland.PutU32(p, 1)
	p = wayland.PutU32(p, mgrID)
	if err := wr.Send(2, 0, p, nil); err != nil {
		t.Fatal(err)
	}
	// get_pointer(id, wl_pointer) — pointer id does not need a live object
	p = wayland.PutU32(nil, deviceID)
	p = wayland.PutU32(p, ptrID)
	if err := wr.Send(mgrID, 1, p, nil); err != nil {
		t.Fatal(err)
	}
	p = wayland.PutU32(nil, 1) // serial
	p = wayland.PutU32(p, CursorShapeText)
	if err := wr.Send(deviceID, 1, p, nil); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, _, _, _, _, _, _, shape, vis := s.Cursor()
		if vis && shape == CursorShapeText {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, _, _, _, _, _, _, shape, vis := s.Cursor()
	t.Fatalf("set_shape not applied vis=%v shape=%d", vis, shape)
}
