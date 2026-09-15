package wlsrv

import (
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestTextInputManagerAdvertised(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-ime-adv", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := make(chan struct{})
	go dispatchUntil(s, stop)
	defer close(stop)

	c, wr, rd := dialServer(t, s)
	defer c.Close()
	if err := wr.Send(1, 1, wayland.PutU32(nil, 2), nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var name uint32
	for time.Now().Before(deadline) && name == 0 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			n, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "zwp_text_input_manager_v3" {
				name = n
			}
		}
	}
	if name == 0 {
		t.Fatal("zwp_text_input_manager_v3 not advertised")
	}
	if name != globalTextInput {
		t.Fatalf("name %d want %d", name, globalTextInput)
	}
}

func TestTextInputEnableCommitDone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-ime-en", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := make(chan struct{})
	go dispatchUntil(s, stop)
	defer close(stop)

	conn, wr, rd := dialServer(t, s)
	defer conn.Close()
	if err := wr.Send(1, 1, wayland.PutU32(nil, 2), nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var imeName, seatName uint32
	for time.Now().Before(deadline) && (imeName == 0 || seatName == 0) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			n, _ := cur.U32()
			iface, _ := cur.String()
			switch iface {
			case "zwp_text_input_manager_v3":
				imeName = n
			case "wl_seat":
				seatName = n
			}
		}
	}
	if imeName == 0 || seatName == 0 {
		t.Fatalf("globals ime=%d seat=%d", imeName, seatName)
	}

	const (
		compID = 3
		surfID = 4
		seatID = 5
		mgrID  = 6
		tiID   = 7
	)
	if err := wr.Send(2, 0, bindPayload(globalCompositor, "wl_compositor", 4, compID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(compID, 0, wayland.PutU32(nil, surfID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(seatName, "wl_seat", 5, seatID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(imeName, "zwp_text_input_manager_v3", 1, mgrID), nil); err != nil {
		t.Fatal(err)
	}
	p := wayland.PutU32(nil, tiID)
	p = wayland.PutU32(p, seatID)
	if err := wr.Send(mgrID, 1, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(tiID, zwpTextInputEnable, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(tiID, zwpTextInputCommit, nil, nil); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(2 * time.Second)
	var serial uint32
	var sawDone bool
	for time.Now().Before(deadline) && !sawDone {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == tiID && msg.Opcode == zwpTextInputDone {
			cur := wayland.NewCursor(msg.Payload, nil)
			serial, _ = cur.U32()
			sawDone = true
		}
	}
	if !sawDone || serial != 1 {
		t.Fatalf("done serial=%d sawDone=%v", serial, sawDone)
	}
}

func TestTextInputEnterLeaveFollowKeyboard(t *testing.T) {
	srvConn, cliConn := unixPair(t)
	scene := engine.NewScene()
	a := &engine.Actor{X: 0, Y: 0, Width: 200, Height: 200, Focused: true}
	scene.Add(a)
	srv := &Server{Scene: scene, ScreenW: 800, ScreenH: 600, log: log.New(io.Discard, "", 0)}
	c := newClient(srv, srvConn)
	c.kbdID = 11
	c.objs[11] = &object{id: 11, kind: kindKeyboard}
	surf := &surface{id: 20, actor: a}
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: surf}
	ti := &textInput{id: 30}
	c.objs[30] = &object{id: 30, kind: kindTextInput, ti: ti}
	rd := wayland.NewReader(cliConn)

	c.keyboardEnter(surf)
	ev := drainTextInput(t, cliConn, rd, 30)
	if n := countTI(ev, zwpTextInputEnter); n != 1 {
		t.Fatalf("enter %d events=%v", n, ev)
	}

	c.keyboardLeave(20)
	ev = drainTextInput(t, cliConn, rd, 30)
	if n := countTI(ev, zwpTextInputLeave); n != 1 {
		t.Fatalf("leave %d events=%v", n, ev)
	}
	if ti.entered != 0 {
		t.Fatal("entered cleared")
	}
}

func TestTextInputCommitWithoutEnterStillDones(t *testing.T) {
	srvConn, cliConn := unixPair(t)
	srv := &Server{Scene: engine.NewScene(), log: log.New(io.Discard, "", 0)}
	c := newClient(srv, srvConn)
	ti := &textInput{id: 30}
	c.objs[30] = &object{id: 30, kind: kindTextInput, ti: ti}
	rd := wayland.NewReader(cliConn)

	_ = c.reqTextInput(c.objs[30], zwpTextInputEnable, wayland.NewCursor(nil, nil))
	_ = c.reqTextInput(c.objs[30], zwpTextInputCommit, wayland.NewCursor(nil, nil))
	if ti.enabled {
		t.Fatal("enable before enter must be ignored")
	}
	ev := drainTextInput(t, cliConn, rd, 30)
	if n := countTI(ev, zwpTextInputDone); n != 1 {
		t.Fatalf("done %d events=%v", n, ev)
	}
	if ev[0].serial != 1 {
		t.Fatalf("serial %d", ev[0].serial)
	}
}

type tiEv struct {
	op     uint16
	surf   uint32
	serial uint32
}

func drainTextInput(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, id uint32) []tiEv {
	t.Helper()
	var out []tiEv
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return out
		}
		if msg.Object != id {
			continue
		}
		ev := tiEv{op: msg.Opcode}
		cur := wayland.NewCursor(msg.Payload, nil)
		switch msg.Opcode {
		case zwpTextInputEnter, zwpTextInputLeave:
			ev.surf, _ = cur.U32()
		case zwpTextInputDone:
			ev.serial, _ = cur.U32()
		}
		out = append(out, ev)
	}
}

func countTI(ev []tiEv, op uint16) int {
	n := 0
	for _, e := range ev {
		if e.op == op {
			n++
		}
	}
	return n
}

func dispatchUntil(s *Server, stop <-chan struct{}) {
	tck := time.NewTicker(time.Millisecond)
	defer tck.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tck.C:
			s.Dispatch()
		}
	}
}

func dialServer(t *testing.T, s *Server) (*net.UnixConn, *wayland.Writer, *wayland.Reader) {
	t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: s.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	return c, wayland.NewWriter(c), wayland.NewReader(c)
}
