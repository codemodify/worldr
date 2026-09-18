//go:build linux && cgo

package xwayland

import (
	"encoding/binary"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

func TestRealXwaylandXDNDCopyCancelAndTimeout(t *testing.T) {
	if os.Getenv("WORLDR_TEST_XWAYLAND") != "1" {
		t.Skip("set WORLDR_TEST_XWAYLAND=1 for isolated Xwayland/X11 acceptance")
	}
	server, err := apps.Open(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bridge, err := Open(server, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	source, target := openWireClient(t, bridge), openWireClient(t, bridge)
	sourceWindow, targetWindow := source.windowSize(180, 100), target.windowSize(180, 100)
	source.title(sourceWindow, "XDND source")
	target.title(targetWindow, "XDND target")
	aware := target.atom("XdndAware")
	selection := source.atom("XdndSelection")
	typeList := source.atom("XdndTypeList")
	enterAtom := source.atom("XdndEnter")
	positionAtom := source.atom("XdndPosition")
	status := source.atom("XdndStatus")
	leaveAtom := source.atom("XdndLeave")
	dropAtom := source.atom("XdndDrop")
	finished := source.atom("XdndFinished")
	actionCopy := source.atom("XdndActionCopy")
	utf8 := source.atom("text/plain;charset=utf-8")
	targets := source.atom("TARGETS")
	property := target.atom("WORLDR_XDND_PAYLOAD")
	version := make([]byte, 4)
	binary.LittleEndian.PutUint32(version, 5)
	target.changeProperty(targetWindow, aware, 4, 32, version)
	types := make([]byte, 4)
	binary.LittleEndian.PutUint32(types, utf8)
	source.changeProperty(sourceWindow, typeList, 4, 32, types)
	source.mapped(sourceWindow, true)
	target.mapped(targetWindow, true)

	var sourceSurface, targetSurface uint64
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := bridge.Poll(); err != nil {
				t.Fatal(err)
			}
			surfaces, err := server.Poll()
			if err != nil {
				t.Fatal(err)
			}
			for _, surface := range surfaces {
				switch surface.Title {
				case "XDND source":
					sourceSurface = surface.ID
				case "XDND target":
					targetSurface = surface.ID
				}
			}
			for _, window := range bridge.windows {
				if window.ID == sourceWindow {
					sourceSurface = window.SurfaceID
				}
				if window.ID == targetWindow {
					targetSurface = window.SurfaceID
				}
			}
			if check() {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
		bridge.mu.Lock()
		snapshot := append([]Window(nil), bridge.snapshot...)
		bridge.mu.Unlock()
		t.Fatalf("timed out waiting for %s; offer=%+v snapshot=%+v windows=%+v log=%q", label, bridge.XDNDOffer(), snapshot, bridge.windows, bridge.log.String())
	}
	wait("two associated X11 surfaces", func() bool { return sourceSurface != 0 && targetSurface != 0 })

	begin := func() {
		t.Helper()
		source.setSelectionOwner(sourceWindow, selection)
		_ = source.selectionOwner(selection)
		wait("XDND source metadata", func() bool {
			return bridge.DragActive(sourceSurface) && reflect.DeepEqual(bridge.XDNDOffer().MIMEs, []string{"text/plain;charset=utf-8"})
		})
	}
	clear := func() {
		t.Helper()
		source.setSelectionOwner(0, selection)
		_ = source.selectionOwner(selection)
		wait("XDND selection clear", func() bool { return !bridge.XDNDOffer().Active })
	}
	accept := func(timestamp, wantVersion uint32) uint32 {
		t.Helper()
		if err := bridge.DragMotion(sourceSurface, targetSurface, 12, 13, timestamp); err != nil {
			t.Fatal(err)
		}
		enter := target.event(33)
		if binary.LittleEndian.Uint32(enter[8:]) != enterAtom || enter[1] != 32 || binary.LittleEndian.Uint32(enter[16:])>>24 != wantVersion || binary.LittleEndian.Uint32(enter[20:]) != utf8 {
			t.Fatalf("malformed XdndEnter: %x", enter)
		}
		proxy := binary.LittleEndian.Uint32(enter[12:])
		position := target.event(33)
		if binary.LittleEndian.Uint32(position[8:]) != positionAtom || binary.LittleEndian.Uint32(position[28:]) != actionCopy {
			t.Fatalf("malformed XdndPosition: %x", position)
		}
		target.clientMessage(proxy, status, [5]uint32{targetWindow, 1, 0, 0, actionCopy})
		wait("target acceptance", func() bool { return bridge.XDNDOffer().Accepted })
		forwarded := source.event(33)
		if binary.LittleEndian.Uint32(forwarded[8:]) != status || binary.LittleEndian.Uint32(forwarded[12:]) != targetWindow || binary.LittleEndian.Uint32(forwarded[28:]) != actionCopy {
			t.Fatalf("source did not receive accepted XdndStatus: %x", forwarded)
		}
		return proxy
	}

	begin()
	proxy := accept(41, 5)
	if err := bridge.DropXDND(sourceSurface, targetSurface, 42); err != nil {
		t.Fatal(err)
	}
	drop := target.event(33)
	if binary.LittleEndian.Uint32(drop[8:]) != dropAtom || binary.LittleEndian.Uint32(drop[12:]) != proxy {
		t.Fatalf("malformed XdndDrop: %x", drop)
	}
	target.convertSelection(targetWindow, selection, utf8, property)
	request := source.event(30)
	if binary.LittleEndian.Uint32(request[16:]) != selection || binary.LittleEndian.Uint32(request[20:]) != utf8 {
		t.Fatalf("unexpected XDND selection request: %x", request)
	}
	payload := []byte("worldr X11 drag λ")
	requestor, requestProperty := binary.LittleEndian.Uint32(request[12:]), binary.LittleEndian.Uint32(request[24:])
	source.changeProperty(requestor, requestProperty, utf8, 8, payload)
	source.selectionNotify(request, requestProperty)
	notify := target.event(31)
	if binary.LittleEndian.Uint32(notify[20:]) != property {
		t.Fatalf("XDND selection conversion failed: %x", notify)
	}
	format, propertyType, received := target.property(targetWindow, property, 0)
	if format != 8 || propertyType != utf8 || string(received) != string(payload) {
		t.Fatalf("XDND payload format=%d type=%d data=%q", format, propertyType, received)
	}
	target.clientMessage(proxy, finished, [5]uint32{targetWindow, 1, actionCopy})
	completed := source.event(33)
	if binary.LittleEndian.Uint32(completed[8:]) != finished || binary.LittleEndian.Uint32(completed[16:]) != 1 || binary.LittleEndian.Uint32(completed[20:]) != actionCopy {
		t.Fatalf("source did not receive successful XdndFinished: %x", completed)
	}
	wait("successful XDND completion", func() bool { return !bridge.XDNDOffer().Active })
	clear()

	// Version 3/4 destinations leave XdndFinished flags unset; receipt after a
	// drop still means success. The proxy normalizes that for a modern source.
	binary.LittleEndian.PutUint32(version, 4)
	target.changeProperty(targetWindow, aware, 4, 32, version)
	begin()
	proxy = accept(47, 4)
	if err := bridge.DropXDND(sourceSurface, targetSurface, 48); err != nil {
		t.Fatal(err)
	}
	_ = target.event(33)
	target.clientMessage(proxy, finished, [5]uint32{targetWindow})
	legacyFinished := source.event(33)
	if binary.LittleEndian.Uint32(legacyFinished[8:]) != finished || binary.LittleEndian.Uint32(legacyFinished[16:]) != 1 || binary.LittleEndian.Uint32(legacyFinished[20:]) != actionCopy {
		t.Fatalf("v4 XdndFinished was not normalized: %x", legacyFinished)
	}
	wait("legacy XDND completion", func() bool { return !bridge.XDNDOffer().Active })
	clear()
	binary.LittleEndian.PutUint32(version, 5)
	target.changeProperty(targetWindow, aware, 4, 32, version)

	begin()
	_ = accept(51, 5)
	if err := bridge.CancelXDND(); err != nil {
		t.Fatal(err)
	}
	leave := target.event(33)
	if binary.LittleEndian.Uint32(leave[8:]) != leaveAtom {
		t.Fatalf("cancel did not send XdndLeave: %x", leave)
	}
	wait("explicit XDND cancellation", func() bool { return !bridge.XDNDOffer().Active })
	clear()

	begin()
	_ = accept(61, 5)
	if err := bridge.DropXDND(sourceSurface, targetSurface, 62); err != nil {
		t.Fatal(err)
	}
	_ = target.event(33)
	source.conn.SetDeadline(time.Now().Add(4 * time.Second))
	timedOut := source.event(33)
	if binary.LittleEndian.Uint32(timedOut[8:]) != finished || binary.LittleEndian.Uint32(timedOut[16:]) != 0 {
		t.Fatalf("unfinished drop did not fail after bound: %x", timedOut)
	}
	wait("post-drop timeout cleanup", func() bool { return !bridge.XDNDOffer().Active })
	clear()

	// Sources with three or fewer types need not publish XdndTypeList. Fall
	// back to the selection's bounded TARGETS negotiation in that case.
	source.changeProperty(sourceWindow, typeList, 4, 32, nil)
	source.setSelectionOwner(sourceWindow, selection)
	_ = source.selectionOwner(selection)
	targetRequest := source.event(30)
	if binary.LittleEndian.Uint32(targetRequest[20:]) != targets {
		t.Fatalf("XDND metadata fallback requested %#x, want TARGETS %#x", binary.LittleEndian.Uint32(targetRequest[20:]), targets)
	}
	targetData := make([]byte, 4)
	binary.LittleEndian.PutUint32(targetData, utf8)
	requestor = binary.LittleEndian.Uint32(targetRequest[12:])
	requestProperty = binary.LittleEndian.Uint32(targetRequest[24:])
	source.changeProperty(requestor, requestProperty, 4, 32, targetData)
	source.selectionNotify(targetRequest, requestProperty)
	wait("TARGETS-derived XDND metadata", func() bool {
		return bridge.DragActive(sourceSurface) && reflect.DeepEqual(bridge.XDNDOffer().MIMEs, []string{"text/plain;charset=utf-8"})
	})
	if err := bridge.CancelXDND(); err != nil {
		t.Fatal(err)
	}
	clear()

	// An accepted destination cannot remain valid after its X11 window unmaps.
	source.changeProperty(sourceWindow, typeList, 4, 32, types)
	begin()
	_ = accept(71, 5)
	target.mapped(targetWindow, false)
	_ = target.selectionOwner(selection)
	wait("target unmap cancellation", func() bool { return !bridge.XDNDOffer().Active })
	unmappedLeave := target.event(33)
	if binary.LittleEndian.Uint32(unmappedLeave[8:]) != leaveAtom {
		t.Fatalf("target unmap did not send XdndLeave: %x", unmappedLeave)
	}
	clear()
}
