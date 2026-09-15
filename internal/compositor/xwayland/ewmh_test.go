package xwayland

import (
	"encoding/binary"
	"testing"
)

func TestParseWMClass(t *testing.T) {
	inst, class := ParseWMClass([]byte("xterm\x00XTerm\x00"))
	if inst != "xterm" || class != "XTerm" {
		t.Fatalf("got %q %q", inst, class)
	}
	inst, class = ParseWMClass([]byte("only"))
	if inst != "only" || class != "" {
		t.Fatalf("single %q %q", inst, class)
	}
	inst, class = ParseWMClass(nil)
	if inst != "" || class != "" {
		t.Fatalf("empty")
	}
}

func TestParseTitlePrefersNet(t *testing.T) {
	if ParseTitle([]byte("utf8\x00"), []byte("latin")) != "utf8" {
		t.Fatal("net")
	}
	if ParseTitle(nil, []byte("xterm\x00")) != "xterm" {
		t.Fatal("icccm")
	}
	if ParseTitle(nil, nil) != "" {
		t.Fatal("empty")
	}
}

func TestNoChromeFor(t *testing.T) {
	popup := map[uint32]struct{}{9: {}}
	if !NoChromeFor(true, false, nil, popup) {
		t.Fatal("override")
	}
	if !NoChromeFor(false, true, nil, popup) {
		t.Fatal("transient")
	}
	if !NoChromeFor(false, false, []uint32{9}, popup) {
		t.Fatal("menu type")
	}
	if NoChromeFor(false, false, []uint32{1}, popup) {
		t.Fatal("normal type should have SSD")
	}
}

func TestPickPendingSizeThenFIFO(t *testing.T) {
	a := &xWin{id: 1, w: 10, h: 20}
	b := &xWin{id: 2, w: 30, h: 40}
	picked, rest := pickPending([]*xWin{a, b}, 30, 40)
	if picked != b || len(rest) != 1 || rest[0] != a {
		t.Fatalf("size match")
	}
	picked, rest = pickPending([]*xWin{a}, 99, 99)
	if picked != a || len(rest) != 0 {
		t.Fatalf("single pending")
	}
	picked, rest = pickPending(nil, 1, 1)
	if picked != nil || rest != nil {
		t.Fatalf("empty")
	}
}

func TestParseAtoms32(t *testing.T) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:], 0x11)
	binary.LittleEndian.PutUint32(b[4:], 0x22)
	got := parseAtoms32(b)
	if len(got) != 2 || got[0] != 0x11 || got[1] != 0x22 {
		t.Fatalf("%v", got)
	}
}

func TestEWMHSupportedNames(t *testing.T) {
	names := EWMHSupportedNames()
	need := []string{"_NET_SUPPORTED", "_NET_ACTIVE_WINDOW", "_NET_WM_NAME", "_NET_CLIENT_LIST"}
	for _, n := range need {
		found := false
		for _, have := range names {
			if have == n {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s", n)
		}
	}
}

func TestFocusWindowSendsTakeFocus(t *testing.T) {
	rec := &recordConn{}
	xc := &xConn{c: rec, byteOrder: binary.LittleEndian, root: 1}
	wm := &XWM{
		xc:    xc,
		atoms: xAtoms{wmProtocols: 10, wmTakeFocus: 11, netActive: 12},
		wins:  map[uint32]*xWin{0x42: {id: 0x42, protocols: []uint32{11}}},
		popup: map[uint32]struct{}{},
	}
	wm.FocusWindow(0x42)
	if rec.n < 12 || rec.buf[0] != xOpSetInputFocus {
		t.Fatalf("expected SetInputFocus, got %x n=%d", rec.buf, rec.n)
	}
	if binary.LittleEndian.Uint32(rec.buf[4:]) != 0x42 {
		t.Fatalf("focus window")
	}
	foundTake := false
	foundActive := false
	i := 12
	for i < rec.n {
		op := rec.buf[i]
		switch op {
		case xOpSendEvent:
			if i+20 < rec.n && rec.buf[i+12] == xEvClientMessage {
				atom := binary.LittleEndian.Uint32(rec.buf[i+24:])
				if atom == 11 {
					foundTake = true
				}
			}
			i += 44
		case xOpChangeProperty:
			n := int(binary.LittleEndian.Uint16(rec.buf[i+2:])) * 4
			prop := binary.LittleEndian.Uint32(rec.buf[i+8:])
			if prop == 12 {
				foundActive = true
			}
			if n < 24 {
				n = 24
			}
			i += n
		default:
			i++
		}
	}
	if !foundTake {
		t.Fatal("missing WM_TAKE_FOCUS ClientMessage")
	}
	if !foundActive {
		t.Fatal("missing _NET_ACTIVE_WINDOW")
	}
}

func TestDeleteWindowSendsWMDelete(t *testing.T) {
	rec := &recordConn{}
	xc := &xConn{c: rec, byteOrder: binary.LittleEndian}
	wm := &XWM{
		xc:    xc,
		atoms: xAtoms{wmProtocols: 7, wmDelete: 8},
		wins:  map[uint32]*xWin{5: {id: 5, protocols: []uint32{8}}},
	}
	wm.DeleteWindow(5)
	if rec.n != 44 || rec.buf[0] != xOpSendEvent {
		t.Fatalf("packet %x n=%d", rec.buf, rec.n)
	}
	if rec.buf[12] != xEvClientMessage {
		t.Fatal("event")
	}
	if binary.LittleEndian.Uint32(rec.buf[24:]) != 8 {
		t.Fatalf("delete atom %d", binary.LittleEndian.Uint32(rec.buf[24:]))
	}
}

func TestDeleteWindowNoProtocolIsNoop(t *testing.T) {
	rec := &recordConn{}
	xc := &xConn{c: rec, byteOrder: binary.LittleEndian}
	wm := &XWM{xc: xc, atoms: xAtoms{wmDelete: 8}, wins: map[uint32]*xWin{5: {id: 5}}}
	wm.DeleteWindow(5)
	if rec.n != 0 {
		t.Fatalf("sent %d", rec.n)
	}
}

func TestConsumeMapFIFO(t *testing.T) {
	wm := &XWM{
		popup:   map[uint32]struct{}{},
		pending: []*xWin{{id: 1, title: "one", w: 8, h: 8}, {id: 2, title: "two", class: "XTerm", w: 80, h: 24}},
	}
	h, ok := wm.ConsumeMap(80, 24)
	if !ok || h.Win != 2 || h.Title != "two" || h.AppID != "XTerm" {
		t.Fatalf("%+v ok=%v", h, ok)
	}
	h, ok = wm.ConsumeMap(1, 1)
	if !ok || h.Win != 1 {
		t.Fatalf("leftover %+v ok=%v", h, ok)
	}
	if _, ok := wm.ConsumeMap(1, 1); ok {
		t.Fatal("empty")
	}
}

func TestXWinHintsNoChromeTransient(t *testing.T) {
	xw := &xWin{id: 3, title: "Find", class: "XCalc", transient: true, x: 10, y: 20, w: 40, h: 16}
	h := xw.hints(nil)
	if !h.NoChrome || h.Title != "Find" || h.AppID != "XCalc" || h.X != 10 {
		t.Fatalf("%+v", h)
	}
}
