//go:build linux

package wlclient

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
)

func encodeHostEnter(serial uint32, x, y int32) []byte {
	p := wayland.PutU32(nil, serial)
	p = wayland.PutU32(p, 1) // surface
	p = wayland.PutI32(p, x)
	return wayland.PutI32(p, y)
}

func encodeHostKey(code, state uint32) []byte {
	p := wayland.PutU32(nil, 1)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, code)
	return wayland.PutU32(p, state)
}

func TestPointerEnterSetsCursorWithoutDeadlock(t *testing.T) {
	w := &Window{ptrID: 13, cursorDev: 15, kbdID: 14}
	// readLoop holds mu for the whole handle() call.
	w.mu.Lock()
	err := w.handlePointer(wayland.Message{
		Object:  13,
		Opcode:  0,
		Payload: encodeHostEnter(7, 10*256, 20*256),
	})
	w.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if w.ptrSerial != 7 || !w.hostInside || !w.hostCursorSet || w.hostCursorHidden {
		t.Fatalf("enter state serial=%d inside=%v set=%v hidden=%v",
			w.ptrSerial, w.hostInside, w.hostCursorSet, w.hostCursorHidden)
	}
	in := w.TakeInput()
	if !in.Inside || in.X != 10 || in.Y != 20 {
		t.Fatalf("TakeInput after enter %+v", in)
	}
}

func TestSeatEventsAfterCursorShapeUnderLock(t *testing.T) {
	w := &Window{ptrID: 13, cursorDev: 15, kbdID: 14}
	w.mu.Lock()
	if err := w.handlePointer(wayland.Message{
		Object: 13, Opcode: 0, Payload: encodeHostEnter(42, 4*256, 8*256),
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.handlePointer(wayland.Message{
		Object: 13, Opcode: 3, Payload: encodeHostButton(0x110, 1),
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.handleKeyboard(wayland.Message{
		Object: 14, Opcode: 3, Payload: encodeHostKey(30, 1),
	}); err != nil {
		t.Fatal(err)
	}
	w.mu.Unlock()

	in := w.TakeInput()
	if !in.Inside || !in.Click {
		t.Fatalf("click after set_shape %+v", in)
	}
	if len(in.Keys) != 1 || in.Keys[0].Code != 30 || !in.Keys[0].Pressed {
		t.Fatalf("key after set_shape %+v", in.Keys)
	}
}

func TestPointerEnterUnderReadLoopLock(t *testing.T) {
	w := &Window{ptrID: 13, cursorDev: 15}
	done := make(chan error, 1)
	go func() {
		// readLoop holds mu across handle(). 0.9.21 called EnsureHostCursor
		// here and deadlocked the nest (TakeInput never ran).
		w.mu.Lock()
		err := w.handlePointer(wayland.Message{
			Object: 13, Opcode: 0, Payload: encodeHostEnter(9, 0, 0),
		})
		w.mu.Unlock()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handlePointer enter re-locked Window.mu (0.9.21 freeze)")
	}
	in := w.TakeInput()
	if !in.Inside {
		t.Fatal("enter must still mark Inside so the shell loop sees the seat")
	}
}

func TestEncodeHostCursorShapeUsesEnterSerial(t *testing.T) {
	p := encodeHostCursorShape(42, hostCursorShapeDefault)
	cur := wayland.NewCursor(p, nil)
	ser, err := cur.U32()
	if err != nil || ser != 42 {
		t.Fatalf("serial %d %v", ser, err)
	}
	shape, err := cur.U32()
	if err != nil || shape != hostCursorShapeDefault {
		t.Fatalf("shape %d %v", shape, err)
	}
	if cur.Remaining() != 0 {
		t.Fatal("leftover")
	}
}

func TestSetHostCursorHiddenIdempotent(t *testing.T) {
	w := &Window{ptrID: 13, cursorDev: 15, ptrSerial: 3, hostCursorSet: true, hostCursorHidden: false}
	w.SetHostCursorHidden(false)
	if !w.hostCursorSet || w.hostCursorHidden {
		t.Fatal("show is idempotent")
	}
	w.SetHostCursorHidden(true)
	if !w.hostCursorHidden {
		t.Fatal("hide")
	}
}
