package wlsrv

import (
	_ "embed"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

// Full US keymap (evdev keycodes, XKB = evdev+8). Regenerated with:
//
//	setxkbmap -layout us -print | xkbcomp -xkb - keymap_us.xkb
//
// or: xkbcli compile-keymap --layout us
//
//go:embed keymap_us.xkb
var xkbKeymap []byte

// keymapBytes is the mmap payload: compiled text plus a trailing NUL
// (required by wl_keyboard.keymap xkb_v1).
func keymapBytes() []byte {
	if len(xkbKeymap) == 0 || xkbKeymap[len(xkbKeymap)-1] != 0 {
		out := make([]byte, len(xkbKeymap)+1)
		copy(out, xkbKeymap)
		return out
	}
	return xkbKeymap
}

// ToXKBKeycode maps a linux evdev scancode to a Wayland/XKB keycode (evdev+8).
// Nested host seats already send XKB codes — pass those through.
func ToXKBKeycode(code uint32, alreadyXKB bool) uint32 {
	if alreadyXKB {
		return code
	}
	return code + 8
}

func (c *Client) sendKeymap(kbd uint32) error {
	b := keymapBytes()
	fd, err := unix.MemfdCreate("worldr-xkb", 0)
	if err != nil {
		return err
	}
	if err := unix.Ftruncate(fd, int64(len(b))); err != nil {
		_ = syscall.Close(fd)
		return err
	}
	mem, err := syscall.Mmap(fd, 0, len(b), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Close(fd)
		return err
	}
	copy(mem, b)
	_ = syscall.Munmap(mem)
	p := wayland.PutU32(nil, 1) // xkb_v1
	p = wayland.PutU32(p, uint32(len(b)))
	err = c.send(kbd, 0, p, []int{fd})
	_ = syscall.Close(fd)
	if err != nil {
		return err
	}
	// repeat_info
	p = wayland.PutI32(nil, 25)
	p = wayland.PutI32(p, 600)
	return c.send(kbd, 5, p, nil)
}

func (c *Client) keyboardEnter(s *surface) {
	if c.kbdID == 0 || s == nil {
		return
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, s.id)
	p = wayland.PutArray(p, nil)
	_ = c.send(c.kbdID, 1, p, nil)
	p = wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, 0)
	_ = c.send(c.kbdID, 4, p, nil) // modifiers
	c.textInputEnter(s)
}

func (c *Client) keyboardLeave(sid uint32) {
	c.textInputLeave(sid)
	if c.kbdID == 0 || sid == 0 {
		return
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, sid)
	_ = c.send(c.kbdID, 2, p, nil)
}

// KeyboardKey sends a Wayland/XKB keycode (evdev+8) to the focused client.
func (c *Client) KeyboardKey(code uint32, pressed bool) {
	if c.kbdID == 0 {
		return
	}
	state := uint32(0)
	if pressed {
		state = 1
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, uint32(time.Now().UnixMilli()))
	p = wayland.PutU32(p, code)
	p = wayland.PutU32(p, state)
	_ = c.send(c.kbdID, 3, p, nil)
}
