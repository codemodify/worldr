//go:build linux

package wlclient

import (
	"testing"

	"github.com/codemodify/worldr/internal/wayland"
)

func TestHostPointerPressThenRelease(t *testing.T) {
	w := &Window{ptrID: 1}
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 3, Payload: encodeHostButton(0x110, 1)}); err != nil {
		t.Fatal(err)
	}
	in := w.TakeInput()
	if !in.Click || in.Release {
		t.Fatalf("press %+v", in)
	}
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 3, Payload: encodeHostButton(0x110, 0)}); err != nil {
		t.Fatal(err)
	}
	in = w.TakeInput()
	if in.Click || !in.Release {
		t.Fatalf("release %+v", in)
	}
}

func TestHostPointerDropsUnmatchedRelease(t *testing.T) {
	w := &Window{ptrID: 1}
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 3, Payload: encodeHostButton(0x110, 0)}); err != nil {
		t.Fatal(err)
	}
	in := w.TakeInput()
	if in.Release || in.Click {
		t.Fatalf("stray host release forwarded: %+v", in)
	}
}

func TestHostPointerLeaveWhileDownSynthesizesRelease(t *testing.T) {
	w := &Window{ptrID: 1}
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 3, Payload: encodeHostButton(0x110, 1)}); err != nil {
		t.Fatal(err)
	}
	_ = w.TakeInput()
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 1, Payload: wayland.PutU32(wayland.PutU32(nil, 1), 0)}); err != nil {
		t.Fatal(err)
	}
	in := w.TakeInput()
	if !in.Release {
		t.Fatal("leave-while-down must synthesize a matching release")
	}
	if err := w.handlePointer(wayland.Message{Object: 1, Opcode: 3, Payload: encodeHostButton(0x110, 0)}); err != nil {
		t.Fatal(err)
	}
	in = w.TakeInput()
	if in.Release {
		t.Fatal("host release after synthesized leave must not double-fire")
	}
}

func encodeHostButton(btn, state uint32) []byte {
	p := wayland.PutU32(nil, 1)
	p = wayland.PutU32(p, 0)
	p = wayland.PutU32(p, btn)
	return wayland.PutU32(p, state)
}
