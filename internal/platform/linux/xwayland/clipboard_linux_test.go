//go:build linux && cgo

package xwayland

import (
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

func TestRealXwaylandClipboardBothDirections(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated Xwayland/X11 acceptance")
	}
	if _, err := exec.LookPath("Xwayland"); err != nil {
		t.Skip(err)
	}
	server, err := apps.Open(320, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bridge, err := Open(server, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if initial := bridge.ClipboardOffer(); initial.Revision != 0 || initial.ID != 0 || initial.ExternalID != 0 || len(initial.MIMEs) != 0 {
		t.Fatalf("empty X11 selection published a synthetic change: %+v", initial)
	}
	wire := openWireClient(t, bridge)
	owner := wire.window()
	clipboard := wire.atom("CLIPBOARD")
	targets := wire.atom("TARGETS")
	utf8 := wire.atom("UTF8_STRING")
	property := wire.atom("WORLDR_TEST_CLIPBOARD")

	// An X11 owner advertises TARGETS. The bridge exposes only metadata until a
	// destination explicitly requests the matching MIME.
	wire.setSelectionOwner(owner, clipboard)
	request := wire.event(30)
	if got := binary.LittleEndian.Uint32(request[20:]); got != targets {
		t.Fatalf("target negotiation requested atom %d, want TARGETS %d", got, targets)
	}
	targetData := make([]byte, 8)
	binary.LittleEndian.PutUint32(targetData, targets)
	binary.LittleEndian.PutUint32(targetData[4:], utf8)
	requestor := binary.LittleEndian.Uint32(request[12:])
	requestProperty := binary.LittleEndian.Uint32(request[24:])
	wire.changeProperty(requestor, requestProperty, 4, 32, targetData)
	wire.selectionNotify(request, requestProperty)

	var x11Offer ClipboardOffer
	waitForX11(t, func() bool {
		x11Offer = bridge.ClipboardOffer()
		return x11Offer.ID != 0 && x11Offer.ExternalID == 0 && reflect.DeepEqual(x11Offer.MIMEs, []string{"text/plain;charset=utf-8"})
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := bridge.ReceiveClipboard(x11Offer.ID, x11Offer.MIMEs[0], int(writer.Fd())); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	request = wire.event(30)
	if got := binary.LittleEndian.Uint32(request[20:]); got != utf8 {
		t.Fatalf("clipboard data requested target %d, want UTF8_STRING %d", got, utf8)
	}
	requestor = binary.LittleEndian.Uint32(request[12:])
	requestProperty = binary.LittleEndian.Uint32(request[24:])
	wire.changeProperty(requestor, requestProperty, utf8, 8, []byte("copy from X11 λ"))
	wire.selectionNotify(request, requestProperty)
	if err := reader.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "copy from X11 λ" {
		t.Fatalf("X11 clipboard export %q: %v", data, err)
	}

	// Publishing an external offer claims CLIPBOARD but still defers the bytes.
	// TARGETS is answered locally; only UTF8_STRING creates a broker request.
	if err := bridge.OfferClipboard(91, []string{"text/plain;charset=utf-8"}); err != nil {
		t.Fatal(err)
	}
	if echo := bridge.ClipboardOffer(); echo.ExternalID != 91 || !reflect.DeepEqual(echo.MIMEs, []string{"text/plain;charset=utf-8"}) {
		t.Fatalf("published X11 offer was not acknowledged synchronously: %+v", echo)
	}
	var bridgeOwner uint32
	waitForX11(t, func() bool {
		bridgeOwner = wire.selectionOwner(clipboard)
		return bridgeOwner != 0 && bridgeOwner != owner
	})
	wire.convertSelection(owner, clipboard, targets, property)
	notify := wire.event(31)
	if got := binary.LittleEndian.Uint32(notify[20:]); got != property {
		t.Fatalf("TARGETS conversion failed with property %d", got)
	}
	format, propertyType, values := wire.property(owner, property, 4)
	if format != 32 || propertyType != 4 || !containsAtom(values, targets) || !containsAtom(values, utf8) {
		t.Fatalf("TARGETS property format=%d type=%d values=%v", format, propertyType, values)
	}

	wire.convertSelection(owner, clipboard, utf8, property)
	var requests []ClipboardRequest
	waitForX11(t, func() bool {
		requests = bridge.PollClipboardRequests()
		return len(requests) > 0
	})
	if len(requests) != 1 || requests[0].ExternalID != 91 || requests[0].MIME != "text/plain;charset=utf-8" {
		t.Fatalf("X11 clipboard request: %+v", requests)
	}
	destination := os.NewFile(uintptr(requests[0].FD), "X11 test clipboard")
	if destination == nil {
		t.Fatal("invalid clipboard request descriptor")
	}
	if _, err := destination.WriteString("copy into X11 λ"); err != nil {
		destination.Close()
		t.Fatal(err)
	}
	destination.Close()
	notify = wire.event(31)
	if got := binary.LittleEndian.Uint32(notify[20:]); got != property {
		t.Fatalf("UTF8 conversion failed with property %d", got)
	}
	format, propertyType, data = wire.property(owner, property, 0)
	if format != 8 || propertyType != utf8 || string(data) != "copy into X11 λ" {
		t.Fatalf("X11 clipboard import format=%d type=%d data=%q", format, propertyType, data)
	}

	if err := bridge.OfferClipboard(0, nil); err != nil {
		t.Fatal(err)
	}
	waitForX11(t, func() bool { return wire.selectionOwner(clipboard) == 0 })
	if err := bridge.ReceiveClipboard(x11Offer.ID, x11Offer.MIMEs[0], 0); err == nil {
		t.Fatal("stale X11 offer remained readable after ownership changed")
	}
}

func waitForX11(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(4 * time.Millisecond)
	}
	t.Fatal("timed out waiting for private X11 clipboard state")
}

func containsAtom(data []byte, atom uint32) bool {
	for len(data) >= 4 {
		if binary.LittleEndian.Uint32(data) == atom {
			return true
		}
		data = data[4:]
	}
	return false
}
