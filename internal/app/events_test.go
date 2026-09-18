package app

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestPlatformEventsUseSemanticKeys(t *testing.T) {
	for _, code := range []uint32{'z', 'Z'} {
		e := hostEvent(host.Event{Kind: host.Key, Code: code, Modifiers: 3, Pressed: true})
		if e.Key != experience.KeyZ || e.Modifiers != experience.ModControl|experience.ModShift {
			t.Fatalf("lost undo/redo chord: %+v", e)
		}
	}
	e := hostEvent(host.Event{Kind: host.Cancel})
	if e.Kind != experience.PointerCancel {
		t.Fatal("focus loss must cancel capture")
	}
	for _, code := range []uint32{'p', 'P'} {
		if hostEvent(host.Event{Kind: host.Key, Code: code}).Key != experience.KeyP {
			t.Fatal("host lost presentation shortcut")
		}
	}
	for _, code := range []uint32{'g', 'G'} {
		if hostEvent(host.Event{Kind: host.Key, Code: code}).Key != experience.KeyG {
			t.Fatal("host lost portal shortcut")
		}
	}
	var direct keyState
	if direct.event(25, true).Key != experience.KeyP {
		t.Fatal("direct-display input lost presentation shortcut")
	}
	if direct.event(34, true).Key != experience.KeyG {
		t.Fatal("direct-display input lost portal shortcut")
	}
}

func TestApplicationInputRetainsRawKeyAndModifierState(t *testing.T) {
	raw := host.Event{Kind: host.Key, Code: 'a', Keycode: 30, Time: 912, Pressed: true,
		Modifiers: 1, Depressed: 4, Latched: 2, Locked: 16, Group: 1}
	e := hostEvent(raw)
	if e.Kind != experience.KeyInput || e.Key != experience.KeyUnknown || e.Keycode != 30 ||
		e.Time != 912 || !e.Pressed || e.Depressed != 4 || e.Latched != 2 || e.Locked != 16 || e.Group != 1 {
		t.Fatalf("unsupported semantic key lost application input: %+v", e)
	}
	for _, pressed := range []bool{false, true} {
		raw.Pressed = pressed
		if hostEvent(raw).Pressed != pressed {
			t.Fatal("lost key edge")
		}
	}
	if e := hostEvent(host.Event{Kind: host.ModifiersChanged, Locked: 2}); e.Kind != experience.KeyboardModifiers || e.Depressed != 0 || e.Locked != 2 {
		t.Fatalf("modifier-only release lost: %+v", e)
	}
	if e := hostEvent(host.Event{Kind: host.KeyboardCancel}); e.Kind != experience.KeyboardCancel {
		t.Fatal("keyboard focus loss became pointer cancellation")
	}
	if e := hostEvent(host.Event{Kind: host.KeymapChanged, Keymap: "xkb_keymap {...}"}); e.Kind != experience.KeymapChanged || e.Keymap == "" {
		t.Fatal("lost keyboard map")
	}
	if e := hostEvent(host.Event{Kind: host.RepeatInfo, RepeatRate: 25, RepeatDelay: 600}); e.Kind != experience.KeyboardRepeatInfo || e.RepeatRate != 25 || e.RepeatDelay != 600 || e.Repeat {
		t.Fatalf("lost repeat policy or synthesized repeated key: %+v", e)
	}
}

func TestApplicationInputRetainsButtonsAndScroll(t *testing.T) {
	for code, button := range map[uint32]experience.Button{
		0x110: experience.ButtonPrimary, 0x111: experience.ButtonSecondary,
		0x112: experience.ButtonMiddle, 0x113: experience.ButtonNone,
	} {
		for kind, want := range map[host.Kind]experience.EventKind{host.Down: experience.PointerDown, host.Up: experience.PointerUp} {
			e := hostEvent(host.Event{Kind: kind, ButtonCode: code, Time: 78, X: 90, Y: 112})
			if e.Kind != want || e.ButtonCode != code || e.Button != button || e.Time != 78 || e.X != 90 || e.Y != 112 {
				t.Fatalf("lost pointer button: %+v", e)
			}
		}
	}
	e := hostEvent(host.Event{Kind: host.Scroll, ScrollX: -2.5, ScrollY: 10, Time: 91, X: 100, Y: 250})
	if e.Kind != experience.PointerScroll || e.ScrollX != -2.5 || e.ScrollY != 10 || e.Time != 91 || e.X != 100 || e.Y != 250 {
		t.Fatalf("lost axis input: %+v", e)
	}
}

func TestDirectInputRetainsUnknownKeysAndUSModifierMasks(t *testing.T) {
	var state keyState
	state.event(29, true)
	state.event(42, true)
	state.event(56, true)
	state.event(125, true)
	state.event(58, true)
	state.event(58, false)
	e := state.event(30, true)
	if e.Key != experience.KeyUnknown || e.Keycode != 30 || !e.Pressed || e.Depressed != 1|4|8|64 || e.Locked != 2 {
		t.Fatalf("direct key lost physical code or US masks: %+v", e)
	}
	for _, code := range []uint32{29, 42, 56, 125} {
		e = state.event(code, false)
	}
	if e.Depressed != 0 || e.Modifiers != 0 || e.Locked != 2 {
		t.Fatalf("direct modifier release stuck: %+v", e)
	}
	state.event(58, true)
	if e := state.event(58, true); e.Locked != 0 {
		t.Fatal("duplicate press toggled Caps Lock twice")
	}
}

func TestFirstInputUsesActualRenderExtent(t *testing.T) {
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	prepareInputLayout(work, 2880, 1800)
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		work.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: 200, Y: 674})
	}
	if got := work.Document().Selection; got != workspace.Housing {
		t.Fatalf("first HiDPI click selected %s instead of housing", got)
	}
}

func TestResizeCancelsPendingGestureBeforeNewCoordinates(t *testing.T) {
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	prepareInputLayout(work, 1440, 900)
	before := work.Document()
	work.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 700, Y: 400})
	work.Handle(experience.Event{Kind: experience.PointerMove, X: 850, Y: 450})
	if work.Document().View.Camera == before.View.Camera {
		t.Fatal("test did not start an orbit")
	}
	prepareInputLayout(work, 2880, 1800)
	work.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 1700, Y: 900})
	if work.Document() != before || work.CanUndo() {
		t.Fatal("resize committed an interrupted gesture")
	}
}

func TestHostSaveCancelsUncommittedPreview(t *testing.T) {
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	prepareInputLayout(work, 1440, 900)
	before := work.Document()
	work.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 700, Y: 400})
	work.Handle(experience.Event{Kind: experience.PointerMove, X: 850, Y: 450})
	path := filepath.Join(t.TempDir(), "study.json")
	quit, err := dispatchEvent(work, experience.Event{Kind: experience.KeyInput, Key: experience.KeyS, Modifiers: experience.ModControl, Pressed: true}, path, io.Discard)
	if err != nil || quit {
		t.Fatalf("save shortcut: quit=%v err=%v", quit, err)
	}
	reloaded, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if err := loadState(path, reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.Document() != before || work.Document() != before {
		t.Fatal("save serialized a gesture preview that cancellation would discard")
	}
	work.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 850, Y: 450})
	if work.CanUndo() {
		t.Fatal("release after save revived canceled gesture")
	}
}

func TestHostQuitRequiresCurrentControlAndPress(t *testing.T) {
	var s keyState
	if isHostShortcut(s.event(16, true), experience.KeyQ) {
		t.Fatal("bare Q quit")
	}
	s.event(29, true)
	if !isHostShortcut(s.event(16, true), experience.KeyQ) {
		t.Fatal("Ctrl Q ignored")
	}
	if isHostShortcut(s.event(16, false), experience.KeyQ) {
		t.Fatal("release quit")
	}
	s.event(29, false)
	if isHostShortcut(s.event(16, true), experience.KeyQ) {
		t.Fatal("released modifier stuck")
	}
	s.event(97, true)
	if !isHostShortcut(s.event(16, true), experience.KeyQ) {
		t.Fatal("right Ctrl ignored")
	}
	s.event(56, true)
	if isHostShortcut(s.event(16, true), experience.KeyQ) {
		t.Fatal("different chord quit")
	}
}
