package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

type closingApplications struct {
	*fakeApplications
	closed []uint64
	check  func(uint64)
}

func (f *closingApplications) CloseApplication(id uint64) {
	if f.check != nil {
		f.check(id)
	}
	f.closed = append(f.closed, id)
}

func TestCloseSelectedRequestsOnlyActiveWindowAfterInputCancellation(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, apps.surfaces[1])
	pointer(w, experience.PointerDown, x, y)
	// Leave pointer capture and a modifier held when the close button is used.
	w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 29, Pressed: true})
	if !w.OwnsKeyboard() || w.m.applicationState.Selected != 3 {
		t.Fatal("test did not select both windows and focus the active one")
	}
	before, historyPosition, historyLen := w.Document(), w.historyPosition, len(w.history)
	eventsBefore := len(apps.events)
	closer.check = func(id uint64) {
		if id != 701 || w.OwnsKeyboard() || w.applicationCapturedID != 0 || len(w.applicationButtons) != 0 || w.pointer.kind != captureNone {
			t.Fatalf("close request retained input capture or targeted another window: %d", id)
		}
		keyboardCancelled, pointerCancelled := false, false
		for _, sent := range apps.events[eventsBefore:] {
			if sent.id != id {
				t.Fatalf("close interaction sent input to another window: %+v", sent)
			}
			keyboardCancelled = keyboardCancelled || sent.event.Kind == experience.KeyboardCancel
			pointerCancelled = pointerCancelled || sent.event.Kind == experience.PointerCancel
		}
		if !keyboardCancelled || !pointerCancelled || apps.focus[len(apps.focus)-1] != 0 {
			t.Fatal("close request preceded input cancellation")
		}
	}
	x, y = applicationCloseButton.x+15, applicationCloseButton.y+15
	if !pointer(w, experience.PointerDown, x, y) || len(closer.closed) != 0 {
		t.Fatal("close should wait for an explicit completed click")
	}
	if !pointer(w, experience.PointerUp, x, y) || len(closer.closed) != 1 || closer.closed[0] != 701 {
		t.Fatalf("close did not target only the active window: %v", closer.closed)
	}
	w.Draw(1440, 900)
	if w.Document() != before || w.historyPosition != historyPosition || len(w.history) != historyLen {
		t.Fatal("live close request changed the document or undo history")
	}
	if len(w.applicationSurfaces) != 2 || w.application.ID != 701 {
		t.Fatal("workspace removed a window before its provider accepted close")
	}
	// A legacy client can keep its surface for an unsaved-work dialog. Removal
	// occurs only when that provider finally withdraws it; its sibling stays.
	apps.surfaces = apps.surfaces[:1]
	w.Update(0)
	if len(w.applicationSurfaces) != 1 || w.application.ID != 700 || len(closer.closed) != 1 {
		t.Fatal("provider withdrawal did not preserve the other window")
	}
}

func TestCloseSelectedCancelledClicksNeverCloseAnotherWindow(t *testing.T) {
	for _, action := range []string{"release-outside", "pointer-cancel", "keyboard-cancel", "selection-changed", "surface-withdrawn"} {
		t.Run(action, func(t *testing.T) {
			w, apps := multipleApplications(t, 2)
			closer := &closingApplications{fakeApplications: apps}
			w.SetApplications(closer)
			x, y := applicationCloseButton.x+15, applicationCloseButton.y+15
			pointer(w, experience.PointerDown, x, y)
			switch action {
			case "release-outside":
				x += applicationCloseButton.w
			case "pointer-cancel":
				w.Handle(experience.Event{Kind: experience.PointerCancel})
			case "keyboard-cancel":
				w.Handle(experience.Event{Kind: experience.KeyboardCancel})
			case "selection-changed":
				command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
			case "surface-withdrawn":
				apps.surfaces = apps.surfaces[1:]
			}
			pointer(w, experience.PointerUp, x, y)
			if len(closer.closed) != 0 {
				t.Fatalf("cancelled click requested close: %v", closer.closed)
			}
		})
	}
}
