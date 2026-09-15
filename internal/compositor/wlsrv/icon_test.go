package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestPickIconClosestSize(t *testing.T) {
	a := &iconSnap{w: 8, h: 8}
	b := &iconSnap{w: 16, h: 16}
	c := &iconSnap{w: 32, h: 32}
	got := pickIcon([]*iconSnap{a, b, c}, 16)
	if got != b {
		t.Fatal("want 16")
	}
	if pickIcon(nil, 16) != nil {
		t.Fatal("empty")
	}
}

func TestSnapshotShmAndApply(t *testing.T) {
	pix := []byte{0x11, 0x22, 0x33, 0xff, 0x44, 0x55, 0x66, 0xff}
	b := &shmBuffer{pool: &shmPool{mem: pix}, offset: 0, w: 2, h: 1, stride: 8}
	snap := snapshotShm(b)
	if snap == nil || snap.w != 2 || snap.h != 1 || snap.pix[0] != 0x11 {
		t.Fatalf("%+v", snap)
	}
	a := &engine.Actor{}
	snap.apply(a)
	if !a.HasIcon() || a.IconW != 2 || a.IconPix[0] != 0x11 {
		t.Fatalf("%+v", a)
	}
	(*iconSnap)(nil).apply(a)
	if a.HasIcon() {
		t.Fatal("unset")
	}
}

func TestAdvertiseToplevelIconAndSizes(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-icon", scene, 800, 600, nil)
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
	var iconName uint32
	for time.Now().Before(deadline) && iconName == 0 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "xdg_toplevel_icon_manager_v1" {
				iconName = name
			}
		}
	}
	if iconName == 0 {
		t.Fatal("xdg_toplevel_icon_manager_v1 not advertised")
	}

	const mgrID = 3
	if err := wr.Send(2, 0, bindPayload(iconName, "xdg_toplevel_icon_manager_v1", 1, mgrID), nil); err != nil {
		t.Fatal(err)
	}

	var sizes []int32
	var sawDone bool
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawDone {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object != mgrID {
			continue
		}
		switch msg.Opcode {
		case 0:
			cur := wayland.NewCursor(msg.Payload, nil)
			sz, err := cur.I32()
			if err != nil {
				t.Fatal(err)
			}
			sizes = append(sizes, sz)
		case 1:
			sawDone = true
		}
	}
	if !sawDone || len(sizes) < 1 {
		t.Fatalf("sizes=%v done=%v", sizes, sawDone)
	}
	found16 := false
	for _, sz := range sizes {
		if sz == IconSize {
			found16 = true
		}
	}
	if !found16 {
		t.Fatalf("missing icon_size %d in %v", IconSize, sizes)
	}
}

func TestSetIconNullClearsActor(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-icon-unset", scene, 800, 600, nil)
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
	var iconName, xdgName uint32
	for time.Now().Before(deadline) && (iconName == 0 || xdgName == 0) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			switch iface {
			case "xdg_toplevel_icon_manager_v1":
				iconName = name
			case "xdg_wm_base":
				xdgName = name
			}
		}
	}
	if iconName == 0 || xdgName == 0 {
		t.Fatal("globals")
	}

	const (
		compID = 3
		surfID = 4
		xdgID  = 5
		xsID   = 6
		topID  = 7
		mgrID  = 8
	)
	if err := wr.Send(2, 0, bindPayload(globalCompositor, "wl_compositor", 4, compID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(compID, 0, wayland.PutU32(nil, surfID), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(xdgName, "xdg_wm_base", 5, xdgID), nil); err != nil {
		t.Fatal(err)
	}
	p := wayland.PutU32(nil, xsID)
	p = wayland.PutU32(p, surfID)
	if err := wr.Send(xdgID, 2, p, nil); err != nil { // get_xdg_surface
		t.Fatal(err)
	}
	if err := wr.Send(xsID, 1, wayland.PutU32(nil, topID), nil); err != nil { // get_toplevel
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(iconName, "xdg_toplevel_icon_manager_v1", 1, mgrID), nil); err != nil {
		t.Fatal(err)
	}
	// set_icon(toplevel, null)
	p = wayland.PutU32(nil, topID)
	p = wayland.PutU32(p, 0)
	if err := wr.Send(mgrID, 2, p, nil); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.Dispatch()
		time.Sleep(2 * time.Millisecond)
		s.mu.Lock()
		cl := append([]*Client(nil), s.clients...)
		s.mu.Unlock()
		for _, cln := range cl {
			if o := cln.objs[topID]; o != nil && o.xdgT != nil && o.xdgT.icon == nil {
				return
			}
		}
	}
	t.Fatal("set_icon null did not clear pending icon")
}
