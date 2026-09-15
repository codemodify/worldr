package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func testLauncherItems() []LaunchItem {
	return fallbackCatalog(false)
}

func TestHandleLauncherKeysF1AndEsc(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	meta := false
	now := time.Unix(1, 0)
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyF1, Pressed: true}}}
	handleLauncherKeys(&ln, ptr, now, &meta, nil)
	if !ln.Open {
		t.Fatal("F1 should open")
	}
	ptr = &input.Pointer{Quit: true, Keys: []input.Key{{Code: keyEsc, Pressed: true}}}
	handleLauncherKeys(&ln, ptr, now.Add(time.Second), &meta, nil)
	if ln.Open {
		t.Fatal("Esc should close")
	}
	if ptr.Quit {
		t.Fatal("Esc must not quit")
	}
}

func TestHandleLauncherKeysSuperSpaceAndEnter(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	meta := true
	now := time.Unix(1, 0)
	ptr := &input.Pointer{Keys: []input.Key{{Code: keySpace, Pressed: true}}}
	_, spawn := handleLauncherKeys(&ln, ptr, now, &meta, nil)
	if !ln.Open || spawn != nil {
		t.Fatal("Super+Space should open")
	}
	meta = false
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyDown, Pressed: true}}}
	handleLauncherKeys(&ln, ptr, now, &meta, nil)
	if ln.Select != 1 {
		t.Fatal(ln.Select)
	}
	ptr = &input.Pointer{Keys: []input.Key{{Code: keyEnter, Pressed: true}}}
	_, spawn = handleLauncherKeys(&ln, ptr, now, &meta, nil)
	if spawn == nil || spawn.Bin != "weston-simple-shm" {
		t.Fatalf("spawn %+v", spawn)
	}
	if ln.Open {
		t.Fatal("enter closes")
	}
}

func TestHandleLauncherKeysF1WaylandOffset(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	meta := false
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyF1 + 8, Pressed: true}}}
	handleLauncherKeys(&ln, ptr, time.Unix(1, 0), &meta, nil)
	if !ln.Open {
		t.Fatal("nested evdev+8 F1")
	}
}

func TestLauncherOutsideCloseSuppressedUntilRelease(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	var g launcherGate
	now := time.Unix(1, 0)
	const w, h = 800, 600

	handlePanelAppsClick(&ln, &g, now)
	if !ln.Open || !g.ignoreOutside {
		t.Fatal("apps should open and arm ignoreOutside")
	}

	// Same press, pointer reported in the desktop (not the card).
	spawn, ate := handleLauncherDesktopClick(&ln, &g, 1, 1, w, h, PanelH)
	if spawn != nil || !ate {
		t.Fatalf("opening press must be consumed spawn=%v ate=%v", spawn, ate)
	}
	if !ln.Open {
		t.Fatal("outside-close suppressed until release")
	}

	g.onRelease()
	if g.ignoreOutside {
		t.Fatal("release clears ignoreOutside")
	}

	spawn, ate = handleLauncherDesktopClick(&ln, &g, 1, 1, w, h, PanelH)
	if spawn != nil || !ate {
		t.Fatalf("after release outside click should dismiss spawn=%v ate=%v", spawn, ate)
	}
	if ln.Open {
		t.Fatal("outside click after release closes")
	}
}

func TestPanelAppsExplicitCloseAfterDebounce(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	var g launcherGate
	now := time.Unix(1, 0)

	handlePanelAppsClick(&ln, &g, now)
	handlePanelAppsClick(&ln, &g, now.Add(20*time.Millisecond))
	if !ln.Open {
		t.Fatal("second apps click within debounce must not close")
	}

	handlePanelAppsClick(&ln, &g, now.Add(200*time.Millisecond))
	if ln.Open {
		t.Fatal("apps click after debounce closes")
	}
}

func TestHandleLauncherKeysF1ArmsIgnoreOutside(t *testing.T) {
	ln := Launcher{Items: testLauncherItems()}
	var g launcherGate
	meta := false
	ptr := &input.Pointer{Keys: []input.Key{{Code: keyF1, Pressed: true}}}
	handleLauncherKeys(&ln, ptr, time.Unix(1, 0), &meta, &g)
	if !ln.Open || !g.ignoreOutside {
		t.Fatal("F1 open must arm ignoreOutside")
	}
	_, ate := handleLauncherDesktopClick(&ln, &g, 2, 2, 800, 600, PanelH)
	if !ln.Open || !ate {
		t.Fatal("F1 then leftover desktop click must not dismiss")
	}
}

func TestClientEnvironWaylandStripsHost(t *testing.T) {
	base := []string{"HOME=/tmp", "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0", "WAYLAND_SOCKET=4", "PATH=/bin"}
	env := ClientEnviron(base, "wayland-1", ":1", false)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "WAYLAND_DISPLAY=wayland-0") || strings.Contains(joined, "WAYLAND_SOCKET=") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "WAYLAND_DISPLAY=wayland-1") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "DISPLAY=:1") {
		t.Fatal("keep worldr X11 display for Wayland clients")
	}
}

func TestClientEnvironX11Only(t *testing.T) {
	base := []string{"WAYLAND_DISPLAY=wayland-1", "DISPLAY=:0", "HOME=/tmp"}
	env := ClientEnviron(base, "wayland-1", ":2", true)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "WAYLAND_DISPLAY=") {
		t.Fatal("xeyes must not inherit WAYLAND_DISPLAY")
	}
	if !strings.Contains(joined, "DISPLAY=:2") {
		t.Fatal(joined)
	}
}

func TestOverviewKeysStealNavLeavesEsc(t *testing.T) {
	// When the launcher is open, Esc must not close overview (launcher owns Esc).
	scene := engine.NewScene()
	var ov Overview
	ov.Open(time.Unix(1, 0))
	meta := false
	ptr := &input.Pointer{Quit: true, Keys: []input.Key{{Code: keyEsc, Pressed: true}}}
	handleOverviewKeys(&ov, ptr, scene, time.Unix(2, 0), 800, 600, &meta, true, nil, nil)
	if !ov.Want {
		t.Fatal("stealNav should leave overview open")
	}
}
