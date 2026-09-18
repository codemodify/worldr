//go:build linux

package input

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// Synthetic pipes exercise the actual nonblocking reader without input devices.
func testPointer(t *testing.T) (*Pointer, int) {
	t.Helper()
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	p := &Pointer{X: 50, Y: 40, W: 100, H: 80, devices: []*device{{fd: fds[0]}}}
	t.Cleanup(func() { p.Close(); _ = unix.Close(fds[1]) })
	return p, fds[1]
}

func encodePackets(events ...packet) []byte {
	data := make([]byte, len(events)*eventBytes)
	for i, event := range events {
		b := data[i*eventBytes:]
		word := func(at int, value uint64) {
			if eventWordBytes == 8 {
				binary.NativeEndian.PutUint64(b[at:], value)
			} else {
				binary.NativeEndian.PutUint32(b[at:], uint32(value))
			}
		}
		word(0, event.time/1_000_000)
		word(eventWordBytes, event.time%1_000_000)
		binary.NativeEndian.PutUint16(b[2*eventWordBytes:], event.typ)
		binary.NativeEndian.PutUint16(b[2*eventWordBytes+2:], event.code)
		binary.NativeEndian.PutUint32(b[2*eventWordBytes+4:], uint32(event.value))
	}
	return data
}

func sendBytes(t *testing.T, fd int, data []byte) {
	t.Helper()
	n, err := unix.Write(fd, data)
	if err != nil || n != len(data) {
		t.Fatalf("write synthetic input: %d/%d, %v", n, len(data), err)
	}
}
func sendPackets(t *testing.T, fd int, events ...packet) {
	t.Helper()
	sendBytes(t, fd, encodePackets(events...))
}
func report(at uint64) packet                             { return packet{time: at, typ: evSyn, code: synReport} }
func key(at uint64, code uint16, value int32) packet      { return packet{at, evKey, code, value} }
func relative(at uint64, code uint16, value int32) packet { return packet{at, evRel, code, value} }

func TestOrderedEdgesKeepOriginalCoordinatesAndTime(t *testing.T) {
	p, fd := testPointer(t)
	sendPackets(t, fd,
		key(1000, 42, 1), relative(2000, relX, 4), key(3000, 0x110, 1),
		relative(4000, relY, 7), key(5000, 0x110, 0),
		key(6000, 0x111, 1), key(7000, 0x111, 0),
		key(8000, 0x112, 1), key(9000, 0x112, 0),
		key(10000, 0x113, 1), key(11000, 0x113, 0), key(12000, 42, 0), report(12000))
	p.Poll()
	kinds := []EventKind{KeyInput, Move, Down, Move, Up, Down, Up, Down, Up, Down, Up, KeyInput}
	if len(p.Events) != len(kinds) {
		t.Fatalf("events: %+v", p.Events)
	}
	for i, event := range p.Events {
		if event.Kind != kinds[i] || event.Time != uint32(i+1) {
			t.Fatalf("event %d: %+v", i, event)
		}
	}
	if p.Events[2].X != 54 || p.Events[2].Y != 40 || p.Events[4].X != 54 || p.Events[4].Y != 47 {
		t.Fatalf("selection endpoints were collapsed: %+v", p.Events)
	}
	for i, code := range []uint32{0x110, 0x111, 0x112, 0x113} {
		index := 2 + 2*i
		if i > 0 {
			index++
		}
		if p.Events[index].Code != code {
			t.Fatalf("button code lost: %+v", p.Events[index])
		}
	}
	p.Poll()
	if len(p.Events) != 0 || p.X != 54 || p.Y != 47 {
		t.Fatalf("idle poll replayed events or moved cursor: %+v", p)
	}
}

func TestCompletedDevicesMergeByTimestamp(t *testing.T) {
	p, mouse := testPointer(t)
	keyboard, keyfd := testPointer(t)
	p.devices = append(p.devices, keyboard.devices...)
	keyboard.devices = nil // Ownership moves to p for cleanup.
	sendPackets(t, mouse, key(2000, 0x110, 1), key(4000, 0x110, 0), report(4000))
	sendPackets(t, keyfd, key(1000, 29, 1), key(3000, 29, 0), report(3000))
	p.Poll()
	var got []uint32
	for _, event := range p.Events {
		got = append(got, event.Time)
	}
	if !reflect.DeepEqual(got, []uint32{1, 2, 3, 4}) {
		t.Fatalf("device read order replaced input order: %v", got)
	}
}

func TestWheelResolutionDirectionAndNoDuplicateDetents(t *testing.T) {
	p, fd := testPointer(t)
	sendPackets(t, fd, relative(1000, relWheel, 1), relative(1000, relHWheel, -2), report(1000),
		relative(2000, relWheel, 1), relative(2000, relWheelHi, 120),
		relative(2000, relHWheel, -1), relative(2000, relHWheelHi, -120), report(2000),
		relative(3000, relWheelHi, 30), relative(3000, relHWheelHi, 15), report(3000))
	p.Poll()
	if len(p.Events) != 6 {
		t.Fatalf("duplicated high-resolution wheel: %+v", p.Events)
	}
	want := [6][2]float32{{0, -10}, {-20, 0}, {0, -10}, {-10, 0}, {0, -2.5}, {1.25, 0}}
	for i, event := range p.Events {
		if event.Kind != Scroll || event.X != 50 || event.Y != 40 || [2]float32{event.ScrollX, event.ScrollY} != want[i] {
			t.Fatalf("wheel %d: %+v", i, event)
		}
	}
}

func TestRawExtendedKeysAndRepeatOwnership(t *testing.T) {
	p, fd := testPointer(t)
	sendPackets(t, fd, key(1000, 30, 1), key(2000, 30, 2), key(3000, 30, 0),
		key(4000, 0x160, 1), key(5000, 0x1d0, 1), key(6000, 767, 1), key(7000, 768, 1), key(8000, 0, 1),
		key(9000, 0x220, 1), key(10000, 0x2c0, 1), report(10000))
	p.Poll()
	want := []uint32{30, 30, 0x160, 0x1d0, 767, 0x220, 0x2c0}
	if len(p.Events) != len(want) {
		t.Fatalf("raw key/repeat edges: %+v", p.Events)
	}
	for i, event := range p.Events {
		kind := KeyInput
		if i >= 5 {
			kind = Down
		}
		if event.Kind != kind || event.Code != want[i] {
			t.Fatalf("key %d: %+v", i, event)
		}
	}
}

func TestPartialPacketAndReportRemainPending(t *testing.T) {
	p, fd := testPointer(t)
	data := encodePackets(key(12003000, 18, 1))
	sendBytes(t, fd, data[:5])
	p.Poll()
	if len(p.Events) != 0 {
		t.Fatal("partial packet became an event")
	}
	sendBytes(t, fd, data[5:])
	p.Poll()
	if len(p.Events) != 0 {
		t.Fatal("incomplete report became an event")
	}
	sendPackets(t, fd, report(12003000))
	p.Poll()
	if len(p.Events) != 1 || p.Events[0].Time != 12003 || p.Events[0].Code != 18 {
		t.Fatalf("split packet was lost: %+v", p.Events)
	}
}

func TestDroppedReportCancelsAndDiscardsStaleEdges(t *testing.T) {
	p, fd := testPointer(t)
	sendPackets(t, fd, key(1000, 29, 1), key(1000, 0x110, 1), report(1000))
	p.Poll()
	if len(p.Events) != 2 {
		t.Fatal("test did not establish held state")
	}
	sendPackets(t, fd, relative(2000, relX, 10), packet{3000, evSyn, synDropped, 0},
		key(4000, 29, 0), key(4000, 0x110, 0), relative(4000, relX, 50), report(4000),
		key(5000, 30, 1), report(5000))
	p.Poll()
	if len(p.Events) != 2 || p.Events[0].Kind != Cancel || p.Events[0].Time != 3 || p.Events[1].Code != 30 || p.X != 50 {
		t.Fatalf("lost input revived a stale report: %+v cursor=%d", p.Events, p.X)
	}
}

func TestEOFCancelsAndClosesDescriptor(t *testing.T) {
	p, fd := testPointer(t)
	readfd := p.devices[0].fd
	sendPackets(t, fd, key(1000, 30, 1), report(1000))
	p.Poll()
	// A truncated packet on disconnect must never be mistaken for a release.
	sendBytes(t, fd, encodePackets(key(2000, 30, 0))[:7])
	if err := unix.Close(fd); err != nil {
		t.Fatal(err)
	}
	p.Poll()
	if len(p.Events) != 1 || p.Events[0].Kind != Cancel || len(p.devices) != 0 {
		t.Fatalf("EOF did not cancel/retire device: %+v", p)
	}
	if _, err := unix.FcntlInt(uintptr(readfd), unix.F_GETFD, 0); err != unix.EBADF {
		t.Fatalf("retired descriptor remains open: %v", err)
	}
	p.Poll()
	if len(p.Events) != 0 {
		t.Fatal("removed device canceled repeatedly")
	}
	p.Close()
	p.Close()
}

func TestLostDeviceCancelsAfterOtherDeviceEdges(t *testing.T) {
	p, mouse := testPointer(t)
	keyboard, keyfd := testPointer(t)
	p.devices = append(p.devices, keyboard.devices...)
	keyboard.devices = nil
	if err := unix.Close(mouse); err != nil {
		t.Fatal(err)
	}
	sendPackets(t, keyfd, key(1000, 29, 1), report(1000))
	p.Poll()
	if len(p.Events) != 2 || p.Events[0].Kind != KeyInput || p.Events[1].Kind != Cancel || p.Events[1].Time != 1 {
		t.Fatalf("later device edge revived held state after loss: %+v", p.Events)
	}
}

func TestPollBudgetAndMalformedReportBoundMemory(t *testing.T) {
	p, fd := testPointer(t)
	packets := make([]packet, 0, maxPollEvents+2)
	for i := 0; i < maxPollEvents+1; i++ {
		packets = append(packets, key(uint64(i+1)*1000, 30, 1))
	}
	packets = append(packets, report(300000))
	sendPackets(t, fd, packets...)
	p.Poll()
	if len(p.Events) != 0 || len(p.devices[0].frame) != maxReportEvents {
		t.Fatal("poll exceeded its bounded packet budget")
	}
	p.Poll()
	if len(p.Events) != 1 || p.Events[0].Kind != Cancel || len(p.devices[0].frame) != 0 || p.devices[0].dropping {
		t.Fatalf("oversized report was not canceled and recovered: %+v", p)
	}
	sendPackets(t, fd, key(400000, 18, 1), report(400000))
	p.Poll()
	if len(p.Events) != 1 || p.Events[0].Code != 18 {
		t.Fatal("reader failed to resume after next report")
	}
}

func TestCursorBoundsAndTimestampWrap(t *testing.T) {
	p, fd := testPointer(t)
	at := (uint64(math.MaxUint32) + 5) * 1000
	sendPackets(t, fd, relative(at, relX, math.MaxInt32), relative(at, relY, math.MinInt32), key(at, 0x110, 1), report(at))
	p.Poll()
	if len(p.Events) != 3 || p.Events[2].X != 99 || p.Events[2].Y != 0 || p.Events[2].Time != 4 {
		t.Fatalf("cursor bounds or millisecond wrap: %+v", p.Events)
	}
}
