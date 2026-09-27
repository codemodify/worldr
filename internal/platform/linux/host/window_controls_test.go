//go:build linux && cgo

package host

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// A protocol peer verifies CSD negotiation, transparent surface state and the
// actual seat/press serial sent for move/resize, rather than mocking Go methods.
func TestStandaloneWindowControlsAndTransparentDecoration(t *testing.T) {
	type request struct {
		kind         string
		serial, edge uint32
	}
	requests := make(chan request, 16)
	fakeCompositor(t, func(conn net.Conn) {
		var registry, compositor, wm, seat, manager, surface, xdg, top, decoration, pointer uint32
		configured, sent := false, false
		for {
			var header [8]byte
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				return
			}
			object, word := binary.NativeEndian.Uint32(header[:]), binary.NativeEndian.Uint32(header[4:])
			if word>>16 < 8 {
				return
			}
			payload := make([]byte, int(word>>16)-8)
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			opcode := word & 0xffff
			arg := func(n int) uint32 { return binary.NativeEndian.Uint32(payload[n*4:]) }
			switch {
			case object == 1 && opcode == 1:
				registry = arg(0)
			case object == 1 && opcode == 0:
				writeGlobal(conn, registry, 1, "wl_compositor", 4)
				writeGlobal(conn, registry, 2, "xdg_wm_base", 1)
				writeGlobal(conn, registry, 3, "wl_seat", 5)
				writeGlobal(conn, registry, 4, "zxdg_decoration_manager_v1", 1)
				writeMessage(conn, arg(0), 0, []uint32{1})
			case object == registry && opcode == 0:
				id := binary.NativeEndian.Uint32(payload[len(payload)-4:])
				switch arg(0) {
				case 1:
					compositor = id
				case 2:
					wm = id
				case 3:
					seat = id
					writeMessage(conn, seat, 0, []uint32{1})
				case 4:
					manager = id
				}
			case object == compositor && opcode == 0:
				surface = arg(0)
			case object == wm && opcode == 2:
				xdg = arg(0)
			case object == xdg && opcode == 1:
				top = arg(0)
			case object == manager && opcode == 1:
				decoration = arg(0)
			case object == decoration && opcode == 1:
				requests <- request{kind: "decoration", edge: arg(0)}
			case object == seat && opcode == 0:
				pointer = arg(0)
			case object == surface && opcode == 4:
				requests <- request{kind: "opaque-region", edge: arg(0)}
			case object == surface && opcode == 6 && !configured:
				writeMessage(conn, decoration, 0, []uint32{1})
				writeMessage(conn, top, 0, []uint32{640, 480, 0})
				writeMessage(conn, xdg, 0, []uint32{1})
				configured = true
			case object == top && opcode == 5:
				requests <- request{kind: "move", serial: arg(1), edge: arg(0)}
				writeMessage(conn, pointer, 3, []uint32{42, 110, 0x110, 1})
			case object == top && opcode == 6:
				requests <- request{kind: "resize", serial: arg(1), edge: arg(2)}
			case object == top && (opcode == 9 || opcode == 10):
				states := []uint32{640, 480, 0}
				if opcode == 9 {
					states = []uint32{800, 600, 4, 1}
				}
				writeMessage(conn, top, 0, states)
				writeMessage(conn, xdg, 0, []uint32{50 + opcode})
			case object == top && opcode == 13:
				requests <- request{kind: "minimize"}
			}
			if configured && pointer != 0 && !sent {
				writeMessage(conn, pointer, 0, []uint32{20, surface, 100 * 256, 30 * 256})
				writeMessage(conn, pointer, 3, []uint32{41, 100, 0x110, 1})
				sent = true
			}
		}
	})
	window, err := openOptions("transparent controls", 640, 480, Options{ClientDecorated: true, Transparent: true}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer window.Close()
	pollUntil := func(done func([]Event) bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			events, err := window.Poll(nil)
			if err != nil {
				t.Fatal(err)
			}
			if done(events) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("window state did not arrive")
	}
	pressed := func(events []Event) bool {
		for _, e := range events {
			if e.Kind == Down {
				return true
			}
		}
		return false
	}
	pollUntil(pressed)
	if !window.BeginMove() || window.BeginMove() {
		t.Fatal("move serial was absent or reused")
	}
	window.SetMaximized(true)
	window.Minimize()
	window.SetTitle("updated")
	secondPress := false
	pollUntil(func(events []Event) bool {
		secondPress = secondPress || pressed(events)
		return secondPress && window.Maximized()
	})
	if window.BeginResize(3) {
		t.Fatal("accepted an invalid resize edge")
	}
	if !window.BeginResize(ResizeBottomRight) {
		t.Fatal("resize did not use the new press")
	}
	window.SetMaximized(false)
	pollUntil(func([]Event) bool { return !window.Maximized() && len(requests) == 5 })
	want := []request{{kind: "decoration", edge: 1}, {kind: "opaque-region"}, {kind: "move", serial: 41}, {kind: "minimize"}, {kind: "resize", serial: 42, edge: 10}}
	for i, expected := range want {
		actual := <-requests
		if actual.kind == "move" {
			if actual.edge == 0 {
				t.Fatal("move omitted its seat")
			}
			actual.edge = 0
		}
		if actual != expected {
			t.Fatalf("request %d = %+v, want %+v", i, actual, expected)
		}
	}
}

func TestClosedWindowControlsAreSafe(t *testing.T) {
	for _, window := range []*Window{nil, {}} {
		if window.BeginMove() || window.BeginResize(ResizeRight) || window.Maximized() {
			t.Fatal("closed window reported an active operation")
		}
		window.Minimize()
		window.SetMaximized(true)
		window.SetTitle("closed")
	}
}
