//go:build linux && cgo

package host

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Exercise the actual Wayland callbacks and C-to-Go ownership boundary. The
// keyboard map travels as a protocol fd, just as it does from a real host.
func TestHostTransportsApplicationInputAndFocusLoss(t *testing.T) {
	keymap := "xkb_keymap { xkb_keycodes { include \"evdev+aliases(qwerty)\" }; xkb_types { include \"complete\" }; xkb_compatibility { include \"complete\" }; xkb_symbols { include \"pc+us+inet(evdev)\" }; };\x00"
	file, err := os.CreateTemp(t.TempDir(), "keymap")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if _, err := file.WriteString(keymap); err != nil {
		t.Fatal(err)
	}
	fakeCompositor(t, func(conn net.Conn) {
		serveInputHost(conn, func(surface, pointer, keyboard uint32) {
			packet := wireMessage(keyboard, 0, []uint32{1, uint32(len(keymap))})
			conn.(*net.UnixConn).WriteMsgUnix(packet, unix.UnixRights(int(file.Fd())), nil)
			writeMessage(conn, keyboard, 1, []uint32{2, surface, 0}) // enter, empty held-key array
			writeMessage(conn, keyboard, 5, []uint32{25, 600})
			writeMessage(conn, keyboard, 4, []uint32{3, 4, 0, 2, 0}) // Ctrl + Caps Lock
			writeMessage(conn, keyboard, 3, []uint32{4, 120, 30, 1}) // A has no AXIAL semantic key
			writeMessage(conn, keyboard, 3, []uint32{5, 121, 30, 0})
			writeMessage(conn, keyboard, 4, []uint32{6, 0, 0, 2, 0})
			writeMessage(conn, keyboard, 2, []uint32{7, surface})
			writeMessage(conn, pointer, 0, []uint32{8, surface, 90 * 256, 112 * 256})
			for i, button := range []uint32{0x110, 0x111, 0x112, 0x113} {
				writeMessage(conn, pointer, 3, []uint32{9, 130 + uint32(i), button, 1})
			}
			writeMessage(conn, pointer, 4, []uint32{140, 0, 10 * 256})
			writeMessage(conn, pointer, 4, []uint32{141, 1, 640}) // +2.5 horizontal units
			writeMessage(conn, pointer, 5, nil)                   // frame, introduced in seat v5
			writeMessage(conn, pointer, 1, []uint32{10, surface})
			writeMessage(conn, pointer, 3, []uint32{11, 142, 0x110, 0}) // canceled release
		})
	})
	window, err := open("input transport", 640, 480, false, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer window.Close()
	var events []Event
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		batch, err := window.Poll(nil)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, batch...)
		if len(events) > 0 && events[len(events)-1].Kind == Cancel {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var maps, keys, buttons, scrolls, cancels, keyboardCancels int
	var keymapCopy string
	modifierRelease, repeatPolicy := false, false
	for _, e := range events {
		switch e.Kind {
		case KeymapChanged:
			maps++
			keymapCopy = e.Keymap
			if !strings.Contains(e.Keymap, "xkb_keymap") || !strings.Contains(e.Keymap, "<AC01>") {
				t.Fatalf("keymap fd not decoded: %q", e.Keymap)
			}
		case Key:
			if e.Code != 'A' || e.Keycode != 30 || e.Time != uint32(120+keys) || e.Pressed != (keys == 0) || e.Depressed != 4 || e.Locked != 2 || e.Modifiers != 1 {
				t.Fatalf("key %d lost raw data: %+v", keys, e)
			}
			keys++
		case Down:
			if e.ButtonCode != uint32(0x110+buttons) || !e.Pressed || e.X != 90 || e.Y != 112 {
				t.Fatalf("button %d lost: %+v", buttons, e)
			}
			buttons++
		case Up:
			t.Fatalf("release escaped canceled capture: %+v", e)
		case Scroll:
			if e.X != 90 || e.Y != 112 || (scrolls == 0 && (e.ScrollY != 10 || e.ScrollX != 0)) || (scrolls == 1 && (e.ScrollX != 2.5 || e.ScrollY != 0)) {
				t.Fatalf("axis %d lost: %+v", scrolls, e)
			}
			scrolls++
		case Cancel:
			cancels++
		case KeyboardCancel:
			keyboardCancels++
		case ModifiersChanged:
			modifierRelease = modifierRelease || e.Depressed == 0 && e.Locked == 2
		case RepeatInfo:
			repeatPolicy = e.RepeatRate == 25 && e.RepeatDelay == 600
		}
	}
	if maps != 1 || keys != 2 || buttons != 4 || scrolls != 2 || cancels != 1 || keyboardCancels < 1 || !modifierRelease || !repeatPolicy {
		t.Fatalf("incomplete input: maps=%d keys=%d buttons=%d scrolls=%d pointer cancellations=%d keyboard cancellations=%d modifiers=%v repeat=%v, events=%+v", maps, keys, buttons, scrolls, cancels, keyboardCancels, modifierRelease, repeatPolicy, events)
	}
	window.Close()
	if !strings.Contains(keymapCopy, "xkb_keymap") {
		t.Fatal("Go keymap did not survive native host cleanup")
	}
}

func wireMessage(object, opcode uint32, payload []uint32) []byte {
	data := make([]byte, 8+4*len(payload))
	binary.NativeEndian.PutUint32(data, object)
	binary.NativeEndian.PutUint32(data[4:], uint32(len(data))<<16|opcode)
	for i, value := range payload {
		binary.NativeEndian.PutUint32(data[8+i*4:], value)
	}
	return data
}

// Minimal server for a real host.Open handshake, followed by scripted input.
func serveInputHost(conn net.Conn, send func(surface, pointer, keyboard uint32)) {
	var registry, compositor, wm, seat, surface, xdg, top, pointer, keyboard uint32
	configured, sent := false, false
	for {
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		object, word := binary.NativeEndian.Uint32(header[:4]), binary.NativeEndian.Uint32(header[4:])
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
				writeMessage(conn, seat, 0, []uint32{3})
			}
		case object == compositor && opcode == 0:
			surface = arg(0)
		case object == wm && opcode == 2:
			xdg = arg(0)
		case object == xdg && opcode == 1:
			top = arg(0)
		case object == seat && opcode == 0:
			pointer = arg(0)
		case object == seat && opcode == 1:
			keyboard = arg(0)
		case object == surface && opcode == 6 && !configured:
			writeMessage(conn, top, 0, []uint32{640, 480, 0})
			writeMessage(conn, xdg, 0, []uint32{1})
			configured = true
		}
		if configured && pointer != 0 && keyboard != 0 && !sent {
			send(surface, pointer, keyboard)
			sent = true
		}
	}
}
