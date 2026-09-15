//go:build linux

package wlclient

import (
	"testing"

	"github.com/codemodify/worldr/internal/wayland"
)

func TestHandleHostOfferPicksText(t *testing.T) {
	w := &Window{dataDev: 10, hostOffers: map[uint32]*hostOffer{}}
	p := wayland.PutU32(nil, 42)
	if err := w.handleClip(wayland.Message{Object: 10, Opcode: wlDataDevOffer, Payload: p}); err != nil {
		t.Fatal(err)
	}
	p = wayland.PutString(nil, "image/png")
	if err := w.handleClip(wayland.Message{Object: 42, Opcode: wlDataOfferOffer, Payload: p}); err != nil {
		t.Fatal(err)
	}
	p = wayland.PutString(nil, "text/plain")
	if err := w.handleClip(wayland.Message{Object: 42, Opcode: wlDataOfferOffer, Payload: p}); err != nil {
		t.Fatal(err)
	}
	o := w.hostOffers[42]
	if o == nil || len(o.mimes) != 2 {
		t.Fatalf("%+v", o)
	}
	called := false
	w.onClipImport = func(primary bool, mime string, data []byte) { called = true }
	// wr is nil: receive is skipped, import must not run
	p = wayland.PutU32(nil, 42)
	if err := w.handleClip(wayland.Message{Object: 10, Opcode: wlDataDevSelection, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("no writer → no receive")
	}
}

func TestHandleHostSelectionIgnoresOwn(t *testing.T) {
	w := &Window{dataDev: 10, hostOffers: map[uint32]*hostOffer{
		7: {id: 7, mimes: []string{"text/plain"}},
	}}
	w.ownHost.Set(false, true)
	imported := false
	w.onClipImport = func(primary bool, mime string, data []byte) { imported = true }
	p := wayland.PutU32(nil, 7)
	if err := w.handleClip(wayland.Message{Object: 10, Opcode: wlDataDevSelection, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("echo of our host set_selection must be ignored")
	}
}

func TestHandleHostSelectionClearsOnZero(t *testing.T) {
	w := &Window{dataDev: 10, hostOffers: map[uint32]*hostOffer{}}
	imported := false
	w.onClipImport = func(primary bool, mime string, data []byte) { imported = true }
	p := wayland.PutU32(nil, 0)
	if err := w.handleClip(wayland.Message{Object: 10, Opcode: wlDataDevSelection, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("null selection")
	}
}

func TestOfferHostTextNoDeviceIsNoop(t *testing.T) {
	w := &Window{}
	w.OfferHostText(false, []string{"text/plain"})
	if w.hostSrc != 0 {
		t.Fatal("no mgr")
	}
}

func TestHostSourceCancelledClearsOwn(t *testing.T) {
	w := &Window{hostSrc: 5}
	w.ownHost.Set(false, true)
	if err := w.handleClip(wayland.Message{Object: 5, Opcode: wlDataSrcCancelled}); err != nil {
		t.Fatal(err)
	}
	if w.hostSrc != 0 || w.ownHost.Owns(false) {
		t.Fatal("cancelled")
	}
}

func TestPrimaryOfferTrack(t *testing.T) {
	w := &Window{primDev: 11, hostOffers: map[uint32]*hostOffer{}}
	p := wayland.PutU32(nil, 9)
	if err := w.handleClip(wayland.Message{Object: 11, Opcode: primDevOffer, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if w.hostOffers[9] == nil || !w.hostOffers[9].primary {
		t.Fatal("primary offer")
	}
}
