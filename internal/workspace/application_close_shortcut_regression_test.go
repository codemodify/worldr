package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func closeShortcutRegressionFocus(t *testing.T, w *Workspace, surface experience.ApplicationSurface) {
	t.Helper()
	x, y := visibleApplication(t, w, surface)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.applicationFocusedID != surface.ID || !w.OwnsKeyboard() {
		t.Fatalf("fixture did not focus application %d", surface.ID)
	}
}

func assertOrdinaryCReachesFocusedApplication(t *testing.T, w *Workspace, apps *fakeApplications, id uint64) {
	t.Helper()
	before := len(apps.events)
	down := superC(true, false, 0)
	up := superC(false, false, 0)
	if !w.Handle(down) || !w.Handle(up) {
		t.Fatal("focused application did not consume an ordinary C stroke")
	}
	if len(apps.events) != before+2 || apps.events[before] != (applicationEvent{id: id, event: down}) || apps.events[before+1] != (applicationEvent{id: id, event: up}) {
		t.Fatalf("ordinary C did not reach the focused application unchanged: %+v", apps.events[before:])
	}
}

func TestSuperCReleaseDrainsWhenModalOpensMidStroke(t *testing.T) {
	modalCases := []struct {
		name  string
		open  func(*testing.T, *Workspace)
		close func(*Workspace)
	}{
		{
			name: "command-palette",
			open: func(t *testing.T, w *Workspace) {
				if !w.openCommands() {
					t.Fatal("could not open command palette")
				}
			},
			close: func(w *Workspace) { w.commands.open = false },
		},
		{
			name:  "help",
			open:  func(_ *testing.T, w *Workspace) { w.openHelp() },
			close: func(w *Workspace) { w.helpOpen = false },
		},
		{
			name:  "portal-atlas",
			open:  func(_ *testing.T, w *Workspace) { w.openPortalAtlas() },
			close: func(w *Workspace) { w.portals.open = false },
		},
	}
	for _, tc := range modalCases {
		t.Run(tc.name, func(t *testing.T) {
			w, apps := multipleApplications(t, 2)
			w.desktop = true
			closer := &closingApplications{fakeApplications: apps}
			w.SetApplications(closer)
			closeShortcutRegressionFocus(t, w, apps.surfaces[0])

			if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 {
				t.Fatal("fresh Super+C did not close exactly once")
			}
			tc.open(t, w)
			if !w.Handle(superC(false, false, 0)) {
				t.Fatal("modal did not consume the release of an owned Super+C stroke")
			}
			if w.applicationCloseShortcut.active {
				t.Fatal("modal left the released Super+C stroke armed")
			}
			tc.close(w)
			closeShortcutRegressionFocus(t, w, apps.surfaces[1])
			assertOrdinaryCReachesFocusedApplication(t, w, apps, apps.surfaces[1].ID)
			if len(closer.closed) != 1 {
				t.Fatalf("modal release issued another close: %v", closer.closed)
			}
		})
	}
}

func TestSuperCKeyboardCancelClearsStrokeBehindModal(t *testing.T) {
	modalCases := []struct {
		name  string
		open  func(*testing.T, *Workspace)
		close func(*Workspace)
	}{
		{
			name: "command-palette",
			open: func(t *testing.T, w *Workspace) {
				if !w.openCommands() {
					t.Fatal("could not open command palette")
				}
			},
			close: func(w *Workspace) { w.commands.open = false },
		},
		{
			name:  "portal-atlas",
			open:  func(_ *testing.T, w *Workspace) { w.openPortalAtlas() },
			close: func(w *Workspace) { w.portals.open = false },
		},
	}
	for _, tc := range modalCases {
		t.Run(tc.name, func(t *testing.T) {
			w, apps := multipleApplications(t, 2)
			w.desktop = true
			closer := &closingApplications{fakeApplications: apps}
			w.SetApplications(closer)
			closeShortcutRegressionFocus(t, w, apps.surfaces[0])

			if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 {
				t.Fatal("fresh Super+C did not close exactly once")
			}
			tc.open(t, w)
			w.Handle(experience.Event{Kind: experience.KeyboardCancel})
			if w.applicationCloseShortcut.active {
				t.Fatal("keyboard cancellation behind a modal left Super+C armed")
			}
			tc.close(w)
			closeShortcutRegressionFocus(t, w, apps.surfaces[1])
			assertOrdinaryCReachesFocusedApplication(t, w, apps, apps.surfaces[1].ID)
			if len(closer.closed) != 1 {
				t.Fatalf("keyboard cancellation issued another close: %v", closer.closed)
			}
		})
	}
}

func TestSuperCKeyboardCancelBehindCommandsDoesNotPoisonHelp(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	w.desktop = true
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	closeShortcutRegressionFocus(t, w, apps.surfaces[0])

	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 {
		t.Fatal("fresh Super+C did not close exactly once")
	}
	if !w.openCommands() {
		t.Fatal("could not open command palette")
	}
	w.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if w.commands.open || w.applicationCloseShortcut.active {
		t.Fatal("keyboard cancellation did not clear commands and close shortcut")
	}

	w.openHelp()
	w.helpOpen = false
	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 2 {
		t.Fatalf("stale Help key ownership swallowed fresh Super+C: %v", closer.closed)
	}
}

func TestSuperCReleaseRemainsOwnedAfterAnotherApplicationTakesFocus(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	closeShortcutRegressionFocus(t, w, apps.surfaces[0])

	if !w.Handle(superC(true, false, experience.ModSuper)) || len(closer.closed) != 1 || closer.closed[0] != apps.surfaces[0].ID {
		t.Fatalf("fresh Super+C did not close the active application: %v", closer.closed)
	}
	closeShortcutRegressionFocus(t, w, apps.surfaces[1])
	before := len(apps.events)
	if !w.Handle(superC(false, false, 0)) || len(apps.events) != before {
		t.Fatal("Super+C release leaked into an application focused mid-stroke")
	}
	if w.applicationCloseShortcut.active {
		t.Fatal("released Super+C stroke remained armed after focus changed")
	}
	assertOrdinaryCReachesFocusedApplication(t, w, apps, apps.surfaces[1].ID)
}

func TestSuperCCannotAcquireFromHeldClientStrokeOrOrphanRepeat(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	closeShortcutRegressionFocus(t, w, apps.surfaces[0])

	clientStroke := []experience.Event{
		superC(true, false, 0),
		// Super was pressed after C. Even a duplicate down that is not marked as
		// a repeat remains part of the client-owned physical stroke.
		superC(true, false, experience.ModSuper),
		superC(false, false, experience.ModSuper),
	}
	before := len(apps.events)
	for _, event := range clientStroke {
		if !w.Handle(event) {
			t.Fatalf("client-owned C event was not consumed: %+v", event)
		}
	}
	if len(closer.closed) != 0 || w.applicationCloseShortcut.active {
		t.Fatalf("adding Super to a held C stroke acquired close: %v", closer.closed)
	}
	if got := apps.events[before:]; len(got) != len(clientStroke) {
		t.Fatalf("client-owned C stroke lost events: %+v", got)
	} else {
		for i, event := range clientStroke {
			if got[i] != (applicationEvent{id: apps.surfaces[0].ID, event: event}) {
				t.Fatalf("client-owned C event changed: got %+v want %+v", got[i], event)
			}
		}
	}

	// A repeat can arrive after focus or routing changes hid the original down.
	// It cannot become a fresh global shortcut edge.
	orphan := []experience.Event{
		superC(true, true, experience.ModSuper),
		superC(false, false, 0),
	}
	before = len(apps.events)
	for _, event := range orphan {
		if !w.Handle(event) {
			t.Fatalf("orphan C event was not consumed by the focused client: %+v", event)
		}
	}
	if len(closer.closed) != 0 || w.applicationCloseShortcut.active || len(apps.events) != before+len(orphan) {
		t.Fatalf("orphan repeat acquired or stranded close: closed=%v events=%+v", closer.closed, apps.events[before:])
	}
}
