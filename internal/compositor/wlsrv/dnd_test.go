package wlsrv

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestDragDropTextBetweenClients(t *testing.T) {
	src, srcRD, srcConn, dst, dstRD, dstConn := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	act := &engine.Actor{X: 10, Y: 10, Width: 100, Height: 80, Workspace: 0}
	dst.srv.Scene.Add(act)
	dst.objs[40] = &object{id: 40, kind: kindSurface, surf: &surface{id: 40, actor: act}}

	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 0, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	src.srv.dragMotion(40, 40)
	got := drainDndEnter(t, dstConn, dstRD, 31)
	if got.offerID == 0 || !got.mimes[mimeTextPlain] {
		t.Fatalf("enter %+v", got)
	}
	src.srv.dragButtonUp(40, 40)
	if !drainDndDrop(t, dstConn, dstRD, 31) {
		t.Fatal("drop")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	wfd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if err := dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), []int{wfd})); err != nil {
		t.Fatal(err)
	}
	sendFD := drainSourceSend(t, srcConn, srcRD, 20)
	if sendFD < 0 {
		t.Fatal("source send")
	}
	if _, err := syscall.Write(sendFD, []byte("drag-hello")); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Close(sendFD)
	if string(readAll(t, r)) != "drag-hello" {
		t.Fatal("dnd paste")
	}
	if err := dst.reqDataOffer(dst.objs[got.offerID], 3, wayland.NewCursor(nil, nil)); err != nil {
		t.Fatal(err)
	}
	if !drainSourceOp(t, srcConn, srcRD, 20, wlDataSrcDndDone) {
		t.Fatal("dnd_finished")
	}
}

func TestDragDropPNGBetweenClients(t *testing.T) {
	src, srcRD, srcConn, dst, dstRD, dstConn := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	act := &engine.Actor{X: 0, Y: 0, Width: 64, Height: 64}
	dst.srv.Scene.Add(act)
	dst.objs[40] = &object{id: 40, kind: kindSurface, surf: &surface{id: 40, actor: act}}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, "image/png"), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 1)
	_ = src.reqDataDevice(src.objs[21], 0, wayland.NewCursor(p, nil))
	src.srv.dragMotion(8, 8)
	got := drainDndEnter(t, dstConn, dstRD, 31)
	if !got.mimes["image/png"] {
		t.Fatalf("%+v", got)
	}
	src.srv.dragButtonUp(8, 8)
	if !drainDndDrop(t, dstConn, dstRD, 31) {
		t.Fatal("drop")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	wfd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(wayland.PutString(nil, "image/png"), []int{wfd}))
	sendFD := drainSourceSend(t, srcConn, srcRD, 20)
	png := []byte{0x89, 'P', 'N', 'G', 1, 2}
	if _, err := syscall.Write(sendFD, png); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Close(sendFD)
	if string(readAll(t, r)) != string(png) {
		t.Fatal("png dnd")
	}
}

func TestDragDropOnDesktopCancels(t *testing.T) {
	src, srcRD, srcConn, _, _, _ := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 1)
	_ = src.reqDataDevice(src.objs[21], 0, wayland.NewCursor(p, nil))
	src.srv.dragMotion(400, 400) // empty
	src.srv.dragButtonUp(400, 400)
	if !drainSourceCancelled(t, srcConn, srcRD, 20) {
		t.Fatal("desktop drop has no icon canvas — source cancelled (follow-up)")
	}
}

type dndDrain struct {
	offerID uint32
	mimes   map[string]bool
}

func drainDndEnter(t *testing.T, conn interface{ SetReadDeadline(time.Time) error }, rd *wayland.Reader, dev uint32) dndDrain {
	t.Helper()
	out := dndDrain{mimes: map[string]bool{}}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return out
		}
		cur := wayland.NewCursor(msg.Payload, nil)
		switch {
		case msg.Object == dev && msg.Opcode == wlDataDevDataOffer:
			out.offerID, _ = cur.U32()
		case out.offerID != 0 && msg.Object == out.offerID && msg.Opcode == wlDataOfferOffer:
			m, _ := cur.String()
			out.mimes[m] = true
		case msg.Object == dev && msg.Opcode == wlDataDevEnter:
			return out
		}
	}
}

func drainDndDrop(t *testing.T, conn interface{ SetReadDeadline(time.Time) error }, rd *wayland.Reader, dev uint32) bool {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return false
		}
		if msg.Object == dev && msg.Opcode == wlDataDevDrop {
			return true
		}
	}
}

func drainSourceOp(t *testing.T, conn interface{ SetReadDeadline(time.Time) error }, rd *wayland.Reader, srcID uint32, op uint16) bool {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return false
		}
		if msg.Object == srcID && msg.Opcode == op {
			return true
		}
	}
}
