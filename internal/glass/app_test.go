//go:build linux && cgo

package glass

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

func testApp(t *testing.T, script string) *App {
	t.Helper()
	a, err := New(terminal.Options{Command: "/bin/sh", Args: []string{"-c", script}, Cols: 80, Rows: 12, Scrollback: 100}, DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	a.Draw(1100, 720)
	return a
}

func waitApp(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.Update(time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	var screens []string
	for _, p := range a.tabs {
		screens = append(screens, paneText(p.snapshot))
	}
	t.Fatalf("application did not reach expected state: active=%d screens=%q", a.active, screens)
}

func appKey(a *App, code uint32, key experience.Key, mods experience.Modifiers) {
	depressed := uint32(0)
	if mods.Has(experience.ModShift) {
		depressed |= 1
	}
	if mods.Has(experience.ModControl) {
		depressed |= 4
	}
	if mods.Has(experience.ModAlt) {
		depressed |= 8
	}
	for _, pressed := range []bool{true, false} {
		a.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Key: key, Modifiers: mods, Depressed: depressed, Pressed: pressed})
	}
}

func TestPaletteChangesReachExistingAndNewShellTabs(t *testing.T) {
	a := testApp(t, "printf '\033[34mBLUE\033[0m READY'; IFS= read -r next")
	ready := func() bool {
		for _, p := range a.tabs {
			if !strings.Contains(paneText(p.snapshot), "READY") {
				return false
			}
		}
		return true
	}
	waitApp(t, a, ready)
	assertColor := func(theme int) {
		t.Helper()
		for _, p := range a.tabs {
			if p.snapshot.Cells[0].Foreground != ansiPalette(theme)[4] {
				t.Fatalf("tab %d did not apply theme %d: %+v", p.id, theme, p.snapshot.Cells[0].Foreground)
			}
		}
	}
	assertColor(0)
	if err := a.addTab(); err != nil {
		t.Fatal(err)
	}
	waitApp(t, a, ready)
	a.prefs.Palette = 2
	if err := a.Update(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	assertColor(2)
	if err := a.addTab(); err != nil {
		t.Fatal(err)
	}
	waitApp(t, a, ready)
	assertColor(2)
}

func clickAppButton(t *testing.T, a *App, id string) {
	t.Helper()
	for _, b := range a.buttons {
		if b.id != id {
			continue
		}
		x, y := (b.box.x+b.box.w/2)*a.unit, (b.box.y+b.box.h/2)*a.unit
		for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
			a.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: x, Y: y})
		}
		return
	}
	t.Fatalf("button %q missing", id)
}

func TestApplicationChromeAndShortcutsDoNotLeakIntoPTY(t *testing.T) {
	a := testApp(t, "stty raw -echo; printf READY; dd bs=1 count=1 2>/dev/null | od -An -tx1")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	for _, e := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: (a.body.x + .1*a.cellW) * a.unit, Y: (a.body.y + .4*a.cellH) * a.unit},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: (a.body.x + 4.1*a.cellW) * a.unit, Y: (a.body.y + .4*a.cellH) * a.unit},
	} {
		a.Handle(e)
	}
	if text, ok := a.pane().Copy(); !ok || text != "READY" {
		t.Fatalf("framebuffer selection conversion: %q %v", text, ok)
	}
	appKey(a, 46, experience.KeyC, experience.ModControl|experience.ModShift)
	clickAppButton(t, a, "copy")
	clickAppButton(t, a, "appearance")
	if !a.settings {
		t.Fatal("Appearance button did not open settings")
	}
	a.Draw(1100, 720)
	appKey(a, 30, experience.Key("A"), 0) // Modal appearance owns text input.
	appKey(a, 1, experience.KeyEscape, 0)
	if a.settings {
		t.Fatal("Escape did not close settings")
	}
	appKey(a, 30, experience.Key("A"), 0)
	waitApp(t, a, func() bool { return a.pane().snapshot.Exited })
	bytes := strings.Join(strings.Fields(strings.TrimPrefix(paneText(a.pane().snapshot), "READY")), " ")
	if bytes != "61" {
		t.Fatalf("chrome/shortcuts leaked bytes before ordinary a: %q", bytes)
	}
	if len(a.owned) != 0 {
		t.Fatalf("shortcut releases remained captured: %+v", a.owned)
	}
}

func TestApplicationTabsKeepBackgroundSessionsAndTransition(t *testing.T) {
	a := testApp(t, "stty -echo; printf 'READY\r\n'; IFS= read -r next; sleep .03; printf 'BACKGROUND:%s\r\n' \"$next\"; IFS= read -r next; printf 'FINISH:%s\r\n' \"$next\"")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	first := a.pane()
	if err := first.Paste("one\n"); err != nil {
		t.Fatal(err)
	}
	appKey(a, 20, experience.Key("T"), experience.ModControl|experience.ModShift)
	if len(a.tabs) != 2 || a.active != 1 || a.pane() == first {
		t.Fatalf("new session: count=%d active=%d", len(a.tabs), a.active)
	}
	second := a.pane()
	a.Draw(1100, 720)
	waitApp(t, a, func() bool {
		return strings.Contains(paneText(first.snapshot), "BACKGROUND:one") && strings.Contains(paneText(second.snapshot), "READY")
	})
	if first.snapshot.Exited || second.snapshot.Exited {
		t.Fatal("backgrounding stopped a shell")
	}
	appKey(a, 15, experience.KeyTab, experience.ModControl)
	if a.pane() != first || a.tabReveal != 0 {
		t.Fatalf("tab switch did not restart transition: active=%d phase=%f", a.active, a.tabReveal)
	}
	if err := a.Update(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if a.tabReveal <= 0 || a.tabReveal >= 1 {
		t.Fatalf("tab transition skipped intermediate state: %f", a.tabReveal)
	}
	for i := 0; i < 50; i++ {
		if err := a.Update(16 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if a.tabReveal != 1 {
		t.Fatalf("tab transition never settled: %f", a.tabReveal)
	}
	if err := first.Paste("done\n"); err != nil {
		t.Fatal(err)
	}
	waitApp(t, a, func() bool { return first.snapshot.Exited })
	if !strings.Contains(paneText(first.snapshot), "FINISH:done") {
		t.Fatal("resuming a background tab lost its shell input")
	}
	appKey(a, 17, experience.Key("W"), experience.ModControl|experience.ModShift)
	if len(a.tabs) != 1 || a.pane() != second || second.snapshot.Exited {
		t.Fatal("closing one tab disrupted the surviving shell")
	}
	appKey(a, 20, experience.Key("T"), experience.ModControl|experience.ModShift)
	if len(a.tabs) != 2 || a.tabs[1].id <= second.id {
		t.Fatal("new session reused an old identity")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if !a.closed || len(a.tabs) != 0 {
		t.Fatal("application shutdown retained shell sessions")
	}
}

func TestAppearanceKeyboardControlsClampAndHonorReducedMotion(t *testing.T) {
	a := testApp(t, "stty -echo; printf READY; IFS= read -r next")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	appKey(a, 51, experience.Key(","), experience.ModControl)
	if !a.settings {
		t.Fatal("Ctrl+, did not open Appearance")
	}
	a.Draw(1100, 720)
	// Tab from Cyan to Amber, then select with Enter.
	appKey(a, 15, experience.KeyTab, 0)
	appKey(a, 28, experience.KeyEnter, 0)
	if a.prefs.Palette != 1 {
		t.Fatalf("keyboard palette selection=%d", a.prefs.Palette)
	}
	appKey(a, 15, experience.KeyTab, 0)
	appKey(a, 15, experience.KeyTab, 0)
	for i := 0; i < 30; i++ {
		appKey(a, 105, experience.KeyLeft, 0)
	}
	if a.prefs.Opacity != .35 {
		t.Fatalf("opacity lower bound=%f", a.prefs.Opacity)
	}
	for i := 0; i < 30; i++ {
		appKey(a, 106, experience.KeyRight, 0)
	}
	if a.prefs.Opacity != 1 {
		t.Fatalf("opacity upper bound=%f", a.prefs.Opacity)
	}
	appKey(a, 15, experience.KeyTab, 0)
	appKey(a, 15, experience.KeyTab, 0)
	appKey(a, 57, experience.KeySpace, 0)
	if a.prefs.Motion {
		t.Fatal("keyboard Motion toggle failed")
	}
	if err := a.Update(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if a.drawer != 1 || a.opacity != a.prefs.Opacity || a.accent != palette(a.prefs.Palette) {
		t.Fatal("reduced motion did not settle appearance immediately")
	}
	appKey(a, 1, experience.KeyEscape, 0)
	if err := a.Update(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if a.drawer != 0 {
		t.Fatal("reduced motion left closing drawer animation")
	}
	for i := 0; i < 30; i++ {
		appKey(a, 13, experience.Key("="), experience.ModControl)
	}
	if a.prefs.FontSize != 24 {
		t.Fatalf("font upper bound=%f", a.prefs.FontSize)
	}
	for i := 0; i < 30; i++ {
		appKey(a, 12, experience.Key("-"), experience.ModControl)
	}
	if a.prefs.FontSize != 11 {
		t.Fatalf("font lower bound=%f", a.prefs.FontSize)
	}
	appKey(a, 11, experience.Key("0"), experience.ModControl)
	if a.prefs.FontSize != 15 || !a.prefs.Valid() {
		t.Fatalf("font reset/preferences=%+v", a.prefs)
	}
}

func TestPreferencesRejectNonfiniteAndOutOfRangeValues(t *testing.T) {
	if !DefaultPreferences().Valid() {
		t.Fatal("default preferences invalid")
	}
	for _, edit := range []func(*Preferences){
		func(p *Preferences) { p.Opacity = float32(math.NaN()) },
		func(p *Preferences) { p.Glow = float32(math.Inf(1)) },
		func(p *Preferences) { p.FontSize = float32(math.Inf(-1)) },
		func(p *Preferences) { p.Opacity = .34 },
		func(p *Preferences) { p.Opacity = 1.01 },
		func(p *Preferences) { p.Glow = -.01 },
		func(p *Preferences) { p.FontSize = 25 },
		func(p *Preferences) { p.Palette = 3 },
		func(p *Preferences) { p.Palette = -1 },
	} {
		prefs := DefaultPreferences()
		edit(&prefs)
		if prefs.Valid() {
			t.Fatalf("accepted invalid appearance: %+v", prefs)
		}
		if app, err := New(terminal.Options{Command: "/does/not/exist"}, prefs); err == nil || !strings.Contains(err.Error(), "preferences") || app != nil {
			t.Fatalf("invalid appearance reached process launch: app=%v err=%v", app, err)
		}
	}
}

func TestClosingAppearanceRestoresImmediatePaste(t *testing.T) {
	a := testApp(t, "stty raw -echo; printf READY; dd bs=1 count=1 2>/dev/null | od -An -tx1")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	appKey(a, 51, experience.Key(","), experience.ModControl)
	appKey(a, 1, experience.KeyEscape, 0)
	// Clipboard completion invokes Paste directly; it must work before any
	// ordinary key or content click has a chance to restore PTY focus.
	if err := a.pane().Paste("v"); err != nil {
		t.Fatal(err)
	}
	waitApp(t, a, func() bool { return a.pane().snapshot.Exited })
	bytes := strings.Join(strings.Fields(strings.TrimPrefix(paneText(a.pane().snapshot), "READY")), " ")
	if bytes != "76" {
		t.Fatalf("paste after appearance close: %q", bytes)
	}
}

func TestApplicationHighDPISizingAndPointerSelection(t *testing.T) {
	a := testApp(t, "stty -echo; printf READY; IFS= read -r next")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	cols, rows := a.pane().snapshot.Cols, a.pane().snapshot.Rows
	a.SetScale(1.75)
	a.Draw(1925, 1260)
	if a.pane().snapshot.Cols != cols || a.pane().snapshot.Rows != rows {
		t.Fatalf("same logical extent changed cells at 175%%: %dx%d -> %dx%d", cols, rows, a.pane().snapshot.Cols, a.pane().snapshot.Rows)
	}
	for _, e := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: (a.body.x + .1*a.cellW) * a.unit, Y: (a.body.y + .4*a.cellH) * a.unit},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: (a.body.x + 4.1*a.cellW) * a.unit, Y: (a.body.y + .4*a.cellH) * a.unit},
	} {
		a.Handle(e)
	}
	if text, ok := a.pane().Copy(); !ok || text != "READY" {
		t.Fatalf("175%% pointer selected wrong cells: %q %v", text, ok)
	}
	a.Draw(1400, 1050)
	if a.pane().snapshot.Cols >= cols || a.pane().snapshot.Rows >= rows {
		t.Fatal("native resize did not reduce PTY geometry")
	}
	if _, selected := a.pane().Copy(); selected {
		t.Fatal("native resize preserved invalid selected cell coordinates")
	}
}
