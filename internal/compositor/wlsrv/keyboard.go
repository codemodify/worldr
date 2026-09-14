package wlsrv

import (
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
	"golang.org/x/sys/unix"
)

// Minimal US keymap so foot/kitty will accept wl_keyboard.
const xkbKeymap = `xkb_keymap {
xkb_keycodes "(unnamed)" {
    minimum = 8;
    maximum = 255;
    <ESC>  = 9;
    <AE01> = 10; <AE02> = 11; <AE03> = 12; <AE04> = 13; <AE05> = 14;
    <AE06> = 15; <AE07> = 16; <AE08> = 17; <AE09> = 18; <AE10> = 19;
    <AD01> = 24; <AD02> = 25; <AD03> = 26; <AD04> = 27; <AD05> = 28;
    <AD06> = 29; <AD07> = 30; <AD08> = 31; <AD09> = 32; <AD10> = 33;
    <AC01> = 38; <AC02> = 39; <AC03> = 40; <AC04> = 41; <AC05> = 42;
    <AC06> = 43; <AC07> = 44; <AC08> = 45; <AC09> = 46; <AC10> = 47;
    <AB01> = 52; <AB02> = 53; <AB03> = 54; <AB04> = 55; <AB05> = 56;
    <AB06> = 57; <AB07> = 58; <AB08> = 59; <AB09> = 60; <AB10> = 61;
    <SPCE> = 65; <RTRN> = 36; <BKSP> = 22; <TAB> = 23; <LFSH> = 50; <RTSH> = 62;
};
xkb_types "(unnamed)" {
    type "ONE_LEVEL" { modifiers = none; level_name[Level1] = "Any"; };
    type "TWO_LEVEL" { modifiers = Shift; map[Shift] = Level2; level_name[Level1] = "Base"; level_name[Level2] = "Shift"; };
};
xkb_compatibility "(unnamed)" { interpret.repeat = False; interpret.locking = False; };
xkb_symbols "(unnamed)" {
    name[Group1] = "worldr-us";
    key <ESC>  { [ Escape ] };
    key <AE01> { [ 1, exclam ] }; key <AE02> { [ 2, at ] };
    key <AD01> { [ q, Q ] }; key <AD02> { [ w, W ] }; key <AD03> { [ e, E ] };
    key <AC01> { [ a, A ] }; key <AC02> { [ s, S ] }; key <AC03> { [ d, D ] };
    key <AB01> { [ z, Z ] };
    key <SPCE> { [ space ] }; key <RTRN> { [ Return ] };
};
};
`

func (c *Client) sendKeymap(kbd uint32) error {
	b := []byte(xkbKeymap)
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
}

func (c *Client) keyboardLeave(sid uint32) {
	if c.kbdID == 0 || sid == 0 {
		return
	}
	p := wayland.PutU32(nil, c.nextSerial())
	p = wayland.PutU32(p, sid)
	_ = c.send(c.kbdID, 2, p, nil)
}

// KeyboardKey sends a linux evdev key code to the focused client (code is evdev, not xkb).
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
