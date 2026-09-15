package shell

import (
	"testing"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func TestIsEvdevAcceptsWaylandOffset(t *testing.T) {
	if !isEvdev(1, keyEsc) || !isEvdev(9, keyEsc) {
		t.Fatal("esc")
	}
	if !isEvdev(88, keyF12) || !isEvdev(96, keyF12) {
		t.Fatal("f12")
	}
	if isEvdev(2, keyEsc) {
		t.Fatal("1 is not 2")
	}
}

func TestOverviewToggleKeys(t *testing.T) {
	if !isOverviewToggle(88, false) || !isOverviewToggle(96, false) {
		t.Fatal("F12")
	}
	if isOverviewToggle(15, false) {
		t.Fatal("bare tab is not toggle")
	}
	if !isOverviewToggle(15, true) || !isOverviewToggle(23, true) {
		t.Fatal("Super+Tab")
	}
}

func TestCtrlAltEvdevOffset(t *testing.T) {
	if !isCtrl(29) || !isCtrl(37) || !isAlt(56) || !isAlt(64) {
		t.Fatal("ctrl/alt evdev+8")
	}
}

func TestLauncherToggleKeys(t *testing.T) {
	if !isLauncherToggle(59, false) || !isLauncherToggle(67, false) {
		t.Fatal("F1")
	}
	if isLauncherToggle(57, false) {
		t.Fatal("bare space is not toggle")
	}
	if !isLauncherToggle(57, true) || !isLauncherToggle(65, true) {
		t.Fatal("Super+Space")
	}
}

func TestHandleQuitKeysCtrlQQuitsWithFocusedClient(t *testing.T) {
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyQ, Pressed: true}}}
	quit, cons := handleQuitKeys(ptr, true, false, true)
	if !quit || !cons[keyQ] || !ptr.Quit {
		t.Fatal("Ctrl+Q must quit even with a focused client")
	}
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyQ + 8, Pressed: true}}}
	quit, _ = handleQuitKeys(ptr, true, true, true)
	if !quit {
		t.Fatal("evdev+8 Ctrl+Q quits with overlay open too")
	}
}

func TestHandleQuitKeysBareQNeverQuits(t *testing.T) {
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyQ, Pressed: true}}}
	if quit, _ := handleQuitKeys(ptr, false, false, true); quit {
		t.Fatal("bare Q must not quit while a client is focused")
	}
	if quit, _ := handleQuitKeys(ptr, false, false, false); quit {
		t.Fatal("bare Q never quits, even on an empty desktop")
	}
}

func TestHandleQuitKeysEscWithClientDoesNotQuit(t *testing.T) {
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyEsc, Pressed: true}}}
	if quit, cons := handleQuitKeys(ptr, false, false, true); quit || cons[keyEsc] {
		t.Fatal("Esc must reach the focused client")
	}
}

func TestHandleQuitKeysEscEmptyDesktop(t *testing.T) {
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyEsc, Pressed: true}}}
	quit, cons := handleQuitKeys(ptr, false, false, false)
	if !quit || !cons[keyEsc] {
		t.Fatal("Esc on empty desktop still quits")
	}
	if quit, _ := handleQuitKeys(ptr, false, true, false); quit {
		t.Fatal("Esc in launcher/overview does not quit")
	}
}

func TestDesktopHasClient(t *testing.T) {
	scene := engine.NewScene()
	if desktopHasClient(scene) {
		t.Fatal("empty")
	}
	scene.Add(&engine.Actor{Width: 10, Height: 10})
	if !desktopHasClient(scene) {
		t.Fatal("mapped actor on active desktop")
	}
}
