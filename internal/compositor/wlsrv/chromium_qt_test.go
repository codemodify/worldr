package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestConfigureSendsActivated(t *testing.T) {
	srv, cli := unixPair(t)
	scene := engine.NewScene()
	c := newClient(&Server{Scene: scene, ScreenW: 1280, ScreenH: 720}, srv)
	rd := wayland.NewReader(cli)
	xs := &xdgSurface{id: 10}
	top := &xdgToplevel{id: 11, xdg: xs}
	xs.top = top
	c.objs[10] = &object{id: 10, kind: kindXdgSurface, xdgS: xs}
	c.objs[11] = &object{id: 11, kind: kindXdgToplevel, xdgT: top}
	if err := c.configure(xs); err != nil {
		t.Fatal(err)
	}
	if !drainToplevelActivated(t, cli, rd, 11) {
		t.Fatal("xdg_toplevel.configure missing activated")
	}
}

func TestNotifySurfaceEnterAndPreferredScale(t *testing.T) {
	srv, cli := unixPair(t)
	scene := engine.NewScene()
	c := newClient(&Server{Scene: scene, ScreenW: 1280, ScreenH: 720, scale120: 240}, srv)
	c.compVer = 6
	c.objs[7] = &object{id: 7, kind: kindOutput}
	s := &surface{id: 4}
	c.objs[4] = &object{id: 4, kind: kindSurface, surf: s}
	rd := wayland.NewReader(cli)
	c.notifySurfaceOutput(s)
	if !s.outEnter {
		t.Fatal("enter")
	}
	c.notifySurfaceOutput(s)
	_ = cli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var enter, scale bool
	for {
		msg, err := rd.Next()
		if err != nil {
			break
		}
		if msg.Object == 4 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			oid, _ := cur.U32()
			if oid != 7 {
				t.Fatalf("enter output %d", oid)
			}
			enter = true
		}
		if msg.Object == 4 && msg.Opcode == 2 {
			cur := wayland.NewCursor(msg.Payload, nil)
			sc, _ := cur.I32()
			if sc != 2 {
				t.Fatalf("preferred_buffer_scale %d want 2", sc)
			}
			scale = true
		}
	}
	if !enter || !scale {
		t.Fatalf("enter=%v scale=%v", enter, scale)
	}
}

func TestActivateSurfaceFocusAndKeyboard(t *testing.T) {
	srv, cli := unixPair(t)
	scene := engine.NewScene()
	c := newClient(&Server{Scene: scene, ScreenW: 800, ScreenH: 600}, srv)
	c.kbdID = 17
	act := &engine.Actor{X: 10, Y: 10, Width: 100, Height: 80}
	scene.Add(act)
	xs := &xdgSurface{id: 21}
	top := &xdgToplevel{id: 22, xdg: xs, inactive: true}
	xs.top = top
	s := &surface{id: 20, actor: act, xdg: xs}
	xs.surf = s
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: s}
	c.objs[21] = &object{id: 21, kind: kindXdgSurface, xdgS: xs}
	c.objs[22] = &object{id: 22, kind: kindXdgToplevel, xdgT: top}
	c.activateSurface(20)
	if !act.Focused {
		t.Fatal("activation must FocusActor")
	}
	if c.kbdSurf != 20 {
		t.Fatalf("keyboard.enter surf %d", c.kbdSurf)
	}
	if top.inactive {
		t.Fatal("activated configure")
	}
	_ = cli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	rd := wayland.NewReader(cli)
	var kbdEnter bool
	for {
		msg, err := rd.Next()
		if err != nil {
			break
		}
		if msg.Object == 17 && msg.Opcode == 1 {
			kbdEnter = true
		}
	}
	if !kbdEnter {
		t.Fatal("wl_keyboard.enter after xdg_activation.activate")
	}
}

func drainToplevelActivated(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, top uint32) bool {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return false
		}
		if msg.Object != top || msg.Opcode != 0 {
			continue
		}
		cur := wayland.NewCursor(msg.Payload, nil)
		_, _ = cur.I32()
		_, _ = cur.I32()
		arr, err := cur.Array()
		if err != nil {
			t.Fatal(err)
		}
		if len(arr) < 4 {
			return false
		}
		st := wayland.NewCursor(arr, nil)
		v, _ := st.U32()
		return v == xdgToplevelActivated
	}
}
