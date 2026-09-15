package wlsrv

import (
	"os"
	"syscall"
	"testing"

	"github.com/codemodify/worldr/internal/clipbridge"
	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

func TestImportHostTextOffersToClient(t *testing.T) {
	_, _, _, dst, dstRD, dstConn := newClipboardPair(t)
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	grantClipFocus(dst)

	exported := 0
	dst.srv.SetClipExport(func(primary bool, mimes []string) {
		exported++
		if primary {
			t.Fatal("clipboard import should not be primary")
		}
		if clipbridge.PickPlainMime(mimes) == "" {
			t.Fatal(mimes)
		}
	})
	dst.srv.ImportHostText(false, []byte("from-plasma"))
	if exported != 0 {
		t.Fatal("host import must not echo back to the host")
	}

	got := drainDataDev(t, dstConn, dstRD, 31)
	if got.offerID == 0 || got.selection != got.offerID {
		t.Fatalf("%+v", got)
	}
	if !got.mimes[mimeTextPlain] {
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
	if err := dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), []int{wfd})); err != nil {
		t.Fatal(err)
	}
	if string(readAll(t, r)) != "from-plasma" {
		t.Fatal("paste")
	}
}

func TestWorldrSetSelectionExportsText(t *testing.T) {
	src, _, _, _, _, _ := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, mimeTextPlain), nil))

	var got []string
	src.srv.SetClipExport(func(primary bool, mimes []string) {
		if primary {
			t.Fatal("clipboard")
		}
		got = append([]string(nil), mimes...)
	})
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if clipbridge.PickPlainMime(got) != mimeTextPlain {
		t.Fatalf("export %v", got)
	}
}

func TestSendSelectionToHostBytes(t *testing.T) {
	scene := engine.NewScene()
	srv := &Server{Scene: scene, ScreenW: 800, ScreenH: 600}
	srv.ImportHostText(false, []byte("abc"))
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
	srv.SendSelectionTo(false, mimeTextPlain, wfd)
	if string(readAll(t, r)) != "abc" {
		t.Fatal("send")
	}
}

func TestImportHostPNGOffersToClient(t *testing.T) {
	_, _, _, dst, dstRD, dstConn := newClipboardPair(t)
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	grantClipFocus(dst)
	png := []byte{0x89, 'P', 'N', 'G', 9, 8, 7}
	dst.srv.ImportHostPayload(false, "image/png", png)
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
	if string(readAll(t, r)) != string(png) {
		t.Fatal("host png paste")
	}
}

func TestImportHostJPEGOffersToClient(t *testing.T) {
	_, _, _, dst, dstRD, dstConn := newClipboardPair(t)
	dst.dataDev = 31
	dst.objs[31] = &object{id: 31, kind: kindDataDevice}
	grantClipFocus(dst)
	jpg := []byte{0xff, 0xd8, 0xff, 1, 2, 3}
	dst.srv.ImportHostPayload(false, "image/jpeg", jpg)
	got := drainDataDev(t, dstConn, dstRD, 31)
	if !got.mimes["image/jpeg"] {
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
	if err := dst.reqDataOffer(dst.objs[got.offerID], 1, wayland.NewCursor(wayland.PutString(nil, "image/jpg"), []int{wfd})); err != nil {
		t.Fatal(err)
	}
	if string(readAll(t, r)) != string(jpg) {
		t.Fatal("host jpeg paste via image/jpg alias")
	}
}

func TestWorldrSetSelectionExportsPNG(t *testing.T) {
	src, _, _, _, _, _ := newClipboardPair(t)
	src.objs[20] = &object{id: 20, kind: kindDataSource, src: &dataSource{id: 20, client: src}}
	src.dataDev = 21
	src.objs[21] = &object{id: 21, kind: kindDataDevice}
	_ = src.reqDataSource(src.objs[20], 0, wayland.NewCursor(wayland.PutString(nil, "image/png"), nil))
	var got []string
	src.srv.SetClipExport(func(primary bool, mimes []string) {
		got = append([]string(nil), mimes...)
	})
	p := wayland.PutU32(nil, 20)
	p = wayland.PutU32(p, 1)
	if err := src.reqDataDevice(src.objs[21], 1, wayland.NewCursor(p, nil)); err != nil {
		t.Fatal(err)
	}
	if clipbridge.PickImageMime(got) != "image/png" {
		t.Fatalf("export %v", got)
	}
}

func TestImportHostTextThenPNGMerges(t *testing.T) {
	scene := engine.NewScene()
	srv := &Server{Scene: scene, ScreenW: 800, ScreenH: 600}
	srv.ImportHostText(false, []byte("abc"))
	srv.ImportHostPayload(false, "image/png", []byte{1, 2, 3})
	if srv.clip == nil || srv.clip.source == nil {
		t.Fatal("sel")
	}
	src := srv.clip.source
	if string(src.hostPayload("text/plain")) != "abc" || string(src.hostPayload("image/png")) != "\x01\x02\x03" {
		t.Fatalf("merge %+v %+v", src.hostBytes, src.hostParts)
	}
}

func TestImportHostTextPrimary(t *testing.T) {
	_, _, _, dst, _, _ := newClipboardPair(t)
	dst.primDev = 41
	dst.objs[41] = &object{id: 41, kind: kindPrimDevice}
	dst.srv.ImportHostText(true, []byte("midclick"))
	if dst.srv.prim == nil || string(dst.srv.prim.source.hostBytes) != "midclick" {
		t.Fatal("primary import")
	}
}
