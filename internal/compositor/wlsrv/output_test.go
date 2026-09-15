package wlsrv

import (
	"net"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestLayoutOutputsTilesLTR(t *testing.T) {
	outs := LayoutOutputs(800, 600, 2, []float64{1, 1.5})
	if len(outs) != 2 {
		t.Fatalf("n=%d", len(outs))
	}
	if outs[0].X != 0 || outs[0].W != 400 || outs[0].H != 600 || outs[0].Name != "WL-1" {
		t.Fatalf("out0 %+v", outs[0])
	}
	if outs[1].X != 400 || outs[1].W != 400 || outs[1].Name != "WL-2" {
		t.Fatalf("out1 %+v", outs[1])
	}
	if outs[0].Scale120 != 120 || outs[1].Scale120 != 180 {
		t.Fatalf("scales %d %d", outs[0].Scale120, outs[1].Scale120)
	}
	if HitOutput(outs, 10, 10) != 0 || HitOutput(outs, 400, 10) != 1 || HitOutput(outs, 799, 10) != 1 {
		t.Fatal("hit")
	}
	seams := OutputSeams(outs)
	if len(seams) != 1 || seams[0] != 400 {
		t.Fatalf("seams %v", seams)
	}
}

func TestLayoutOutputsRemainderLast(t *testing.T) {
	outs := LayoutOutputs(1000, 100, 3, nil)
	if outs[0].W != 333 || outs[1].W != 333 || outs[2].W != 334 {
		t.Fatalf("w %d %d %d", outs[0].W, outs[1].W, outs[2].W)
	}
	if outs[2].X+outs[2].W != 1000 {
		t.Fatalf("cover %d", outs[2].X+outs[2].W)
	}
}

func TestLayoutOutputsClamps(t *testing.T) {
	if n := len(LayoutOutputs(100, 100, 0, nil)); n != 1 {
		t.Fatalf("min %d", n)
	}
	if n := len(LayoutOutputs(100, 100, 9, nil)); n != MaxOutputs {
		t.Fatalf("max %d", n)
	}
}

func TestParseOutputScales(t *testing.T) {
	got, err := ParseOutputScales("1, 1.5")
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 1.5 {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := ParseOutputScales(""); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOutputScales("nope"); err == nil {
		t.Fatal("bad")
	}
	if _, err := ParseOutputScales("0"); err == nil {
		t.Fatal("zero")
	}
	long, err := ParseOutputScales("1,2,3,4,5")
	if err != nil || len(long) != MaxOutputs {
		t.Fatalf("trunc %v %v", long, err)
	}
}

func TestSetOutputScaleAtOneOutput(t *testing.T) {
	s := &Server{ScreenW: 800, ScreenH: 600}
	s.SetOutputs(LayoutOutputs(800, 600, 2, []float64{1, 1}))
	s.SetOutputScaleAt(1, 2)
	outs := s.OutputList()
	if outs[0].Scale120 != 120 || outs[1].Scale120 != 240 {
		t.Fatalf("scales %d %d", outs[0].Scale120, outs[1].Scale120)
	}
	if s.PreferredScale120ths() != 120 {
		t.Fatalf("primary scale %d", s.PreferredScale120ths())
	}
}

func TestSetOutputScaleWritesAll(t *testing.T) {
	s := &Server{ScreenW: 800, ScreenH: 600}
	s.SetOutputs(LayoutOutputs(800, 600, 2, []float64{1, 1.5}))
	s.SetOutputScale(2)
	outs := s.OutputList()
	if outs[0].Scale120 != 240 || outs[1].Scale120 != 240 {
		t.Fatalf("all %d %d", outs[0].Scale120, outs[1].Scale120)
	}
}

func TestRelayoutOutputsKeepsScales(t *testing.T) {
	s := &Server{ScreenW: 800, ScreenH: 600}
	s.SetOutputs(LayoutOutputs(800, 600, 2, []float64{1, 1.5}))
	s.ScreenW, s.ScreenH = 1000, 500
	s.RelayoutOutputs()
	outs := s.OutputList()
	if len(outs) != 2 || outs[0].W != 500 || outs[1].X != 500 || outs[1].H != 500 {
		t.Fatalf("%+v", outs)
	}
	if outs[0].Scale120 != 120 || outs[1].Scale120 != 180 {
		t.Fatalf("scales %d %d", outs[0].Scale120, outs[1].Scale120)
	}
}

func TestAdvertiseTwoWlOutputs(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	scene := engine.NewScene()
	s, err := Listen("wayland-outs", scene, 800, 600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetOutputs(LayoutOutputs(800, 600, 2, []float64{1, 1.5}))

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
	var names []uint32
	for time.Now().Before(deadline) && len(names) < 2 {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		if msg.Object == 2 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			name, _ := cur.U32()
			iface, _ := cur.String()
			if iface == "wl_output" {
				names = append(names, name)
			}
		}
	}
	if len(names) != 2 {
		t.Fatalf("wl_output globals %v", names)
	}
	if names[0] != globalOutput || names[1] != globalOutput2 {
		t.Fatalf("names %v want %d %d", names, globalOutput, globalOutput2)
	}

	const out0, out1 = 10, 11
	if err := wr.Send(2, 0, bindPayload(names[0], "wl_output", 4, out0), nil); err != nil {
		t.Fatal(err)
	}
	if err := wr.Send(2, 0, bindPayload(names[1], "wl_output", 4, out1), nil); err != nil {
		t.Fatal(err)
	}

	var x0, x1, w0, w1 int32
	var sc0, sc1 int32
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (w0 == 0 || w1 == 0 || sc0 == 0 || sc1 == 0) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		msg, err := rd.Next()
		if err != nil {
			continue
		}
		cur := wayland.NewCursor(msg.Payload, nil)
		switch {
		case msg.Object == out0 && msg.Opcode == 0: // geometry
			x0, _ = cur.I32()
		case msg.Object == out1 && msg.Opcode == 0:
			x1, _ = cur.I32()
		case msg.Object == out0 && msg.Opcode == 1: // mode
			_, _ = cur.U32()
			w0, _ = cur.I32()
		case msg.Object == out1 && msg.Opcode == 1:
			_, _ = cur.U32()
			w1, _ = cur.I32()
		case msg.Object == out0 && msg.Opcode == 3:
			sc0, _ = cur.I32()
		case msg.Object == out1 && msg.Opcode == 3:
			sc1, _ = cur.I32()
		}
	}
	if x0 != 0 || x1 != 400 || w0 != 400 || w1 != 400 {
		t.Fatalf("geom x=%d,%d w=%d,%d", x0, x1, w0, w1)
	}
	if sc0 != 1 || sc1 != 2 {
		t.Fatalf("scale %d %d", sc0, sc1)
	}
}

func TestSurfaceLeaveEnterOnOutputCross(t *testing.T) {
	srv, cli := unixPair(t)
	scene := engine.NewScene()
	s := &Server{Scene: scene, ScreenW: 800, ScreenH: 600}
	s.SetOutputs(LayoutOutputs(800, 600, 2, []float64{1, 1.5}))
	c := newClient(s, srv)
	c.compVer = 6
	c.objs[7] = &object{id: 7, kind: kindOutput, outIndex: 0}
	c.objs[8] = &object{id: 8, kind: kindOutput, outIndex: 1}
	act := &engine.Actor{X: 10, Y: 10, Width: 40, Height: 40}
	surf := &surface{id: 4, actor: act, fracID: 9}
	c.objs[4] = &object{id: 4, kind: kindSurface, surf: surf}
	c.objs[9] = &object{id: 9, kind: kindFracScale, surf: surf}

	c.notifySurfaceOutput(surf)
	if surf.outObj != 7 || surf.outIdx != 0 {
		t.Fatalf("enter obj=%d idx=%d", surf.outObj, surf.outIdx)
	}
	act.X = 500
	c.syncSurfaceOutput(surf)
	if surf.outObj != 8 || surf.outIdx != 1 {
		t.Fatalf("cross obj=%d idx=%d", surf.outObj, surf.outIdx)
	}

	_ = cli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	rd := wayland.NewReader(cli)
	var enter0, leave, enter1 bool
	var frac uint32
	for {
		msg, err := rd.Next()
		if err != nil {
			break
		}
		if msg.Object == 4 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			oid, _ := cur.U32()
			switch oid {
			case 7:
				enter0 = true
			case 8:
				enter1 = true
			}
		}
		if msg.Object == 4 && msg.Opcode == 1 {
			cur := wayland.NewCursor(msg.Payload, nil)
			oid, _ := cur.U32()
			if oid == 7 {
				leave = true
			}
		}
		if msg.Object == 9 && msg.Opcode == 0 {
			cur := wayland.NewCursor(msg.Payload, nil)
			frac, _ = cur.U32()
		}
	}
	if !enter0 || !leave || !enter1 {
		t.Fatalf("enter0=%v leave=%v enter1=%v", enter0, leave, enter1)
	}
	if frac != 180 {
		t.Fatalf("preferred_scale %d want 180", frac)
	}
}
