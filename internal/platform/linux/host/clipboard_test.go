//go:build linux && cgo

package host

import (
	"encoding/binary"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const clipboardTestMIME = "text/plain;charset=utf-8"

// This peer is a synthetic compositor; no test connects to the user's display
// or reads clipboard contents other than the strings created in this test.
func TestClipboardLazyBidirectionalRelayAndFocus(t *testing.T) {
	actions := make(chan func(*clipboardPeer), 8)
	published := make(chan uint32, 8)
	received := make(chan struct{}, 8)
	fakeCompositor(t, func(conn net.Conn) {
		peer := clipboardPeer{conn: conn.(*net.UnixConn), actions: actions, published: published, received: received, nextOffer: 0xff000000}
		peer.run()
	})
	w, err := open("synthetic clipboard", 640, 480, false, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pollClipboardUntil(t, w, func() bool { return w.ClipboardOffer().ID != 0 })
	initial := w.ClipboardOffer()
	if !initial.Available || initial.ExternalID != 0 || !reflect.DeepEqual(initial.MIMEs, []string{clipboardTestMIME}) {
		t.Fatalf("wrong initial metadata: %+v", initial)
	}
	select {
	case <-received:
		t.Fatal("observing metadata eagerly read clipboard data")
	default:
	}
	readFD, writeFD, closeWrite := clipboardPipe(t)
	if err := w.ReceiveClipboard(initial.ID, clipboardTestMIME, writeFD); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(uintptr(writeFD), unix.F_GETFD, 0); err != nil {
		t.Fatal("ReceiveClipboard closed its borrowed descriptor")
	}
	closeWrite()
	if got := readClipboardPipe(t, w, readFD); got != "synthetic desktop text" {
		t.Fatalf("incoming pipe data = %q", got)
	}
	<-received
	actions <- func(p *clipboardPeer) { p.offer() }
	pollClipboardUntil(t, w, func() bool { return w.ClipboardOffer().ID != initial.ID })
	_, staleFD, closeStale := clipboardPipe(t)
	if err := w.ReceiveClipboard(initial.ID, clipboardTestMIME, staleFD); !errors.Is(err, ErrClipboardStale) {
		t.Fatalf("old clipboard offer accepted: %v", err)
	}
	closeStale()
	actions <- func(p *clipboardPeer) { writeMessage(p.conn, p.keyboard, 2, []uint32{101, p.surface}) }
	pollClipboardUntil(t, w, func() bool { return !w.ClipboardOffer().Available })
	if got := w.ClipboardOffer(); got.ID != 0 {
		t.Fatalf("incoming clipboard survived keyboard focus loss: %+v", got)
	}
	if err := w.OfferClipboard(77, []string{clipboardTestMIME}); !errors.Is(err, ErrClipboardUnavailable) {
		t.Fatalf("published clipboard without a focused serial: %v", err)
	}
	actions <- func(p *clipboardPeer) { writeMessage(p.conn, p.keyboard, 1, []uint32{102, p.surface, 0}); p.offer() }
	// Availability follows keyboard enter, which may arrive before the
	// compositor's following data-offer messages. Wait for the selected offer
	// itself before replacing it, otherwise a partially delivered offer can be
	// destroyed while its remaining events are still in flight.
	pollClipboardUntil(t, w, func() bool {
		offer := w.ClipboardOffer()
		return offer.Available && offer.ID != 0
	})
	if err := w.OfferClipboard(77, []string{clipboardTestMIME}); err != nil {
		t.Fatal(err)
	}
	if got := w.ClipboardOffer(); got.ExternalID != 77 || !reflect.DeepEqual(got.MIMEs, []string{clipboardTestMIME}) {
		t.Fatalf("published source lacks echo marker: %+v", got)
	}
	var serial uint32
	pollClipboardUntil(t, w, func() bool {
		select {
		case serial = <-published:
			return true
		default:
			return false
		}
	})
	if serial != 102 {
		t.Fatalf("selection used stale input serial %d", serial)
	}
	outerRead, outerWrite, closeOuterWrite := clipboardPipe(t)
	actions <- func(p *clipboardPeer) {
		writeMessage(p.conn, p.keyboard, 2, []uint32{103, p.surface})
		p.request(outerWrite)
		closeOuterWrite()
	}
	var requests []ClipboardRequest
	pollClipboardUntil(t, w, func() bool { requests = append(requests, w.PollClipboardRequests()...); return len(requests) > 0 })
	if w.ClipboardOffer().Available || w.ClipboardOffer().ExternalID != 77 {
		t.Fatal("outgoing source did not remain available after switching windows")
	}
	if len(requests) != 1 || requests[0].ExternalID != 77 || requests[0].MIME != clipboardTestMIME {
		t.Fatalf("wrong lazy send request: %+v", requests)
	}
	if _, err := unix.Write(requests[0].FD, []byte("synthetic foot text")); err != nil {
		t.Fatal(err)
	}
	unix.Close(requests[0].FD)
	if got := readClipboardPipe(t, w, outerRead); got != "synthetic foot text" {
		t.Fatalf("outgoing pipe data = %q", got)
	}
	// A queued request canceled before the bridge drains it must close its fd.
	cancelRead, cancelWrite, closeCancelWrite := clipboardPipe(t)
	actions <- func(p *clipboardPeer) {
		p.request(cancelWrite)
		closeCancelWrite()
		writeMessage(p.conn, p.source, 2, nil)
	}
	pollClipboardUntil(t, w, func() bool { return w.ClipboardOffer().ExternalID == 0 })
	if got := w.PollClipboardRequests(); len(got) != 0 {
		for _, request := range got {
			unix.Close(request.FD)
		}
		t.Fatal("canceled source retained a pending transfer")
	}
	if got := readClipboardPipe(t, w, cancelRead); got != "" {
		t.Fatal("canceled transfer unexpectedly wrote bytes")
	}
}

func TestClipboardClosedWindowAndValidation(t *testing.T) {
	for _, w := range []*Window{nil, {}} {
		if w.ClipboardOffer().ID != 0 || len(w.PollClipboardRequests()) != 0 {
			t.Fatal("closed window retained clipboard state")
		}
		if !errors.Is(w.OfferClipboard(1, []string{clipboardTestMIME}), ErrClosed) || !errors.Is(w.ReceiveClipboard(1, clipboardTestMIME, 0), ErrClosed) {
			t.Fatal("closed clipboard call did not report ErrClosed")
		}
	}
}

func TestClipboardBoundsSendQueueAndClosesUndrainedDescriptors(t *testing.T) {
	actions := make(chan func(*clipboardPeer), 8)
	published := make(chan uint32, 8)
	fakeCompositor(t, func(conn net.Conn) {
		peer := clipboardPeer{conn: conn.(*net.UnixConn), actions: actions, published: published, received: make(chan struct{}, 8), nextOffer: 0xff000000}
		peer.run()
	})
	w, err := open("clipboard queue limits", 640, 480, false, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pollClipboardUntil(t, w, func() bool {
		offer := w.ClipboardOffer()
		return offer.Available && offer.ID != 0
	})
	if err := w.OfferClipboard(88, []string{clipboardTestMIME}); err != nil {
		t.Fatal(err)
	}
	pollClipboardUntil(t, w, func() bool {
		select {
		case <-published:
			return true
		default:
			return false
		}
	})
	readFD, writeFD, closeWrite := clipboardPipe(t)
	sent := make(chan struct{})
	actions <- func(p *clipboardPeer) {
		for i := 0; i < 35; i++ {
			p.request(writeFD)
		}
		closeWrite()
		// This marker is ordered after every send request on the Wayland
		// connection, so observing it proves the client dispatched the burst.
		writeMessage(p.conn, p.keyboard, 3, []uint32{201, 9876, 30, 1})
		close(sent)
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("synthetic peer could not enqueue bounded requests")
	}
	pollClipboardMarker(t, w, 9876)
	requests := w.PollClipboardRequests()
	for _, request := range requests {
		unix.Close(request.FD)
	}
	if len(requests) != 32 {
		t.Fatalf("clipboard request queue was not bounded: %d", len(requests))
	}
	if got := readClipboardPipe(t, w, readFD); got != "" {
		t.Fatal("overflow cleanup wrote unexpected data")
	}
	closeRead, closeFD, closeWriter := clipboardPipe(t)
	sent = make(chan struct{})
	actions <- func(p *clipboardPeer) {
		p.request(closeFD)
		closeWriter()
		writeMessage(p.conn, p.keyboard, 3, []uint32{202, 9877, 30, 1})
		close(sent)
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("synthetic peer could not request final transfer")
	}
	pollClipboardMarker(t, w, 9877)
	w.Close()
	var data [1]byte
	if n, err := unix.Read(closeRead, data[:]); n != 0 || err != nil {
		t.Fatalf("closing window left an undrained clipboard descriptor: n=%d err=%v", n, err)
	}
}

func pollClipboardMarker(t *testing.T, w *Window, marker uint32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, err := w.Poll(nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Kind == Key && event.Time == marker {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("client did not dispatch clipboard marker")
}

func clipboardPipe(t *testing.T) (int, int, func()) {
	t.Helper()
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	// The writer closes after marshalling. Its idempotent cleanup never closes
	// a descriptor recycled by a later transfer, even after an early failure.
	var once sync.Once
	closeWrite := func() { once.Do(func() { unix.Close(fds[1]) }) }
	t.Cleanup(func() { unix.Close(fds[0]) })
	t.Cleanup(closeWrite)
	return fds[0], fds[1], closeWrite
}

func pollClipboardUntil(t *testing.T, w *Window, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := w.Poll(nil); err != nil {
			t.Fatal(err)
		}
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("synthetic clipboard peer did not complete in time")
}

func readClipboardPipe(t *testing.T, w *Window, fd int) string {
	t.Helper()
	var result []byte
	pollClipboardUntil(t, w, func() bool {
		var buffer [256]byte
		n, err := unix.Read(fd, buffer[:])
		if n > 0 {
			result = append(result, buffer[:n]...)
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			t.Fatal(err)
		}
		return n == 0 && err == nil
	})
	return string(result)
}

type clipboardWireRequest struct {
	object, opcode uint32
	payload        []byte
	fds            []int
}
type clipboardPeer struct {
	conn                                                    *net.UnixConn
	actions                                                 chan func(*clipboardPeer)
	published                                               chan uint32
	received                                                chan struct{}
	registry, compositor, wm, seat, manager, device, source uint32
	surface, xdg, top, keyboard, nextOffer                  uint32
	configured, sent                                        bool
	fdQueue                                                 []int
}

func (p *clipboardPeer) offer() {
	id := p.nextOffer
	p.nextOffer++
	writeMessage(p.conn, p.device, 0, []uint32{id})
	p.conn.Write(clipboardStringMessage(id, 0, clipboardTestMIME))
	writeMessage(p.conn, p.device, 5, []uint32{id})
}
func (p *clipboardPeer) request(fd int) {
	p.conn.WriteMsgUnix(clipboardStringMessage(p.source, 1, clipboardTestMIME), unix.UnixRights(fd), nil)
}
func clipboardStringMessage(object, opcode uint32, value string) []byte {
	text := append([]byte(value), 0)
	packet := make([]byte, 12+(len(text)+3)&^3)
	binary.NativeEndian.PutUint32(packet, object)
	binary.NativeEndian.PutUint32(packet[4:], uint32(len(packet))<<16|opcode)
	binary.NativeEndian.PutUint32(packet[8:], uint32(len(text)))
	copy(packet[12:], text)
	return packet
}

func (p *clipboardPeer) run() {
	messages := make(chan clipboardWireRequest)
	done := make(chan struct{})
	defer close(done)
	defer func() {
		for _, fd := range p.fdQueue {
			unix.Close(fd)
		}
	}()
	go readClipboardWire(p.conn, messages, done)
	for {
		select {
		case action := <-p.actions:
			action(p)
		case message, ok := <-messages:
			if !ok {
				return
			}
			p.fdQueue = append(p.fdQueue, message.fds...)
			arg := func(n int) uint32 { return binary.NativeEndian.Uint32(message.payload[n*4:]) }
			object, opcode := message.object, message.opcode
			switch {
			case object == 1 && opcode == 1:
				p.registry = arg(0)
			case object == 1 && opcode == 0:
				writeGlobal(p.conn, p.registry, 1, "wl_compositor", 4)
				writeGlobal(p.conn, p.registry, 2, "xdg_wm_base", 1)
				writeGlobal(p.conn, p.registry, 3, "wl_seat", 5)
				writeGlobal(p.conn, p.registry, 4, "wl_data_device_manager", 3)
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
					writeMessage(p.conn, id, 0, []uint32{2})
				case 4:
					p.manager = id
				}
			case object == p.compositor && opcode == 0:
				p.surface = arg(0)
			case object == p.wm && opcode == 2:
				p.xdg = arg(0)
			case object == p.xdg && opcode == 1:
				p.top = arg(0)
			case object == p.seat && opcode == 1:
				p.keyboard = arg(0)
			case object == p.manager && opcode == 1:
				p.device = arg(0)
			case object == p.manager && opcode == 0:
				p.source = arg(0)
			case object == p.device && opcode == 1:
				p.published <- arg(1)
			case object >= 0xff000000 && opcode == 1:
				if len(p.fdQueue) == 0 {
					return
				}
				fd := p.fdQueue[0]
				p.fdQueue = p.fdQueue[1:]
				unix.Write(fd, []byte("synthetic desktop text"))
				unix.Close(fd)
				p.received <- struct{}{}
			case object == p.surface && opcode == 6 && !p.configured:
				writeMessage(p.conn, p.top, 0, []uint32{640, 480, 0})
				writeMessage(p.conn, p.xdg, 0, []uint32{1})
				p.configured = true
			}
			if p.configured && p.keyboard != 0 && p.device != 0 && !p.sent {
				writeMessage(p.conn, p.keyboard, 1, []uint32{100, p.surface, 0})
				p.offer()
				p.sent = true
			}
		}
	}
}

func readClipboardWire(conn *net.UnixConn, out chan<- clipboardWireRequest, done <-chan struct{}) {
	defer close(out)
	var buffer []byte
	var fds []int
	defer func() {
		for _, fd := range fds {
			unix.Close(fd)
		}
	}()
	for {
		if len(buffer) >= 8 {
			word := binary.NativeEndian.Uint32(buffer[4:])
			size := int(word >> 16)
			if size < 8 {
				return
			}
			if len(buffer) >= size {
				message := clipboardWireRequest{object: binary.NativeEndian.Uint32(buffer), opcode: word & 0xffff, payload: append([]byte(nil), buffer[8:size]...), fds: fds}
				select {
				case out <- message:
					fds = nil
				case <-done:
					return
				}
				buffer = buffer[size:]
				continue
			}
		}
		data, control := make([]byte, 4096), make([]byte, unix.CmsgSpace(32*4))
		n, oobn, _, _, err := conn.ReadMsgUnix(data, control)
		if err != nil || n == 0 {
			return
		}
		buffer = append(buffer, data[:n]...)
		messages, err := unix.ParseSocketControlMessage(control[:oobn])
		if err != nil {
			return
		}
		for _, message := range messages {
			rights, err := unix.ParseUnixRights(&message)
			if err != nil {
				return
			}
			fds = append(fds, rights...)
		}
	}
}
