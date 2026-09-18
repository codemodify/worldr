//go:build linux

package input

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"sort"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	evSyn = 0
	evKey = 1
	evRel = 2

	synReport   = 0
	synDropped  = 3
	relX        = 0
	relY        = 1
	relHWheel   = 6
	relWheel    = 8
	relWheelHi  = 11
	relHWheelHi = 12

	// Linux input_event contains two native unsigned longs, then type/code/value.
	eventWordBytes  = int(unsafe.Sizeof(uintptr(0)))
	eventBytes      = 2*eventWordBytes + 8
	maxPollEvents   = 256 // Per device, including partial reads and EINTR.
	maxReportEvents = 256
)

type packet struct {
	time  uint64 // Microseconds, retained until devices have been merged.
	typ   uint16
	code  uint16
	value int32
}

type device struct {
	fd       int
	buffer   [eventBytes]byte
	used     int
	frame    []packet
	dropping bool
	lastTime uint64
}

// Pointer owns a software cursor and an ordered queue of keyboard and relative
// pointer events. Poll resets Events; its slice is borrowed until the next Poll.
// Initial coordinates are the viewport center. This diagnostic direct-display
// path has no seat manager, hotplug discovery or absolute-device translation.
type Pointer struct {
	X, Y, W, H         int
	Events             []Event
	devices            []*device
	pending            []packet
	lost               bool
	lastTime           uint32
	managed            managedReader
	managedX, managedY float64
	managedErr         error
	closed             bool
}

// Open scans /dev/input/event* once, best effort. It does not grab the devices
// or change the active VT; permissions and session ownership belong to the host.
func Open(w, h int) *Pointer {
	p := &Pointer{X: w / 2, Y: h / 2, W: w, H: h}
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			p.devices = append(p.devices, &device{fd: fd})
		}
	}
	return p
}

func (p *Pointer) Close() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	if p.managed != nil {
		p.managedErr = errors.Join(p.managedErr, p.managed.Close())
		p.managed = nil
	}
	for _, device := range p.devices {
		_ = unix.Close(device.fd)
	}
	p.devices = nil
	p.Events = nil
	p.pending = nil
}

// Poll performs bounded nonblocking work. Only complete SYN_REPORT frames are
// exposed, so a button keeps its original cursor position and high-resolution
// wheel events replace the duplicate legacy wheel value in the same report.
// Completed reports from separate devices are merged by kernel timestamp.
func (p *Pointer) Poll() {
	if p.closed {
		p.Events = nil
		return
	}
	if p.managed != nil {
		p.pollManaged()
		return
	}
	p.Events = p.Events[:0]
	p.pending = p.pending[:0]
	p.lost = false
	live := p.devices[:0]
	for _, device := range p.devices {
		if p.read(device) {
			live = append(live, device)
		}
	}
	clear(p.devices[len(live):])
	p.devices = live
	sort.SliceStable(p.pending, func(i, j int) bool { return p.pending[i].time < p.pending[j].time })
	for _, packet := range p.pending {
		p.apply(packet)
		p.lastTime = uint32(packet.time / 1000)
	}
	if p.lost {
		// EOF has no kernel timestamp. Cancel after this batch, so a later
		// device read cannot reintroduce held state after the cancellation.
		p.Events = append(p.Events, Event{Kind: Cancel, X: p.X, Y: p.Y, Time: p.lastTime})
	}
}

func (p *Pointer) read(device *device) bool {
	for i := 0; i < maxPollEvents; i++ {
		n, err := unix.Read(device.fd, device.buffer[device.used:])
		if err == unix.EINTR {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			return true
		}
		if err != nil || n == 0 {
			// Lost devices must not leave selection, modifiers or repeat active.
			// Discard unfinished reports and permanently retire the descriptor.
			p.lost = true
			_ = unix.Close(device.fd)
			return false
		}
		device.used += n
		if device.used != eventBytes {
			continue
		}
		device.used = 0
		device.accept(p, decodePacket(device.buffer[:]))
	}
	return true
}

func decodePacket(data []byte) packet {
	word := func(data []byte) uint64 {
		if eventWordBytes == 8 {
			return binary.NativeEndian.Uint64(data)
		}
		return uint64(binary.NativeEndian.Uint32(data))
	}
	return packet{
		time:  word(data)*1_000_000 + word(data[eventWordBytes:]),
		typ:   binary.NativeEndian.Uint16(data[2*eventWordBytes:]),
		code:  binary.NativeEndian.Uint16(data[2*eventWordBytes+2:]),
		value: int32(binary.NativeEndian.Uint32(data[2*eventWordBytes+4:])),
	}
}

func (d *device) accept(p *Pointer, event packet) {
	// Preserve device order if its clock moves backwards. Normal timestamps
	// still let keyboard modifiers precede a later mouse click.
	if event.time < d.lastTime {
		event.time = d.lastTime
	}
	d.lastTime = event.time
	if event.typ == evSyn && event.code == synDropped {
		d.frame = d.frame[:0]
		d.dropping = true
		p.pending = append(p.pending, event)
		return
	}
	if d.dropping {
		if event.typ == evSyn && event.code == synReport {
			d.dropping = false
		}
		return
	}
	if event.typ == evSyn && event.code == synReport {
		var horizontalHi, verticalHi bool
		for _, item := range d.frame {
			if item.typ == evRel {
				horizontalHi = horizontalHi || item.code == relHWheelHi
				verticalHi = verticalHi || item.code == relWheelHi
			}
		}
		for _, item := range d.frame {
			if item.typ == evRel && (item.code == relHWheel && horizontalHi || item.code == relWheel && verticalHi) {
				continue // Both deltas describe the same physical wheel movement.
			}
			p.pending = append(p.pending, item)
		}
		d.frame = d.frame[:0]
		return
	}
	if event.typ != evKey && event.typ != evRel {
		return
	}
	if len(d.frame) == maxReportEvents {
		d.frame = d.frame[:0]
		d.dropping = true
		p.pending = append(p.pending, packet{time: event.time, typ: evSyn, code: synDropped})
		return
	}
	d.frame = append(d.frame, event)
}

func (p *Pointer) apply(packet packet) {
	event := Event{Code: uint32(packet.code), Time: uint32(packet.time / 1000)}
	switch packet.typ {
	case evSyn:
		if packet.code != synDropped {
			return
		}
		event.Kind = Cancel
	case evKey:
		if packet.value != 0 && packet.value != 1 || packet.code == 0 || packet.code > 767 {
			return // Apps synthesize repeat; kernel value 2 must not double it.
		}
		event.Pressed = packet.value == 1
		if isButton(packet.code) {
			event.Kind = Up
			if event.Pressed {
				event.Kind = Down
			}
		} else {
			event.Kind = KeyInput
		}
	case evRel:
		if packet.value == 0 {
			return
		}
		switch packet.code {
		case relX, relY:
			if packet.code == relX {
				p.X = int(max(int64(0), min(int64(max(0, p.W-1)), int64(p.X)+int64(packet.value))))
			} else {
				p.Y = int(max(int64(0), min(int64(max(0, p.H-1)), int64(p.Y)+int64(packet.value))))
			}
			event.Kind = Move
		case relWheel:
			event.Kind, event.ScrollY = Scroll, -float32(packet.value)*10
		case relHWheel:
			event.Kind, event.ScrollX = Scroll, float32(packet.value)*10
		case relWheelHi:
			event.Kind, event.ScrollY = Scroll, -float32(packet.value)/12
		case relHWheelHi:
			event.Kind, event.ScrollX = Scroll, float32(packet.value)/12
		default:
			return
		}
	default:
		return
	}
	event.X, event.Y = p.X, p.Y
	p.Events = append(p.Events, event)
}

// EV_KEY shares keyboard keys with disjoint BTN ranges. Extended keyboard
// keys such as KEY_OK (0x160) and KEY_FN (0x1d0) remain raw keys.
func isButton(code uint16) bool {
	return code >= 0x100 && code < 0x160 || code >= 0x220 && code <= 0x223 || code >= 0x2c0 && code <= 0x2e7
}
