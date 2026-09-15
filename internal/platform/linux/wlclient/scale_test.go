//go:build linux

package wlclient

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/wayland"
)

func TestHandleOutputScale(t *testing.T) {
	w := &Window{output: 5}
	p := wayland.PutI32(nil, 2)
	if err := w.handleScale(wayland.Message{Object: 5, Opcode: wlOutputScale, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if w.HostScale() != 2 {
		t.Fatalf("got %v", w.HostScale())
	}
}

func TestHandleFracScaleWins(t *testing.T) {
	w := &Window{output: 5, fracID: 9, hostOutScale: 1}
	p := wayland.PutU32(nil, 180)
	if err := w.handleScale(wayland.Message{Object: 9, Opcode: fracPrefScale, Payload: p}); err != nil {
		t.Fatal(err)
	}
	if w.HostScale() != 1.5 {
		t.Fatalf("got %v", w.HostScale())
	}
}

func TestOnHostScaleChange(t *testing.T) {
	w := &Window{output: 5}
	got := make(chan float64, 1)
	w.OnHostScale(func(sc float64) { got <- sc })
	p := wayland.PutI32(nil, 2)
	if err := w.handleScale(wayland.Message{Object: 5, Opcode: wlOutputScale, Payload: p}); err != nil {
		t.Fatal(err)
	}
	select {
	case sc := <-got:
		if sc != 2 {
			t.Fatal(sc)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("notify")
	}
}

func TestHostScaleDefault(t *testing.T) {
	if (*Window)(nil).HostScale() != 1 {
		t.Fatal("nil")
	}
	if (&Window{}).HostScale() != 1 {
		t.Fatal("empty")
	}
}
