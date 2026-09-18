//go:build linux && cgo

package host

import (
	"math"
	"net"
	"testing"
)

type scaleRequest struct {
	bufferScale int
	destination [2]int
}

func TestFractionalViewportRoundingPointerMappingAndOutputLifecycle(t *testing.T) {
	ready := make(chan struct{})
	peer := &imePeer{actions: make(chan func(*imePeer), 16), requests: make(chan imeRequest, 64), scaling: true, scaleRequests: make(chan scaleRequest, 256), ready: ready}
	fakeCompositor(t, func(conn net.Conn) { peer.conn = conn.(*net.UnixConn); peer.run() })
	w, err := open("fractional host", 640, 480, false, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	imePoll(t, w, func([]Event) bool {
		select {
		case <-ready:
			return true
		default:
			return false
		}
	})
	peer.actions <- func(p *imePeer) {
		writeMessage(p.conn, p.top, 0, []uint32{801, 601, 0})
		writeMessage(p.conn, p.xdg, 0, []uint32{2})
		writeMessage(p.conn, p.surface, 0, []uint32{p.outputA})
		writeMessage(p.conn, p.surface, 0, []uint32{p.outputB})
		writeMessage(p.conn, p.fractional, 0, []uint32{150})
		writeMessage(p.conn, p.pointer, 0, []uint32{3, p.surface, 80 * 256, 120 * 256})
	}
	events := imePoll(t, w, func(events []Event) bool {
		width, height := w.Size()
		return width == 1001 && height == 751 && len(events) > 0 && events[len(events)-1].Kind == Move
	})
	if x, y := w.LogicalSize(); x != 801 || y != 601 || w.Scale() != 1.25 {
		t.Fatalf("logical/fractional state %dx%d @ %v", x, y, w.Scale())
	}
	last := events[len(events)-1]
	if math.Abs(float64(last.X)-80*1001.0/801) > 0.0001 || math.Abs(float64(last.Y)-120*751.0/601) > 0.0001 {
		t.Fatalf("pointer did not follow actual rounded buffer ratio: %+v", last)
	}
	var applied scaleRequest
	imePoll(t, w, func([]Event) bool {
		for {
			select {
			case request := <-peer.scaleRequests:
				applied = request
			default:
				return applied.bufferScale == 1 && applied.destination == [2]int{801, 601}
			}
		}
	})
	state := TextInputState{Enabled: true, ContextID: "fractional-ime", CursorRect: [4]int{0, 0, 1001, 751}}
	if err := w.SetTextInput(state); err != nil {
		t.Fatal(err)
	}
	request := imeRequestUntil(t, w, peer, 1)
	if request.rect != [4]int{0, 0, 801, 601} {
		t.Fatalf("IME cursor area did not return to surface units: %+v", request.rect)
	}
	peer.actions <- func(p *imePeer) { writeMessage(p.conn, p.fractional, 0, []uint32{180}) }
	events = imePoll(t, w, func([]Event) bool { width, height := w.Size(); return width == 1202 && height == 902 })
	if !containsHostKind(events, Cancel) || !containsHostKind(events, Move) {
		t.Fatal("scale change did not cancel old pointer capture and refresh pointer coordinates")
	}
	peer.actions <- func(p *imePeer) {
		writeMessage(p.conn, p.top, 0, []uint32{803, 603, 0})
		writeMessage(p.conn, p.xdg, 0, []uint32{4})
	}
	imePoll(t, w, func([]Event) bool { width, height := w.Size(); return width == 1205 && height == 905 })
	// Losing the optional scaling global destroys its surface extensions and
	// restores the highest entered integer output scale.
	peer.actions <- func(p *imePeer) { writeMessage(p.conn, p.registry, 1, []uint32{6}) }
	imePoll(t, w, func([]Event) bool {
		width, height := w.Size()
		return width == 1606 && height == 1206 && w.Scale() == 2
	})
	imePoll(t, w, func([]Event) bool {
		for {
			select {
			case request := <-peer.scaleRequests:
				applied = request
			default:
				return applied.bufferScale == 2
			}
		}
	})
	peer.actions <- func(p *imePeer) { writeMessage(p.conn, p.registry, 1, []uint32{8}) }
	imePoll(t, w, func([]Event) bool { width, height := w.Size(); return width == 803 && height == 603 && w.Scale() == 1 })
	// Hot-added optional globals can create fresh surface extensions again.
	attached := make(chan struct{})
	peer.actions <- func(p *imePeer) {
		p.fractional = 0
		p.viewport = 0
		p.ready = attached
		writeGlobal(p.conn, p.registry, 9, "wp_fractional_scale_manager_v1", 1)
	}
	imePoll(t, w, func([]Event) bool {
		select {
		case <-attached:
			return true
		default:
			return false
		}
	})
	peer.actions <- func(p *imePeer) { writeMessage(p.conn, p.fractional, 0, []uint32{210}) }
	imePoll(t, w, func([]Event) bool {
		width, height := w.Size()
		return width == 1405 && height == 1055 && w.Scale() == 1.75
	})
	peer.actions <- func(p *imePeer) { writeMessage(p.conn, p.registry, 1, []uint32{5}) }
	imePoll(t, w, func([]Event) bool { width, height := w.Size(); return width == 803 && height == 603 && w.Scale() == 1 })
}

func containsHostKind(events []Event, kind Kind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}
