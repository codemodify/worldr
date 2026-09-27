package app

import (
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/plasma"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestPlatformEventsNormalizePlaygroundKeys(t *testing.T) {
	for _, tc := range []struct {
		symbol, code uint32
		key          experience.Key
	}{
		{'m', 50, experience.KeyM}, {'M', 50, experience.KeyM},
		{0xff0d, 28, experience.KeyEnter}, {0xff8d, 96, experience.KeyEnter},
		{0xff09, 15, experience.KeyTab}, {0xfe20, 15, experience.KeyTab},
		{0xff52, 103, experience.KeyUp}, {0xff54, 108, experience.KeyDown},
	} {
		var direct keyState
		for _, pressed := range []bool{true, false} {
			nested := hostEvent(host.Event{Kind: host.Key, Code: tc.symbol, Keycode: tc.code, Pressed: pressed, Time: 192})
			for _, e := range []experience.Event{nested, direct.event(tc.code, pressed)} {
				if e.Kind != experience.KeyInput || e.Key != tc.key || e.Keycode != tc.code || e.Pressed != pressed {
					t.Fatalf("symbol %x / evdev %d did not preserve semantic key and raw edge: %+v", tc.symbol, tc.code, e)
				}
			}
			if nested.Time != 192 {
				t.Fatal("semantic normalization lost the native timestamp")
			}
		}
	}
}

func TestPlatformEventsNormalizeNavigatorKeysAndModifierRelease(t *testing.T) {
	for _, tc := range []struct {
		lower, upper, code uint32
		key                experience.Key
	}{
		{'h', 'H', 35, experience.KeyH},
		{'j', 'J', 36, experience.KeyJ},
		{'k', 'K', 37, experience.KeyK},
	} {
		for _, mods := range []experience.Modifiers{0, experience.ModShift, experience.ModControl | experience.ModAlt, experience.ModControl | experience.ModAlt | experience.ModShift} {
			var direct keyState
			for _, modifier := range []struct {
				mask experience.Modifiers
				code uint32
			}{{experience.ModControl, 29}, {experience.ModAlt, 56}, {experience.ModShift, 42}} {
				if mods.Has(modifier.mask) {
					direct.event(modifier.code, true)
				}
			}
			for _, pressed := range []bool{true, false} {
				physical := direct.event(tc.code, pressed)
				for _, symbol := range []uint32{tc.lower, tc.upper} {
					nested := hostEvent(host.Event{Kind: host.Key, Code: symbol, Keycode: tc.code, Modifiers: uint8(mods), Pressed: pressed, Time: 194})
					for _, event := range []experience.Event{nested, physical} {
						if event.Kind != experience.KeyInput || event.Key != tc.key || event.Keycode != tc.code || event.Pressed != pressed || event.Modifiers != mods {
							t.Fatalf("navigator key %s lost semantic identity, edge or modifiers: %+v", tc.key, event)
						}
					}
					if nested.Time != 194 {
						t.Fatal("normalizing navigator keys discarded the native timestamp")
					}
				}
			}
			// Shortcut ownership must not depend on modifiers still being held
			// when the command key comes up.
			direct.event(tc.code, true)
			for _, code := range []uint32{29, 56, 42} {
				direct.event(code, false)
			}
			event := direct.event(tc.code, false)
			if event.Key != tc.key || event.Pressed || event.Modifiers != 0 || event.Depressed != 0 {
				t.Fatalf("navigator key release retained old modifiers: %+v", event)
			}
		}
	}
}

func TestPlaygroundBindingsWorkThroughNativeInputConversion(t *testing.T) {
	for _, path := range []string{"nested", "direct"} {
		t.Run(path, func(t *testing.T) {
			p, err := plasma.New()
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			var direct keyState
			stroke := func(symbol, code uint32) {
				t.Helper()
				for _, pressed := range []bool{true, false} {
					e := hostEvent(host.Event{Kind: host.Key, Code: symbol, Keycode: code, Pressed: pressed})
					if path == "direct" {
						e = direct.event(code, pressed)
					}
					if handled := p.Handle(e); pressed && !handled {
						t.Fatalf("playground did not consume native %s press (%d)", e.Key, code)
					}
				}
			}
			check := func(wantRead bool) {
				t.Helper()
				data, err := p.SaveState()
				if err != nil {
					t.Fatal(err)
				}
				var state struct {
					Read   bool
					Layout struct {
						Motion bool
						Active int
					}
				}
				if err := json.Unmarshal(data, &state); err != nil {
					t.Fatal(err)
				}
				if state.Layout.Motion || state.Layout.Active != 0 || state.Read != wantRead {
					t.Fatalf("native Tab/M/Enter did not select Inbox, disable motion and toggle Read: %+v", state)
				}
			}
			stroke(0xff09, 15)
			stroke('m', 50)
			stroke(0xff0d, 28)
			check(true)
			stroke(0xff8d, 96)
			check(false)
		})
	}
}

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
			t.Fatal("host lost semantic G key mapping")
		}
	}
	for _, code := range []uint32{'c', 'C'} {
		if hostEvent(host.Event{Kind: host.Key, Code: code}).Key != experience.KeyC {
			t.Fatal("host lost close shortcut")
		}
	}
	var direct keyState
	if direct.event(25, true).Key != experience.KeyP {
		t.Fatal("direct-display input lost presentation shortcut")
	}
	if direct.event(34, true).Key != experience.KeyG {
		t.Fatal("direct-display input lost semantic G key mapping")
	}
	if direct.event(46, true).Key != experience.KeyC {
		t.Fatal("direct-display input lost close shortcut")
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
