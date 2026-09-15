//go:build linux

package wlclient

import (
	"syscall"

	"github.com/codemodify/worldr/internal/wayland"
)

func fixedToInt(v int32) int { return int(v) / 256 }

func (w *Window) noteSerial(ser uint32) {
	if w == nil || ser == 0 {
		return
	}
	w.ptrSerial = ser
	w.flushPendingHostOffer()
}

func (w *Window) handleSeat(msg wayland.Message) error {
	if w.ptrID != 0 && msg.Object == w.ptrID {
		return w.handlePointer(msg)
	}
	if w.kbdID != 0 && msg.Object == w.kbdID {
		return w.handleKeyboard(msg)
	}
	return nil
}

func (w *Window) handlePointer(msg wayland.Message) error {
	cur := wayland.NewCursor(msg.Payload, nil)
	switch msg.Opcode {
	case 0: // enter
		ser, _ := cur.U32()
		_, _ = cur.U32() // surface
		x, _ := cur.I32()
		y, _ := cur.I32()
		w.noteSerial(ser)
		w.hostX, w.hostY = fixedToInt(x), fixedToInt(y)
		w.hostInside = true
		// readLoop already holds w.mu — do not call EnsureHostCursor
		// (it locks). A nested Lock froze the nest: TakeInput waited
		// forever, 0% CPU, no click/key.
		w.hostCursorHidden = false
		w.hostCursorSet = true
		if ser != 0 {
			w.sendHostCursor(ser, w.cursorDev, w.cursorSurf, false)
		}
	case 1: // leave
		w.hostInside = false
		// Host implicit grab usually delivers the real release; if the
		// host only sends leave, synthesize a matching release so worldr
		// clients are not left with a button down.
		if w.hostBtnDown {
			w.hostRelease = true
			w.hostBtnDown = false
		}
	case 2: // motion
		_, _ = cur.U32()
		x, _ := cur.I32()
		y, _ := cur.I32()
		w.hostX, w.hostY = fixedToInt(x), fixedToInt(y)
	case 3: // button
		ser, _ := cur.U32()
		w.noteSerial(ser)
		_, _ = cur.U32()
		btn, _ := cur.U32()
		state, _ := cur.U32()
		if btn == 0x110 { // BTN_LEFT
			if state == 1 {
				w.hostClick = true
				w.hostBtnDown = true
			} else if w.hostBtnDown {
				w.hostRelease = true
				w.hostBtnDown = false
			}
			// Unmatched host release (click started on Plasma chrome) is dropped.
		}
	}
	return nil
}

func (w *Window) handleKeyboard(msg wayland.Message) error {
	cur := wayland.NewCursor(msg.Payload, nil)
	switch msg.Opcode {
	case 0: // keymap — consume the fd so later shm pools stay aligned
		_, _ = cur.U32()
		_, _ = cur.U32()
		if fd, err := w.rd.TakeFD(); err == nil && fd >= 0 {
			_ = syscall.Close(fd)
		}
	case 1: // enter
		ser, _ := cur.U32()
		w.noteSerial(ser)
	case 3: // key
		ser, _ := cur.U32()
		w.noteSerial(ser)
		_, _ = cur.U32()
		code, _ := cur.U32()
		state, _ := cur.U32()
		w.hostKeys = append(w.hostKeys, HostKey{Code: code, Pressed: state == 1})
	}
	return nil
}

// TakeInput returns and clears host pointer/key edges.
func (w *Window) TakeInput() Input {
	if w == nil {
		return Input{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hostX < 0 {
		w.hostX = 0
	}
	if w.hostY < 0 {
		w.hostY = 0
	}
	if w.w > 0 && w.hostX >= w.w {
		w.hostX = w.w - 1
	}
	if w.h > 0 && w.hostY >= w.h {
		w.hostY = w.h - 1
	}
	in := Input{
		X: w.hostX, Y: w.hostY,
		Click: w.hostClick, Release: w.hostRelease,
		Inside: w.hostInside,
		Keys:   append([]HostKey(nil), w.hostKeys...),
	}
	w.hostClick, w.hostRelease = false, false
	w.hostKeys = w.hostKeys[:0]
	return in
}
