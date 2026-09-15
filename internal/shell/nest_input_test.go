package shell

import (
	"testing"

	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/platform/linux/wlclient"
)

func TestApplyNestedPointerIgnoresEvdevClick(t *testing.T) {
	ptr := &input.Pointer{
		X: 10, Y: 20,
		Click:   true,
		Release: false,
		Quit:    true,
		Keys:    []input.Key{{Code: keyF1, Pressed: true}},
	}
	in := wlclient.Input{
		X: 100, Y: 200,
		Click:   false,
		Release: true,
		Keys:    []wlclient.HostKey{{Code: keyEsc, Pressed: true}},
	}
	applyNestedPointer(ptr, in)
	if ptr.X != 100 || ptr.Y != 200 {
		t.Fatalf("pos %d,%d", ptr.X, ptr.Y)
	}
	if ptr.Click {
		t.Fatal("evdev Click must not survive when wl did not click")
	}
	if !ptr.Release {
		t.Fatal("wl Release")
	}
	if len(ptr.Keys) != 1 || ptr.Keys[0].Code != keyEsc {
		t.Fatalf("keys %+v", ptr.Keys)
	}
	if ptr.Quit {
		t.Fatal("wl Esc must not set Quit; handleQuitKeys owns the chord")
	}
}

func TestFilterNestedButtonsPressThenRelease(t *testing.T) {
	ptr := &input.Pointer{Click: true}
	down := false
	filterNestedButtons(ptr, &down)
	if !down || !ptr.Click {
		t.Fatal("press arms host button")
	}
	ptr.Click = false
	ptr.Release = true
	filterNestedButtons(ptr, &down)
	if !ptr.Release || down {
		t.Fatal("matching release")
	}
}

func TestFilterNestedButtonsReleaseOnlySuppressed(t *testing.T) {
	ptr := &input.Pointer{Release: true}
	down := false
	filterNestedButtons(ptr, &down)
	if ptr.Release {
		t.Fatal("unmatched host release must not reach the shell")
	}
	if down {
		t.Fatal("still up")
	}
}

func TestClientButtonGate(t *testing.T) {
	var g clientButtonGate
	if g.onRelease() {
		t.Fatal("release without press")
	}
	g.onClientPress()
	if !g.onRelease() {
		t.Fatal("matching release")
	}
	if g.onRelease() {
		t.Fatal("second release")
	}
}
