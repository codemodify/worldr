package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func helpKeyEvent(code uint32, key experience.Key, pressed bool) experience.Event {
	return experience.Event{Kind: experience.KeyInput, Keycode: code, Key: key, Pressed: pressed}
}

func TestHelpIsTransientAndConsumesHeldKeysThroughDismissal(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	before := w.Document()
	f1 := helpKeyEvent(59, experience.KeyF1, true)
	if !w.Handle(f1) || !w.helpOpen {
		t.Fatal("workspace F1 did not open help")
	}
	w.Handle(helpKeyEvent(59, experience.KeyF1, false))
	w.Draw(1440, 900)
	for _, event := range []experience.Event{
		helpKeyEvent(18, experience.KeyE, true),
		helpKeyEvent(57, experience.KeySpace, true),
		{Kind: experience.PointerDown, X: 600, Y: 400, Button: experience.ButtonPrimary},
		{Kind: experience.PointerMove, X: 700, Y: 440},
		{Kind: experience.PointerUp, X: 700, Y: 440, Button: experience.ButtonPrimary},
		{Kind: experience.PointerScroll, X: 600, Y: 400, ScrollY: 10},
	} {
		if !w.Handle(event) {
			t.Fatalf("help did not consume %+v", event)
		}
	}
	if w.Document() != before || w.CanUndo() {
		t.Fatal("help input changed the document or history")
	}
	w.Handle(helpKeyEvent(1, experience.KeyEscape, true))
	if w.helpOpen || w.Document() != before {
		t.Fatal("Escape did not just dismiss help")
	}
	for _, code := range []uint32{1, 18, 57} {
		if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: true, Repeat: true}) {
			t.Fatal("held key escaped after help dismissal")
		}
		if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code}) {
			t.Fatal("matching release escaped after help dismissal")
		}
	}
	if w.Document() != before {
		t.Fatal("held commands changed the underlying document")
	}
	w.Handle(helpKeyEvent(18, experience.KeyE, true))
	if !w.State().Exploded {
		t.Fatal("fresh workspace input stayed trapped after help")
	}
}

func TestHelpPreservesAppF1AndDoesNotRestoreTypingFocus(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	apps.events = nil
	f1 := helpKeyEvent(59, experience.KeyF1, true)
	w.Handle(f1)
	if w.helpOpen || len(apps.events) != 1 || apps.events[0].event != f1 {
		t.Fatal("workspace stole F1 from the focused app")
	}
	w.Handle(helpKeyEvent(59, experience.KeyF1, false))
	before := w.Document()
	pointer(w, experience.PointerDown, helpButton.x+20, helpButton.y+10)
	pointer(w, experience.PointerUp, helpButton.x+20, helpButton.y+10)
	if !w.helpOpen || w.OwnsKeyboard() || w.applicationHoveredID != 0 || w.applicationCapturedID != 0 {
		t.Fatal("help did not release app input/cursor ownership")
	}
	apps.events = nil
	// Clicks and Enter cannot activate the app behind the modal.
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.Handle(helpKeyEvent(28, experience.KeyUnknown, true))
	w.Handle(helpKeyEvent(28, experience.KeyUnknown, false))
	pointer(w, experience.PointerDown, helpCloseButton.x+20, helpCloseButton.y+10)
	pointer(w, experience.PointerUp, helpCloseButton.x+20, helpCloseButton.y+10)
	if w.helpOpen || w.OwnsKeyboard() || len(apps.events) != 0 || w.Document() != before {
		t.Fatal("modal dismissal leaked input, refocused typing, or changed layout")
	}
}

func TestHelpControlScaledDragCancelAndResize(t *testing.T) {
	w := study(t)
	w.Draw(2880, 1800)
	x, y := (helpButton.x+20)*2, (helpButton.y+10)*2
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+16, y)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.helpOpen {
		t.Fatal("dragging away and back activated help")
	}
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if w.helpOpen {
		t.Fatal("cancelled help click activated")
	}
	pointer(w, experience.PointerDown, x, y)
	w.Draw(1440, 900)
	pointer(w, experience.PointerUp, x/2, y/2)
	if w.helpOpen {
		t.Fatal("resize turned an old press into a new help click")
	}
	w.Draw(2880, 1800)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if !w.helpOpen {
		t.Fatal("scaled completed help click missed")
	}
	w.Handle(helpKeyEvent(18, experience.KeyE, true))
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	w.Handle(helpKeyEvent(1, experience.KeyEscape, true))
	w.Handle(helpKeyEvent(1, experience.KeyEscape, false))
	w.Handle(helpKeyEvent(18, experience.KeyE, true))
	if !w.State().Exploded {
		t.Fatal("focus loss did not release help's remembered keys")
	}
}

func dismissHelpByKeyboard(t *testing.T, w *Workspace) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		if !w.Handle(helpKeyEvent(1, experience.KeyEscape, pressed)) {
			t.Fatal("help Escape stroke was not consumed")
		}
	}
	if w.helpOpen {
		t.Fatal("Escape did not dismiss help")
	}
}

func TestHelpDrainsAllMouseButtonsAcrossKeyboardDismissalAndRefocus(t *testing.T) {
	w, apps := applicationStudy(t)
	w.Handle(helpKeyEvent(59, experience.KeyF1, true))
	w.Handle(helpKeyEvent(59, experience.KeyF1, false))
	x, y := visibleApplicationPoint(t, w)
	for _, code := range []uint32{272, 273, 274, 275} {
		if !w.Handle(experience.Event{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: code}) {
			t.Fatal("modal pointer down escaped")
		}
	}
	dismissHelpByKeyboard(t, w)
	tapOverviewKey(t, w, 28) // Explicit focus is allowed after help closes.
	w.Draw(1440, 900)
	x, y = visibleApplicationPoint(t, w)
	apps.events = nil
	before := w.Document()
	for _, event := range []experience.Event{
		{Kind: experience.PointerMove, X: x + 20, Y: y + 10},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 10},
		{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: 272}, // Duplicate hold.
	} {
		if !w.Handle(event) {
			t.Fatalf("held modal pointer event escaped after dismissal: %+v", event)
		}
	}
	for _, code := range []uint32{272, 273, 274, 275} {
		if !w.Handle(experience.Event{Kind: experience.PointerUp, X: x, Y: y, ButtonCode: code}) {
			t.Fatal("modal button release escaped after focus changed")
		}
	}
	if len(apps.events) != 0 || w.Document() != before || len(w.helpButtons) != 0 {
		t.Fatal("draining modal buttons changed application or workspace state")
	}
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if len(apps.events) != 2 {
		t.Fatal("fresh application click remained suppressed after modal releases")
	}
}

func TestHelpAdoptsKeysAndPointerGestureAlreadyHeldWhenOpened(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	letter := helpKeyEvent(30, experience.KeyUnknown, true)
	w.Handle(letter)
	// A second button can be held in an app while primary opens the guide.
	w.Handle(experience.Event{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: 273})
	pointer(w, experience.PointerDown, helpButton.x+20, helpButton.y+10)
	pointer(w, experience.PointerUp, helpButton.x+20, helpButton.y+10)
	if !w.helpOpen || w.OwnsKeyboard() || w.applicationCapturedID != 0 {
		t.Fatal("help did not cancel the previous application stroke")
	}
	dismissHelpByKeyboard(t, w)
	tapOverviewKey(t, w, 28)
	apps.events = nil
	letter.Repeat = true
	w.Handle(letter)
	letter.Pressed, letter.Repeat = false, false
	w.Handle(letter)
	w.Handle(experience.Event{Kind: experience.PointerUp, X: x, Y: y, ButtonCode: 273})
	if len(apps.events) != 0 {
		t.Fatal("a stroke begun before help resumed in the refocused application")
	}
	letter.Pressed = true
	w.Handle(letter)
	if len(apps.events) != 1 || apps.events[0].event != letter {
		t.Fatal("fresh application key stayed trapped after its old release")
	}

	native := study(t)
	native.Draw(1440, 900)
	before := native.Document()
	pointer(native, experience.PointerDown, 700, 400)
	pointer(native, experience.PointerMove, 735, 430)
	if native.Document() == before {
		t.Fatal("fixture did not start a workspace gesture")
	}
	native.Handle(helpKeyEvent(59, experience.KeyF1, true))
	native.Handle(helpKeyEvent(59, experience.KeyF1, false))
	if native.Document() != before || native.pointer.kind != captureNone {
		t.Fatal("opening help did not roll back the in-flight gesture")
	}
	dismissHelpByKeyboard(t, native)
	pointer(native, experience.PointerMove, 780, 470)
	if !native.Handle(experience.Event{Kind: experience.PointerUp, X: 780, Y: 470, Button: experience.ButtonPrimary}) || native.Document() != before || native.CanUndo() {
		t.Fatal("cancelled pre-modal gesture resumed or committed after dismissal")
	}
}

func TestHelpPreservesAndReleasesPreviouslyReservedCommandStrokes(t *testing.T) {
	for _, kind := range []string{"overview-enter", "terminal-enter", "overview-chord"} {
		t.Run(kind, func(t *testing.T) {
			w, apps := multipleApplications(t, 1)
			launcher := terminalLauncher(t, apps)
			w.SetApplications(launcher)
			held := overviewKeyEvent(28, true)
			switch kind {
			case "overview-enter":
				command(t, w, Action{Kind: ToggleApplicationOverview})
			case "terminal-enter":
				held = terminalChord(28)
			case "overview-chord":
				held = helpKeyEvent(24, experience.KeyO, true)
				held.Modifiers = experience.ModControl | experience.ModAlt
			}
			w.Handle(held)
			before, launches := w.Document(), len(launcher.launched)
			w.Handle(helpKeyEvent(59, experience.KeyF1, true))
			w.Handle(helpKeyEvent(59, experience.KeyF1, false))
			dismissHelpByKeyboard(t, w)
			held.Repeat, held.Modifiers = true, 0
			if !w.Handle(held) || w.OwnsKeyboard() || w.Document() != before || len(launcher.launched) != launches {
				t.Fatal("previously reserved command resumed after help")
			}
			held.Pressed, held.Repeat = false, false
			if !w.Handle(held) || w.overviewNavigationKeys != 0 || w.terminalShortcutKeys != 0 || w.overviewShortcutHeld {
				t.Fatal("modal release did not drain the earlier command tracker")
			}
			if kind == "overview-chord" {
				tapOverviewKey(t, w, 28)
			}
			tapOverviewKey(t, w, 28)
			if !w.OwnsKeyboard() {
				t.Fatal("fresh focus command stayed trapped by a stale shortcut tracker")
			}
		})
	}
}

func TestHelpResizeAndKeyboardDismissKeepPendingClickDrained(t *testing.T) {
	for _, resize := range []bool{false, true} {
		w := study(t)
		w.Draw(1440, 900)
		w.Handle(helpKeyEvent(59, experience.KeyF1, true))
		w.Handle(helpKeyEvent(59, experience.KeyF1, false))
		pointer(w, experience.PointerDown, helpCloseButton.x+20, helpCloseButton.y+10)
		if resize {
			w.Draw(2880, 1800)
			pointer(w, experience.PointerMove, 1200, 800)
		}
		dismissHelpByKeyboard(t, w)
		before := w.Document()
		for _, event := range []experience.Event{
			{Kind: experience.PointerMove, X: 1200, Y: 800},
			{Kind: experience.PointerUp, X: 1200, Y: 800, Button: experience.ButtonPrimary},
		} {
			if !w.Handle(event) {
				t.Fatal("dismissed/resized close-control stroke escaped")
			}
		}
		if w.helpOpen || w.Document() != before || w.CanUndo() {
			t.Fatal("old help close click changed the workspace")
		}
	}
}

func TestAbortedHelpClickStillDrainsPreviouslyCancelledAppInput(t *testing.T) {
	w, apps := applicationStudy(t)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	w.Handle(helpKeyEvent(30, experience.KeyUnknown, true))
	w.Handle(experience.Event{Kind: experience.PointerDown, X: x, Y: y, ButtonCode: 273})
	hx, hy := helpButton.x+20, helpButton.y+10
	pointer(w, experience.PointerDown, hx, hy)
	pointer(w, experience.PointerMove, hx+20, hy)
	pointer(w, experience.PointerUp, hx+20, hy)
	if w.helpOpen || w.OwnsKeyboard() || w.applicationCapturedID != 0 {
		t.Fatal("aborted guide click did not leave cancelled app input released")
	}
	tapOverviewKey(t, w, 28)
	apps.events = nil
	for _, event := range []experience.Event{
		{Kind: experience.KeyInput, Keycode: 30, Pressed: true, Repeat: true},
		{Kind: experience.KeyInput, Keycode: 30},
		{Kind: experience.PointerMove, X: x, Y: y},
		{Kind: experience.PointerUp, X: x, Y: y, ButtonCode: 273},
	} {
		if !w.Handle(event) {
			t.Fatalf("aborted guide click leaked old input: %+v", event)
		}
	}
	if len(apps.events) != 0 || len(w.helpButtons) != 0 {
		t.Fatal("previously cancelled app stroke resumed after aborted help click")
	}
}
