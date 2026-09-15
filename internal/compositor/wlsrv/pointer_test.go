package wlsrv

import (
	"io"
	"log"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestPointerButtonsPressRelease(t *testing.T) {
	var b pointerButtons
	if !b.press(btnLeft) {
		t.Fatal("first press")
	}
	if b.press(btnLeft) {
		t.Fatal("duplicate press must be ignored")
	}
	if !b.anyDown() {
		t.Fatal("down after press")
	}
	if !b.release(btnLeft) {
		t.Fatal("matching release")
	}
	if b.anyDown() {
		t.Fatal("cleared after release")
	}
	if b.release(btnLeft) {
		t.Fatal("unmatched release must be suppressed")
	}
}

func TestPointerButtonsTakeAll(t *testing.T) {
	var b pointerButtons
	if got := b.takeAll(); len(got) != 0 {
		t.Fatalf("empty takeAll %v", got)
	}
	_ = b.press(btnLeft)
	got := b.takeAll()
	if len(got) != 1 || got[0] != btnLeft {
		t.Fatalf("takeAll %v", got)
	}
	if b.anyDown() || b.release(btnLeft) {
		t.Fatal("takeAll must clear")
	}
}

func TestPointerButtonPressThenRelease(t *testing.T) {
	c, rd, conn, _ := newPointerClient(t, true)
	c.pointerButton(20, 20, true)
	c.pointerButton(22, 22, false)
	ev := drainPointer(t, conn, rd, c.ptrID)
	if n := countButton(ev, wlPointerPressed); n != 1 {
		t.Fatalf("press count %d events=%v", n, ev)
	}
	if n := countButton(ev, wlPointerReleased); n != 1 {
		t.Fatalf("release count %d events=%v", n, ev)
	}
}

func TestPointerButtonReleaseOnlySuppressed(t *testing.T) {
	c, rd, conn, _ := newPointerClient(t, true)
	c.pointerButton(20, 20, false)
	ev := drainPointer(t, conn, rd, c.ptrID)
	if n := countButton(ev, wlPointerReleased); n != 0 {
		t.Fatalf("stray release sent: %v", ev)
	}
	if n := countButton(ev, wlPointerPressed); n != 0 {
		t.Fatalf("unexpected press: %v", ev)
	}
}

func TestPointerLeaveWhileDownSendsMatchingRelease(t *testing.T) {
	c, rd, conn, _ := newPointerClient(t, true)
	c.pointerButton(20, 20, true)
	c.pointerLeaveCurrent()
	ev := drainPointer(t, conn, rd, c.ptrID)
	if n := countButton(ev, wlPointerPressed); n != 1 {
		t.Fatalf("press count %d events=%v", n, ev)
	}
	if n := countButton(ev, wlPointerReleased); n != 1 {
		t.Fatalf("leave-while-down must send matching release, got %v", ev)
	}
	if n := countOp(ev, wlPointerLeave); n != 1 {
		t.Fatalf("leave count %d events=%v", n, ev)
	}
	// later host release must not emit a second (unmatched) release
	c.pointerButton(20, 20, false)
	ev2 := drainPointer(t, conn, rd, c.ptrID)
	if n := countButton(ev2, wlPointerReleased); n != 0 {
		t.Fatalf("post-leave release leaked: %v", ev2)
	}
}

func TestPointerImplicitGrabKeepsSurfaceUntilRelease(t *testing.T) {
	c, rd, conn, a1 := newPointerClient(t, true)
	a2 := &engine.Actor{X: 300, Y: 0, Width: 100, Height: 100, Focused: false}
	c.srv.Scene.Add(a2)
	s2 := &surface{id: 21, actor: a2}
	c.objs[21] = &object{id: 21, kind: kindSurface, surf: s2}

	c.pointerButton(20, 20, true)
	drainPointer(t, conn, rd, c.ptrID) // discard enter/press

	a1.Focused = false
	a2.Focused = true
	c.pointerMotion(320, 20)
	c.pointerButton(320, 20, false)
	ev := drainPointer(t, conn, rd, c.ptrID)
	if n := countOp(ev, wlPointerLeave); n != 0 {
		t.Fatalf("implicit grab must not leave while button is down: %v", ev)
	}
	if n := countButton(ev, wlPointerReleased); n != 1 {
		t.Fatalf("release on grabbed surface, got %v", ev)
	}
	if n := countButton(ev, wlPointerPressed); n != 0 {
		t.Fatalf("no extra press, got %v", ev)
	}
}

func TestPointerUnfocusedClientDoesNotGetPress(t *testing.T) {
	c, rd, conn, a := newPointerClient(t, false)
	if a.Focused {
		t.Fatal("fixture")
	}
	c.pointerButton(20, 20, true)
	c.pointerButton(20, 20, false)
	ev := drainPointer(t, conn, rd, c.ptrID)
	if n := countButton(ev, wlPointerPressed); n != 0 {
		t.Fatalf("unfocused client got press: %v", ev)
	}
	if n := countButton(ev, wlPointerReleased); n != 0 {
		t.Fatalf("unfocused client got release: %v", ev)
	}
}

type ptrEv struct {
	op     uint16
	button uint32
	state  uint32
	surf   uint32
}

func newPointerClient(t *testing.T, focused bool) (*Client, *wayland.Reader, *net.UnixConn, *engine.Actor) {
	t.Helper()
	srvConn, cliConn := unixPair(t)
	scene := engine.NewScene()
	a := &engine.Actor{X: 0, Y: 0, Width: 200, Height: 200, Focused: focused}
	scene.Add(a)
	srv := &Server{
		Scene:   scene,
		ScreenW: 800,
		ScreenH: 600,
		log:     log.New(io.Discard, "", 0),
	}
	c := newClient(srv, srvConn)
	c.ptrID = 10
	c.objs[10] = &object{id: 10, kind: kindPointer}
	s := &surface{id: 20, actor: a}
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: s}
	rd := wayland.NewReader(cliConn)
	return c, rd, cliConn, a
}

func unixPair(t *testing.T) (srv, cli *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	conns := make([]*net.UnixConn, 2)
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "u")
		cc, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		u, ok := cc.(*net.UnixConn)
		if !ok {
			t.Fatal("not unix")
		}
		conns[i] = u
	}
	t.Cleanup(func() {
		_ = conns[0].Close()
		_ = conns[1].Close()
	})
	return conns[0], conns[1]
}

func drainPointer(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, ptrID uint32) []ptrEv {
	t.Helper()
	var out []ptrEv
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return out
		}
		if msg.Object != ptrID {
			continue
		}
		ev := ptrEv{op: msg.Opcode}
		cur := wayland.NewCursor(msg.Payload, nil)
		switch msg.Opcode {
		case wlPointerEnter:
			_, _ = cur.U32()
			ev.surf, _ = cur.U32()
		case wlPointerLeave:
			_, _ = cur.U32()
			ev.surf, _ = cur.U32()
		case wlPointerButton:
			_, _ = cur.U32()
			_, _ = cur.U32()
			ev.button, _ = cur.U32()
			ev.state, _ = cur.U32()
		}
		out = append(out, ev)
	}
}

func countButton(ev []ptrEv, state uint32) int {
	n := 0
	for _, e := range ev {
		if e.op == wlPointerButton && e.state == state && e.button == btnLeft {
			n++
		}
	}
	return n
}

func countOp(ev []ptrEv, op uint16) int {
	n := 0
	for _, e := range ev {
		if e.op == op {
			n++
		}
	}
	return n
}
