package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

type cursorApplications struct {
	*fakeApplications
	cursors   map[uint64]experience.ApplicationCursor
	requested uint64
}

func (a *cursorApplications) ApplicationCursor(id uint64) (experience.ApplicationCursor, bool) {
	a.requested = id
	cursor, ok := a.cursors[id]
	return cursor, ok
}

func TestCursorFollowsPointerFocusAndCaptureNotKeyboard(t *testing.T) {
	w, base := multipleApplications(t, 2)
	apps := &cursorApplications{fakeApplications: base, cursors: map[uint64]experience.ApplicationCursor{
		700: {Texture: base.surfaces[0].Texture, Scale: 2, HotspotX: 3, HotspotY: 4},
		701: {Hidden: true},
	}}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	ax, ay := visibleApplication(t, w, base.surfaces[0])
	bx, by := visibleApplication(t, w, base.surfaces[1])
	if _, ok := w.Cursor(); ok {
		t.Fatal("cursor chosen without a pointer route")
	}
	pointer(w, experience.PointerMove, ax, ay)
	got, ok := w.Cursor()
	if !ok || got != apps.cursors[700] || w.OwnsKeyboard() {
		t.Fatal("hover cursor required or granted keyboard focus")
	}
	beforeClick := len(apps.events)
	pointer(w, experience.PointerDown, ax, ay)
	pointer(w, experience.PointerUp, ax, ay)
	for _, event := range apps.events[beforeClick:] {
		if event.event.Kind == experience.PointerCancel {
			t.Fatal("granting keyboard focus canceled the already-hovered client's cursor")
		}
	}
	pointer(w, experience.PointerMove, bx, by)
	got, ok = w.Cursor()
	if !ok || !got.Hidden || apps.requested != 701 || w.applicationFocusedID != 700 {
		t.Fatal("keyboard focus overrode pointer cursor")
	}
	beforeClick = len(apps.events)
	pointer(w, experience.PointerDown, bx, by)
	for _, event := range apps.events[beforeClick:] {
		if event.id == 701 && event.event.Kind == experience.PointerCancel {
			t.Fatal("selecting the already-hovered inactive app invalidated its cursor")
		}
	}
	pointer(w, experience.PointerMove, ax, ay)
	if got, ok = w.Cursor(); !ok || !got.Hidden || apps.requested != 701 {
		t.Fatal("captured cursor followed the obscured app below it")
	}
	pointer(w, experience.PointerUp, ax, ay)
	if got, ok = w.Cursor(); !ok || got != apps.cursors[700] || w.applicationFocusedID != 701 {
		t.Fatal("release did not retarget cursor independently of keyboard focus")
	}
	pointer(w, experience.PointerDown, ax, ay)
	pointer(w, experience.PointerMove, 10, 10)
	if _, ok = w.Cursor(); !ok {
		t.Fatal("captured cursor disappeared outside the workspace viewport")
	}
	pointer(w, experience.PointerUp, 10, 10)
	if _, ok = w.Cursor(); ok {
		t.Fatal("released capture retained application cursor over workspace chrome")
	}
	pointer(w, experience.PointerMove, bx, by)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if _, ok = w.Cursor(); ok {
		t.Fatal("pointer cancellation retained hidden client cursor")
	}
}

func TestCursorFallsBackForOverviewPlacementClosureAndNativeProviders(t *testing.T) {
	w, base := applicationStudy(t)
	apps := &cursorApplications{fakeApplications: base, cursors: map[uint64]experience.ApplicationCursor{777: {Hidden: true}}}
	w.SetApplications(apps)
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerMove, x, y)
	if got, ok := w.Cursor(); !ok || !got.Hidden {
		t.Fatal("fixture did not obtain hidden client cursor")
	}
	w.helpOpen = true
	if _, ok := w.Cursor(); ok {
		t.Fatal("shortcut help inherited a hidden client cursor")
	}
	w.helpOpen = false
	command(t, w, Action{Kind: ToggleApplicationOverview})
	if _, ok := w.Cursor(); ok {
		t.Fatal("overview inherited client cursor")
	}
	command(t, w, Action{Kind: ToggleApplicationOverview})
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	if _, ok := w.Cursor(); ok {
		t.Fatal("placement inherited client cursor")
	}
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	x, y = visibleApplicationPoint(t, w)
	pointer(w, experience.PointerMove, x, y)
	base.surfaces = nil
	w.Update(0)
	if _, ok := w.Cursor(); ok {
		t.Fatal("closed application retained cursor")
	}
	w.SetApplications(nil)
	if _, ok := w.Cursor(); ok {
		t.Fatal("detached provider retained cursor")
	}
	other, plain := applicationStudy(t)
	x, y = visibleApplicationPoint(t, other)
	pointer(other, experience.PointerMove, x, y)
	if _, ok := other.Cursor(); ok || len(plain.events) == 0 {
		t.Fatal("provider without cursor interface did not use fallback")
	}
}
