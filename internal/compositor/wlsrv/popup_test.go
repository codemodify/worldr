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

func TestPopupConfigureAndDestroy(t *testing.T) {
	c, rd, conn := newPopupClient(t)

	parentSurf := &surface{id: 20}
	parentXdg := &xdgSurface{id: 21, surf: parentSurf, top: &xdgToplevel{id: 22}}
	parentSurf.xdg = parentXdg
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: parentSurf}
	c.objs[21] = &object{id: 21, kind: kindXdgSurface, xdgS: parentXdg}

	childSurf := &surface{id: 30}
	childXdg := &xdgSurface{id: 31, surf: childSurf}
	childSurf.xdg = childXdg
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: childSurf}
	c.objs[31] = &object{id: 31, kind: kindXdgSurface, xdgS: childXdg}
	c.objs[32] = &object{id: 32, kind: kindPositioner, pos: &positioner{w: 80, h: 40, ox: 12, oy: 24}}

	p := wayland.PutU32(nil, 40)
	p = wayland.PutU32(p, 21)
	p = wayland.PutU32(p, 32)
	if err := c.getXdgPopup(childXdg, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if childXdg.pop == nil || childXdg.pop.id != 40 {
		t.Fatal("popup not attached")
	}

	ev := drainXdg(t, conn, rd)
	cfg, ok := ev.popupCfg[40]
	if !ok {
		t.Fatalf("no xdg_popup.configure: %+v", ev.popupCfg)
	}
	if cfg.x != 12 || cfg.y != 24 || cfg.w != 80 || cfg.h != 40 {
		t.Fatalf("configure %+v", cfg)
	}
	if ev.surfaceSerial[31] == 0 {
		t.Fatal("missing xdg_surface.configure")
	}

	_ = c.reqXdgPopup(c.objs[40], 0, wayland.NewCursor(nil, nil))
	if childSurf.actor != nil {
		t.Fatal("destroy must unmap")
	}
}

func TestPopupRepositionSendsToken(t *testing.T) {
	c, rd, conn := newPopupClient(t)
	childSurf := &surface{id: 30}
	childXdg := &xdgSurface{id: 31, surf: childSurf}
	childSurf.xdg = childXdg
	c.objs[31] = &object{id: 31, kind: kindXdgSurface, xdgS: childXdg}
	c.objs[32] = &object{id: 32, kind: kindPositioner, pos: &positioner{w: 16, h: 16, ox: 1, oy: 2}}
	p := wayland.PutU32(nil, 40)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 32)
	if err := c.getXdgPopup(childXdg, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	drainXdg(t, conn, rd)

	c.objs[33] = &object{id: 33, kind: kindPositioner, pos: &positioner{w: 20, h: 10, ox: 5, oy: 6}}
	rp := wayland.PutU32(nil, 33)
	rp = wayland.PutU32(rp, 99)
	if err := c.reqXdgPopup(c.objs[40], 2, wayland.NewCursor(rp, nil)); err != nil {
		t.Fatal(err)
	}
	ev := drainXdg(t, conn, rd)
	if ev.repositioned[40] != 99 {
		t.Fatalf("repositioned token %v", ev.repositioned)
	}
	cfg := ev.popupCfg[40]
	if cfg.x != 5 || cfg.y != 6 || cfg.w != 20 || cfg.h != 10 {
		t.Fatalf("reposition configure %+v", cfg)
	}
}

func TestPopupMapsAboveParentWithoutChrome(t *testing.T) {
	c, _, _ := newPopupClient(t)
	parent := &engine.Actor{X: 100, Y: 80, Width: 200, Height: 120, Focused: true}
	c.srv.Scene.Add(parent)
	ps := &surface{id: 20, actor: parent, attached: fakeBuf(200, 120)}
	px := &xdgSurface{id: 21, surf: ps, top: &xdgToplevel{id: 22}, acked: 1}
	ps.xdg = px
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: ps}
	c.objs[21] = &object{id: 21, kind: kindXdgSurface, xdgS: px}

	cs := &surface{id: 30}
	cx := &xdgSurface{id: 31, surf: cs, acked: 1}
	cs.xdg = cx
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: cs}
	c.objs[32] = &object{id: 32, kind: kindPositioner, pos: &positioner{w: 40, h: 20, ox: 8, oy: 16}}
	p := wayland.PutU32(nil, 40)
	p = wayland.PutU32(p, 21)
	p = wayland.PutU32(p, 32)
	if err := c.getXdgPopup(cx, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	cx.acked = 1
	cs.pending = fakeBuf(40, 20)
	c.commit(cs)
	if cs.actor == nil {
		t.Fatal("popup not mapped")
	}
	if !cs.actor.NoChrome {
		t.Fatal("popup must not take SSD")
	}
	if cs.actor.X != 108 || cs.actor.Y != 96 {
		t.Fatalf("popup pos %d,%d", cs.actor.X, cs.actor.Y)
	}
	if cs.actor.Owner != parent {
		t.Fatal("owner")
	}
	acts := c.srv.Scene.Actors()
	if acts[len(acts)-1] != cs.actor {
		t.Fatal("popup must stack above parent")
	}
}

func TestPointerEnterPopupUnderCursor(t *testing.T) {
	c, rd, conn, parent := newPointerClient(t, true)
	popA := &engine.Actor{X: 50, Y: 50, Width: 40, Height: 40, NoChrome: true, Owner: parent}
	c.srv.Scene.Add(popA)
	c.srv.Scene.Raise(popA)
	ps := &surface{id: 30, actor: popA}
	xs := &xdgSurface{surf: ps, pop: &xdgPopup{}}
	ps.xdg = xs
	xs.pop.xdg = xs
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: ps}

	c.pointerMotion(60, 60)
	ev := drainPointer(t, conn, rd, c.ptrID)
	var entered uint32
	for _, e := range ev {
		if e.op == wlPointerEnter {
			entered = e.surf
		}
	}
	if entered != 30 {
		t.Fatalf("enter surface %d want 30 events=%v", entered, ev)
	}
}

func TestPointerButtonReachesPopup(t *testing.T) {
	c, rd, conn, parent := newPointerClient(t, true)
	popA := &engine.Actor{X: 50, Y: 50, Width: 40, Height: 40, NoChrome: true, Owner: parent}
	c.srv.Scene.Add(popA)
	c.srv.Scene.Raise(popA)
	ps := &surface{id: 30, actor: popA}
	xs := &xdgSurface{surf: ps, pop: &xdgPopup{}}
	ps.xdg = xs
	xs.pop.xdg = xs
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: ps}

	c.pointerButton(60, 60, true)
	ev := drainPointer(t, conn, rd, c.ptrID)
	var entered uint32
	for _, e := range ev {
		if e.op == wlPointerEnter {
			entered = e.surf
		}
	}
	if entered != 30 {
		t.Fatalf("enter %d want 30: %v", entered, ev)
	}
	if n := countButton(ev, wlPointerPressed); n != 1 {
		t.Fatalf("press on popup %d: %v", n, ev)
	}
}

func TestSubsurfaceAttachDesync(t *testing.T) {
	c, _, _ := newPopupClient(t)
	parent := &engine.Actor{X: 40, Y: 30, Width: 80, Height: 60}
	c.srv.Scene.Add(parent)
	ps := &surface{id: 20, actor: parent, attached: fakeBuf(80, 60)}
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: ps}

	cs := &surface{id: 30}
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: cs}
	p := wayland.PutU32(nil, 50)
	p = wayland.PutU32(p, 30)
	p = wayland.PutU32(p, 20)
	if err := c.reqSubcomp(&object{id: 4, kind: kindSubcomp}, 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if cs.sub == nil || !cs.sub.sync {
		t.Fatal("default sync")
	}
	_ = c.reqSubsurface(c.objs[50], 5, wayland.NewCursor(nil, nil))
	pp := wayland.PutI32(nil, 6)
	pp = wayland.PutI32(pp, 9)
	_ = c.reqSubsurface(c.objs[50], 1, wayland.NewCursor(pp, nil))
	cs.pending = fakeBuf(16, 16)
	c.commit(cs)
	if cs.actor == nil {
		t.Fatal("subsurface not mapped")
	}
	if !cs.actor.NoChrome {
		t.Fatal("subsurface SSD")
	}
	if cs.actor.X != 46 || cs.actor.Y != 39 {
		t.Fatalf("sub pos %d,%d", cs.actor.X, cs.actor.Y)
	}
}

func TestSubsurfaceSyncWaitsForParentCommit(t *testing.T) {
	c, _, _ := newPopupClient(t)
	parent := &engine.Actor{X: 0, Y: 0, Width: 80, Height: 60}
	c.srv.Scene.Add(parent)
	ps := &surface{id: 20, actor: parent, attached: fakeBuf(80, 60)}
	c.objs[20] = &object{id: 20, kind: kindSurface, surf: ps}
	cs := &surface{id: 30}
	c.objs[30] = &object{id: 30, kind: kindSurface, surf: cs}
	p := wayland.PutU32(nil, 50)
	p = wayland.PutU32(p, 30)
	p = wayland.PutU32(p, 20)
	if err := c.reqSubcomp(&object{kind: kindSubcomp}, 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	cs.pending = fakeBuf(8, 8)
	c.commit(cs)
	if cs.actor != nil {
		t.Fatal("sync child must wait for parent commit")
	}
	c.commit(ps)
	if cs.actor == nil {
		t.Fatal("parent commit must apply sync child")
	}
}

type xdgDrain struct {
	popupCfg      map[uint32]struct{ x, y, w, h int32 }
	surfaceSerial map[uint32]uint32
	repositioned  map[uint32]uint32
}

func newPopupClient(t *testing.T) (*Client, *wayland.Reader, *net.UnixConn) {
	t.Helper()
	srvConn, cliConn := unixPair(t)
	scene := engine.NewScene()
	srv := &Server{
		Scene:   scene,
		ScreenW: 800,
		ScreenH: 600,
		log:     log.New(io.Discard, "", 0),
	}
	c := newClient(srv, srvConn)
	return c, wayland.NewReader(cliConn), cliConn
}

func drainXdg(t *testing.T, conn *net.UnixConn, rd *wayland.Reader) xdgDrain {
	t.Helper()
	out := xdgDrain{
		popupCfg:      map[uint32]struct{ x, y, w, h int32 }{},
		surfaceSerial: map[uint32]uint32{},
		repositioned:  map[uint32]uint32{},
	}
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return out
		}
		cur := wayland.NewCursor(msg.Payload, nil)
		switch {
		case msg.Opcode == xdgPopupRepositioned:
			tok, _ := cur.U32()
			out.repositioned[msg.Object] = tok
		case msg.Opcode == 0 && len(msg.Payload) >= 16:
			var cfg struct{ x, y, w, h int32 }
			cfg.x, _ = cur.I32()
			cfg.y, _ = cur.I32()
			cfg.w, _ = cur.I32()
			cfg.h, _ = cur.I32()
			out.popupCfg[msg.Object] = cfg
		case msg.Opcode == 0 && len(msg.Payload) == 4:
			ser, _ := cur.U32()
			out.surfaceSerial[msg.Object] = ser
		}
	}
}

func fakeBuf(w, h int) *object {
	n := w * h * 4
	mem := make([]byte, n)
	return &object{buf: &shmBuffer{
		pool:   &shmPool{mem: mem, size: n, live: 1},
		w:      w,
		h:      h,
		stride: w * 4,
	}}
}
