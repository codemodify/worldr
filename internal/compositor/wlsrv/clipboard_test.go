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

func TestMatchMimePlainAlias(t *testing.T) {
	offered := []string{mimeTextPlain}
	if matchMime(offered, mimeTextUTF8) != mimeTextPlain {
		t.Fatal("utf-8 request should alias to text/plain")
	}
	if matchMime(offered, mimeTextPlain) != mimeTextPlain {
		t.Fatal("exact")
	}
	if matchMime(offered, "image/png") != "" {
		t.Fatal("unknown mime")
	}
}

func TestClipboardOfferSetSelectionReceive(t *testing.T) {
	src, srcRD, srcConn, dst, dstRD, dstConn := newClipboardPair(t)

	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	grantClipFocus(dst)

	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextUTF8), nil))

	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}

	got := drainDataDev(t, dstConn, dstRD, 31)
	if got.offerID == 0 {
		t.Fatal("no data_offer")
	}
	if !got.mimes[mimeTextPlain] || !got.mimes[mimeTextUTF8] {
		t.Fatalf("mimes %v", got.mimes)
	}
	if got.selection != got.offerID {
		t.Fatalf("selection %d offer %d", got.selection, got.offerID)
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
	pay := wayland.PutString(nil, mimeTextUTF8)
	if err := dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(pay, []int{wfd})); err != nil {
		t.Fatal(err)
	}

	sendFD := drainSourceSend(t, srcConn, srcRD, 20)
	if sendFD < 0 {
		t.Fatal("source did not get send fd")
	}
	if _, err := syscall.Write(sendFD, []byte("hello-worldr")); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Close(sendFD)
	gotBytes := readAll(t, r)
	if string(gotBytes) != "hello-worldr" {
		t.Fatalf("paste %q", gotBytes)
	}
}

func TestClipboardOfferReceivePNG(t *testing.T) {
	src, srcRD, srcConn, dst, dstRD, dstConn := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	grantClipFocus(dst)

	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, "image/png"), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	got := drainDataDev(t, dstConn, dstRD, 31)
	if !got.mimes["image/png"] {
		t.Fatalf("mimes %v", got.mimes)
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
	if err := dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(wayland.PutString(nil, "image/png"), []int{wfd})); err != nil {
		t.Fatal(err)
	}
	sendFD := drainSourceSend(t, srcConn, srcRD, 20)
	if sendFD < 0 {
		t.Fatal("send")
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	if _, err := syscall.Write(sendFD, png); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Close(sendFD)
	if string(readAll(t, r)) != string(png) {
		t.Fatal("png paste")
	}
}

func TestClipboardReplaceCancelsOldSource(t *testing.T) {
	src, srcRD, srcConn, _, _, _ := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.objs[22] = &object{id: 22, kind: kindDataSource, src: &dataSource{id: 22, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	_ = src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil))
	drainDataDev(t, srcConn, srcRD, 21)

	_ = src.reqDataSource(src.objs[22], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	p = wayland.PutU32(nil, 22)
	p = wayland.PutU32(p, 2)
	_ = src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil))
	if !drainSourceCancelled(t, srcConn, srcRD, 20) {
		t.Fatal("expected cancelled on replaced source")
	}
}

func grantClipFocus(c *Client) {
	if c.kbdID == 0 {
		c.kbdID = 99
	}
	if c.kbdSurf == 0 {
		c.kbdSurf = 1
	}
}

// TestGetDataDeviceSilentUntilKeyboardFocus is the ark/Brave Qt6 crash:
// selection(nil) on get_data_device during the init roundtrip makes
// QWaylandDataDevice call platformIntegration()->clipboard() before
// createPlatformIntegration has published the integration (SIGSEGV).
func TestGetDataDeviceSilentUntilKeyboardFocus(t *testing.T) {
	c, rd, conn, _, _, _ := newClipboardPair(t)
	p := wayland.PutU32(nil, 21)
	p = wayland.PutU32(p, 4)
	if err := c.reqDataDevMgr(&object{}, 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if c.dataDev != 21 {
		t.Fatalf("dataDev %d", c.dataDev)
	}
	_ = conn.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	if msg, err := rd.Next(); err == nil {
		t.Fatalf("get_data_device must not emit events (got obj=%d op=%d) — Qt6 SEGVs on selection during init", msg.Object, msg.Opcode)
	}

	c.kbdID = 17
	surf := &surface{id: 40}
	c.keyboardEnter(surf)
	c.kbdSurf = 40

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	sawNull := false
	for {
		msg, err := rd.Next()
		if err != nil {
			break
		}
		if msg.Object != 21 || msg.Opcode != wlDataDevSelection {
			continue
		}
		cur := wayland.NewCursor(msg.Payload, nil)
		id, err := cur.U32()
		if err != nil {
			t.Fatal(err)
		}
		if id != 0 {
			t.Fatalf("empty clipboard must marshal selection(null), got id=%d", id)
		}
		if cur.Remaining() != 0 {
			t.Fatalf("selection payload extra %d bytes", cur.Remaining())
		}
		sawNull = true
	}
	if !sawNull {
		t.Fatal("keyboard.enter must send data_device.selection (null when empty)")
	}
}

func TestGetDataDeviceWhileFocusedSendsCurrent(t *testing.T) {
	src, _, _, dst, dstRD, dstConn := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}

	grantClipFocus(dst)
	p = wayland.PutU32(nil, 31)
	p = wayland.PutU32(p, 4)
	if err := dst.reqDataDevMgr(&object{}, 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	got := drainDataDev(t, dstConn, dstRD, 31)
	if got.offerID == 0 || got.selection != got.offerID {
		t.Fatalf("focused get_data_device must receive current selection: %+v", got)
	}
	if !got.mimes[mimeTextPlain] {
		t.Fatalf("mimes %v", got.mimes)
	}
}

type dataDevDrain struct {
	offerID   uint32
	selection uint32
	mimes     map[string]bool
}

func newClipboardPair(t *testing.T) (src *Client, srcRD *wayland.Reader, srcConn *net.UnixConn, dst *Client, dstRD *wayland.Reader, dstConn *net.UnixConn) {
	t.Helper()
	scene := engine.NewScene()
	srv := &Server{Scene: scene, ScreenW: 800, ScreenH: 600, log: log.New(io.Discard, "", 0)}
	srcSrv, srcCli := unixPair(t)
	dstSrv, dstCli := unixPair(t)
	src = newClient(srv, srcSrv)
	dst = newClient(srv, dstSrv)
	srv.clients = []*Client{src, dst}
	return src, wayland.NewReader(srcCli), srcCli, dst, wayland.NewReader(dstCli), dstCli
}

func drainDataDev(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, dev uint32) dataDevDrain {
	t.Helper()
	out := dataDevDrain{mimes: map[string]bool{}}
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
		case msg.Object == dev && msg.Opcode == wlDataDevSelection:
			out.selection, _ = cur.U32()
		case out.offerID != 0 && msg.Object == out.offerID && msg.Opcode == wlDataOfferOffer:
			m, _ := cur.String()
			out.mimes[m] = true
		}
	}
}

func drainSourceSend(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, srcID uint32) int {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return -1
		}
		if msg.Object == srcID && msg.Opcode == wlDataSrcSend {
			fd, err := rd.TakeFD()
			if err != nil {
				return -1
			}
			return fd
		}
	}
}

func drainSourceCancelled(t *testing.T, conn *net.UnixConn, rd *wayland.Reader, srcID uint32) bool {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		msg, err := rd.Next()
		if err != nil {
			return false
		}
		if msg.Object == srcID && msg.Opcode == wlDataSrcCancelled {
			return true
		}
	}
}

func readAll(t *testing.T, r *os.File) []byte {
	t.Helper()
	_ = r.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
