//go:build linux && cgo

package host

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

type imeRequest struct {
	serial         uint32
	enabled        bool
	text           string
	cursor, anchor int
	cause          uint32
	rect           [4]int
}
type imePeer struct {
	conn                                                                           *net.UnixConn
	actions                                                                        chan func(*imePeer)
	requests                                                                       chan imeRequest
	registry, compositor, wm, seat, manager, input, surface, xdg, top, keyboard    uint32
	configured, entered                                                            bool
	state                                                                          imeRequest
	scaling                                                                        bool
	viewporter, fractionalManager, viewport, fractional, outputA, outputB, pointer uint32
	scaleRequests                                                                  chan scaleRequest
	scaleState                                                                     scaleRequest
	ready                                                                          chan struct{}
}

func (p *imePeer) run() {
	messages := make(chan clipboardWireRequest)
	done := make(chan struct{})
	defer close(done)
	go readClipboardWire(p.conn, messages, done)
	for {
		select {
		case action := <-p.actions:
			action(p)
		case message, ok := <-messages:
			if !ok {
				return
			}
			object, opcode := message.object, message.opcode
			arg := func(n int) uint32 { return binary.NativeEndian.Uint32(message.payload[n*4:]) }
			switch {
			case object == 1 && opcode == 1:
				p.registry = arg(0)
			case object == 1 && opcode == 0:
				writeGlobal(p.conn, p.registry, 1, "wl_compositor", 4)
				writeGlobal(p.conn, p.registry, 2, "xdg_wm_base", 1)
				writeGlobal(p.conn, p.registry, 3, "wl_seat", 5)
				writeGlobal(p.conn, p.registry, 4, "zwp_text_input_manager_v3", 1)
				if p.scaling {
					writeGlobal(p.conn, p.registry, 5, "wp_viewporter", 1)
					writeGlobal(p.conn, p.registry, 6, "wp_fractional_scale_manager_v1", 1)
					writeGlobal(p.conn, p.registry, 7, "wl_output", 2)
					writeGlobal(p.conn, p.registry, 8, "wl_output", 2)
				}
				writeMessage(p.conn, arg(0), 0, []uint32{1})
			case object == p.registry && opcode == 0:
				id := binary.NativeEndian.Uint32(message.payload[len(message.payload)-4:])
				switch arg(0) {
				case 1:
					p.compositor = id
				case 2:
					p.wm = id
				case 3:
					p.seat = id
					caps := uint32(2)
					if p.scaling {
						caps = 3
					}
					writeMessage(p.conn, id, 0, []uint32{caps})
				case 4:
					p.manager = id
				case 5:
					p.viewporter = id
				case 6, 9:
					p.fractionalManager = id
				case 7:
					p.outputA = id
					writeMessage(p.conn, id, 3, []uint32{1})
					writeMessage(p.conn, id, 2, nil)
				case 8:
					p.outputB = id
					writeMessage(p.conn, id, 3, []uint32{2})
					writeMessage(p.conn, id, 2, nil)
				}
			case object == p.compositor && opcode == 0:
				p.surface = arg(0)
			case object == p.wm && opcode == 2:
				p.xdg = arg(0)
			case object == p.xdg && opcode == 1:
				p.top = arg(0)
			case object == p.seat && opcode == 1:
				p.keyboard = arg(0)
			case object == p.seat && opcode == 0:
				p.pointer = arg(0)
			case p.scaling && object == p.viewporter && opcode == 1:
				p.viewport = arg(0)
			case p.scaling && object == p.fractionalManager && opcode == 1:
				p.fractional = arg(0)
			case p.scaling && object == p.viewport && opcode == 2:
				p.scaleState.destination = [2]int{int(arg(0)), int(arg(1))}
				p.scaleRequests <- p.scaleState
			case p.scaling && object == p.surface && opcode == 8:
				p.scaleState.bufferScale = int(arg(0))
				p.scaleRequests <- p.scaleState
			case object == p.manager && opcode == 1:
				p.input = arg(0)
			case object == p.surface && opcode == 6 && !p.configured:
				writeMessage(p.conn, p.top, 0, []uint32{640, 480, 0})
				writeMessage(p.conn, p.xdg, 0, []uint32{1})
				p.configured = true
			case object == p.input && opcode == 1:
				p.state.enabled = true
			case object == p.input && opcode == 2:
				p.state.enabled = false
			case object == p.input && opcode == 3:
				length := int(arg(0))
				p.state.text = string(message.payload[4 : 4+length-1])
				offset := 4 + (length+3)&^3
				p.state.cursor = int(int32(binary.NativeEndian.Uint32(message.payload[offset:])))
				p.state.anchor = int(int32(binary.NativeEndian.Uint32(message.payload[offset+4:])))
			case object == p.input && opcode == 4:
				p.state.cause = arg(0)
			case object == p.input && opcode == 6:
				for i := range p.state.rect {
					p.state.rect[i] = int(int32(arg(i)))
				}
			case object == p.input && opcode == 7:
				p.state.serial++
				p.requests <- p.state
			}
			if p.configured && p.input != 0 && !p.entered {
				writeMessage(p.conn, p.input, 0, []uint32{p.surface})
				p.entered = true
			}
			if p.ready != nil && p.configured && p.pointer != 0 && p.viewport != 0 && p.fractional != 0 {
				close(p.ready)
				p.ready = nil
			}
		}
	}
}
func (p *imePeer) commit(text string) { p.conn.Write(clipboardStringMessage(p.input, 3, text)) }
func (p *imePeer) preedit(text string, begin, end int32) {
	packet := clipboardStringMessage(p.input, 2, text)
	packet = append(packet, make([]byte, 8)...)
	binary.NativeEndian.PutUint32(packet[4:], uint32(len(packet))<<16|2)
	binary.NativeEndian.PutUint32(packet[len(packet)-8:], uint32(begin))
	binary.NativeEndian.PutUint32(packet[len(packet)-4:], uint32(end))
	p.conn.Write(packet)
}
func (p *imePeer) done(serial uint32) { writeMessage(p.conn, p.input, 5, []uint32{serial}) }
func imeWindow(t *testing.T) (*Window, *imePeer) {
	t.Helper()
	peer := &imePeer{actions: make(chan func(*imePeer), 16), requests: make(chan imeRequest, 64)}
	fakeCompositor(t, func(conn net.Conn) { peer.conn = conn.(*net.UnixConn); peer.run() })
	w, err := open("IME", 640, 480, false, 1000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, peer
}
func imePoll(t *testing.T, w *Window, ready func([]Event) bool) []Event {
	t.Helper()
	var all []Event
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, err := w.Poll(nil)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, events...)
		if ready(all) {
			return all
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("IME peer timed out: %+v", all)
	return nil
}
func imeRequestUntil(t *testing.T, w *Window, p *imePeer, serial uint32) imeRequest {
	t.Helper()
	var found imeRequest
	imePoll(t, w, func([]Event) bool {
		for {
			select {
			case state := <-p.requests:
				found = state
				if state.serial >= serial {
					return true
				}
			default:
				return false
			}
		}
	})
	return found
}
func imeHas(events []Event, kind Kind, text string) bool {
	for _, event := range events {
		if event.Kind == kind && event.Text == text {
			return true
		}
	}
	return false
}

func TestTextInputTransactionsFocusSerialsAndOwnership(t *testing.T) {
	w, peer := imeWindow(t)
	state := TextInputState{Enabled: true, ContextID: "field/a", Surrounding: "hello", Cursor: 5, Anchor: 5, CursorRect: [4]int{25, 40, 1, 22}}
	if err := w.SetTextInput(state); err != nil {
		t.Fatal(err)
	}
	initial := imeRequestUntil(t, w, peer, 1)
	if !w.TextInputAvailable() || !initial.enabled || initial.text != "hello" || initial.cursor != 5 || initial.rect != state.CursorRect {
		t.Fatalf("missing enabled field state: %+v", initial)
	}
	ready := make(chan struct{})
	peer.actions <- func(p *imePeer) { p.preedit("한", 3, 3); close(ready) }
	<-ready
	for i := 0; i < 3; i++ {
		events, err := w.Poll(nil)
		if err != nil {
			t.Fatal(err)
		}
		if imeHas(events, TextPreedit, "한") {
			t.Fatal("preedit escaped before done")
		}
	}
	peer.actions <- func(p *imePeer) {
		writeMessage(p.conn, p.input, 4, []uint32{2, 0})
		p.commit("韓")
		p.preedit("字", 3, 3)
		p.done(1)
	}
	events := imePoll(t, w, func(events []Event) bool { return imeHas(events, TextPreedit, "字") })
	var textEvents []Event
	for _, e := range events {
		if e.Kind == TextCommit || e.Kind == TextPreedit {
			textEvents = append(textEvents, e)
		}
	}
	if len(textEvents) != 2 || textEvents[0].Kind != TextCommit || textEvents[0].Text != "韓" || textEvents[0].DeleteBefore != 2 || textEvents[1].Text != "字" || textEvents[1].PreeditBegin != 3 || textEvents[0].TextContext != "field/a" {
		t.Fatalf("transaction order/data: %+v", textEvents)
	}
	state.Surrounding = "hel韓"
	state.Cursor = 6
	state.Anchor = 6
	if err := w.SetTextInput(state); err != nil {
		t.Fatal(err)
	}
	updated := imeRequestUntil(t, w, peer, 2)
	if updated.cause != 0 {
		t.Fatal("IME-caused surrounding edit was mislabeled as external")
	}
	state.CursorRect[0]++
	w.SetTextInput(state)
	imeRequestUntil(t, w, peer, 3)
	peer.actions <- func(p *imePeer) { p.commit("same"); p.done(2) }
	imePoll(t, w, func(events []Event) bool { return imeHas(events, TextCommit, "same") })
	state.Surrounding += "same"
	state.Cursor += 4
	state.Anchor += 4
	w.SetTextInput(state)
	for i := 0; i < 3; i++ {
		if _, err := w.Poll(nil); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case request := <-peer.requests:
		t.Fatalf("state published before matching serial: %+v", request)
	default:
	}
	peer.actions <- func(p *imePeer) { p.done(3) }
	imePoll(t, w, func(events []Event) bool { return imeHas(events, TextPreedit, "") })
	w.SetTextInput(state)
	imeRequestUntil(t, w, peer, 4)
	state.ContextID = "field/b"
	state.Surrounding = ""
	state.Cursor = 0
	state.Anchor = 0
	w.SetTextInput(state)
	imeRequestUntil(t, w, peer, 6)
	peer.actions <- func(p *imePeer) { p.commit("STALE"); p.done(4); p.commit("fresh"); p.done(6) }
	events = imePoll(t, w, func(events []Event) bool { return imeHas(events, TextCommit, "fresh") })
	if imeHas(events, TextCommit, "STALE") {
		t.Fatal("previous field transaction reached new field")
	}
	for _, e := range events {
		if e.Kind == TextCommit && e.TextContext != "field/b" {
			t.Fatal("IME context identity was lost")
		}
	}
	w.SetTextInput(TextInputState{})
	imeRequestUntil(t, w, peer, 7)
	peer.actions <- func(p *imePeer) {
		p.commit("disabled")
		p.done(7)
		writeMessage(p.conn, p.input, 1, []uint32{p.surface})
	}
	for i := 0; i < 3; i++ {
		events, err := w.Poll(nil)
		if err != nil {
			t.Fatal(err)
		}
		if imeHas(events, TextCommit, "disabled") {
			t.Fatal("disabled field accepted IME text")
		}
	}
	w.Close()
	if textEvents[0].Text != "韓" || textEvents[1].Text != "字" {
		t.Fatal("Go IME text did not survive C cleanup")
	}
}

func TestTextInputValidationAndMalformedComposition(t *testing.T) {
	w, peer := imeWindow(t)
	for _, state := range []TextInputState{{Enabled: true}, {Enabled: true, ContextID: "x", Surrounding: "界", Cursor: 1}, {Enabled: true, ContextID: "x", CursorRect: [4]int{0, 0, -1, 2}}} {
		if err := w.SetTextInput(state); err == nil {
			t.Fatalf("accepted invalid state %+v", state)
		}
	}
	state := TextInputState{Enabled: true, ContextID: "valid"}
	w.SetTextInput(state)
	imeRequestUntil(t, w, peer, 1)
	peer.actions <- func(p *imePeer) {
		p.commit("\xff")
		p.preedit("界", 1, 2)
		p.done(1)
		p.preedit("ok", 2, 2)
		p.done(1)
	}
	events := imePoll(t, w, func(events []Event) bool { return imeHas(events, TextPreedit, "ok") })
	for _, e := range events {
		if e.Kind == TextCommit || e.Kind == TextPreedit && e.Text == "界" {
			t.Fatalf("invalid UTF-8 or middle-byte cursor escaped: %+v", e)
		}
	}
}
