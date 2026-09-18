package workspace

import (
	"errors"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func terminalChord(code uint32) experience.Event {
	return experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true, Modifiers: experience.ModControl | experience.ModAlt}
}

func TestTerminalShortcutRevokesAppInputAndConsumesWholeKeystroke(t *testing.T) {
	for _, appID := range []string{"native:terminal", "legacy:foot"} {
		for _, code := range []uint32{28, 96} {
			t.Run(appID+map[uint32]string{28: "/enter", 96: "/keypad-enter"}[code], func(t *testing.T) {
				w, apps := multipleApplications(t, 1)
				apps.surfaces[0].AppID = appID
				launcher := terminalLauncher(t, apps)
				w.SetApplications(launcher)
				command(t, w, Action{Kind: ToggleApplicationReading})
				w.Draw(1440, 900)
				x, y := visibleApplicationPoint(t, w)
				pointer(w, experience.PointerDown, x, y)
				for _, modifier := range []uint32{29, 56} {
					w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: modifier, Pressed: true})
				}
				if !w.OwnsKeyboard() || w.applicationCapturedID == 0 {
					t.Fatal("fixture did not start with keyboard and pointer held by the app")
				}
				before, historyPosition, eventCount := w.Document(), w.historyPosition, len(apps.events)
				if !w.Handle(terminalChord(code)) || len(launcher.launched) != 1 || launcher.launched[0] != "terminal" {
					t.Fatal("focused application blocked the global terminal shortcut")
				}
				if w.OwnsKeyboard() || w.applicationCapturedID != 0 || len(w.applicationButtons) != 0 || apps.focus[len(apps.focus)-1] != 0 {
					t.Fatal("terminal shortcut left app keyboard or pointer input active")
				}
				if w.application.ID != launcher.surface.ID || !w.m.applicationReading || w.Document().View.Camera != before.View.Camera || w.historyPosition != historyPosition {
					t.Fatal("shortcut failed to select new terminal or changed the view/history")
				}
				keyboardCancels, pointerCancels := 0, 0
				for _, sent := range apps.events[eventCount:] {
					switch sent.event.Kind {
					case experience.KeyboardCancel:
						keyboardCancels++
					case experience.PointerCancel:
						pointerCancels++
					default:
						t.Fatalf("launch chord leaked to app: %+v", sent)
					}
				}
				if keyboardCancels != 1 || pointerCancels != 1 {
					t.Fatalf("held input cancellation: keyboard=%d pointer=%d", keyboardCancels, pointerCancels)
				}
				// The user can click the new terminal before releasing Enter.
				w.Draw(1440, 900)
				x, y = visibleApplicationPoint(t, w)
				pointer(w, experience.PointerDown, x, y)
				pointer(w, experience.PointerUp, x, y)
				if !w.OwnsKeyboard() {
					t.Fatal("explicit click did not restore typing")
				}
				eventCount = len(apps.events)
				for _, repeat := range []bool{true, false, true} {
					event := experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true, Repeat: repeat}
					if !w.Handle(event) || len(launcher.launched) != 1 || len(apps.events) != eventCount {
						t.Fatal("held shortcut leaked a repeat/duplicate press or relaunched")
					}
				}
				release := experience.Event{Kind: experience.KeyInput, Keycode: code}
				if !w.Handle(release) || len(apps.events) != eventCount {
					t.Fatal("shortcut release leaked after its modifiers changed")
				}
				release.Pressed = true
				if !w.Handle(release) || len(apps.events) != eventCount+1 || apps.events[eventCount].event != release {
					t.Fatal("ordinary Enter did not resume after shortcut release")
				}
			})
		}
	}
}

func TestTerminalShortcutPreservesOrdinaryAppEnter(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	before := w.Document()
	for _, code := range []uint32{28, 96} {
		for _, mods := range []experience.Modifiers{0, experience.ModControl, experience.ModAlt, experience.ModControl | experience.ModAlt | experience.ModShift, experience.ModControl | experience.ModAlt | experience.ModSuper} {
			for _, pressed := range []bool{true, false} {
				event := experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: pressed, Modifiers: mods}
				count := len(apps.events)
				if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].event != event {
					t.Fatalf("ordinary app Enter was intercepted: %+v", event)
				}
			}
		}
		// Pressing modifiers after a normal Enter must not swallow that normal
		// key's release: only a shortcut's own press acquires suppression.
		event := terminalChord(code)
		event.Pressed = false
		count := len(apps.events)
		if !w.Handle(event) || len(apps.events) != count+1 || apps.events[count].event != event {
			t.Fatal("unmatched shortcut release stole an ordinary key-up")
		}
	}
	if len(launcher.launched) != 0 || w.Document() != before || !w.OwnsKeyboard() {
		t.Fatal("ordinary Enter changed the workspace")
	}
}

func TestTerminalShortcutCancelAndRepeatWithoutInitialPress(t *testing.T) {
	w, apps := multipleApplications(t, 0)
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	repeat := terminalChord(28)
	repeat.Repeat = true
	if !w.Handle(repeat) || len(launcher.launched) != 0 {
		t.Fatal("an orphaned repeat started a terminal")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if w.terminalShortcutKeys != 0 {
		t.Fatal("keyboard cancellation retained shortcut suppression")
	}
	// A fresh shortcut works even with no existing app. It also cancels a
	// pending New Terminal pointer click so its later release cannot launch twice.
	x, y := applicationLaunchButton.x+15, applicationLaunchButton.y+15
	pointer(w, experience.PointerDown, x, y)
	if !w.Handle(terminalChord(28)) || len(launcher.launched) != 1 || w.pointer.kind != captureNone || w.OwnsKeyboard() {
		t.Fatal("fresh shortcut did not launch from an empty workspace")
	}
	pointer(w, experience.PointerUp, x, y)
	if len(launcher.launched) != 1 {
		t.Fatal("cancelled launch-button release started another terminal")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	w.Draw(1440, 900)
	x, y = visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	count := len(apps.events)
	plain := experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true}
	if !w.Handle(plain) || len(apps.events) != count+1 || apps.events[count].event != plain {
		t.Fatal("focus loss left ordinary Enter suppressed")
	}
}

func TestTerminalShortcutLaunchFailureUsesExistingNotice(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	launcher := terminalLauncher(t, apps)
	launcher.err = errors.New("fixture shell unavailable")
	w.SetApplications(launcher)
	before, history := w.Document(), w.historyPosition
	chord := terminalChord(28)
	if !w.Handle(chord) || len(launcher.launched) != 1 || !strings.Contains(w.applicationNotice, "fixture shell unavailable") {
		t.Fatal("shortcut launch failure did not show the existing notice")
	}
	assertApplicationNoticeVisible(t, w)
	chord.Repeat = true
	w.Handle(chord)
	if len(launcher.launched) != 1 || w.Document() != before || w.historyPosition != history || w.OwnsKeyboard() {
		t.Fatal("failed shortcut launch repeated, changed layout, or captured typing")
	}
	w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 28})
	if !w.Handle(terminalChord(28)) || len(launcher.launched) != 2 {
		t.Fatal("a deliberate retry did not work after shortcut release")
	}
}

func TestTerminalShortcutTracksBothEnterKeysIndependently(t *testing.T) {
	w, apps := multipleApplications(t, 0)
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	w.Handle(terminalChord(28))
	launcher.surface.ID++
	launcher.surface.Key += "-second"
	w.Handle(terminalChord(96))
	if len(launcher.launched) != 2 || w.terminalShortcutKeys != 3 {
		t.Fatal("separate physical Enter presses shared shortcut ownership")
	}
	for _, code := range []uint32{28, 96} {
		if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code}) {
			t.Fatal("second physical Enter overwrote first key's release suppression")
		}
	}
	if w.terminalShortcutKeys != 0 {
		t.Fatal("released Enter keys remained suppressed")
	}
}
