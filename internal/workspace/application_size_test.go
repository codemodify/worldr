package workspace

import (
	"fmt"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func narrowApplicationWorkspace(t *testing.T) (*Workspace, *fakeApplications) {
	t.Helper()
	w, apps := windowDragWorkspace(t, 1)
	apps.surfaces[0].MinWidth, apps.surfaces[0].MinHeight = 96, 64
	w.Draw(1440, 900)
	return w, apps
}

func TestNarrowApplicationSizesRestoreByStableKeyAndMaximizeRoundTrip(t *testing.T) {
	for _, size := range [][2]int{{190, 720}, {460, 86}, {230, 120}, {96, 64}, {1920, 1080}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w, apps := narrowApplicationWorkspace(t)
			surface := apps.surfaces[0]
			if initial := apps.resizes[0]; initial.width != 960 || initial.height != 600 {
				t.Fatal("narrow support changed the default application size")
			}
			command(t, w, Action{Kind: ResizeApplication, ApplicationKey: surface.Key, Width: size[0], Height: size[1]})
			if got := apps.resizes[len(apps.resizes)-1]; got.width != size[0] || got.height != size[1] {
				t.Fatalf("narrow provider request = %+v", got)
			}
			_, _, width, height := w.applicationTransformFor(surface)
			if abs(width-4.6*float32(size[0])/960) > .0001 || abs(height-4.6*float32(size[1])/960) > .0001 {
				t.Fatal("narrow logical dimensions and spatial geometry disagree")
			}
			data, err := w.SaveState()
			if err != nil {
				t.Fatal(err)
			}
			restored := desktop(t)
			if err := restored.LoadState(data); err != nil {
				t.Fatal(err)
			}
			surface.ID += 100
			reconnected := &fakeApplications{surfaces: []experience.ApplicationSurface{surface}}
			restored.SetApplications(reconnected)
			if got := reconnected.resizes[len(reconnected.resizes)-1]; got.id != surface.ID || got.width != size[0] || got.height != size[1] {
				t.Fatalf("stable-key reconnection lost narrow dimensions: %+v", got)
			}
			command(t, restored, Action{Kind: ToggleApplicationMaximized, ApplicationKey: surface.Key})
			data, err = restored.SaveState()
			if err != nil {
				t.Fatal(err)
			}
			if err := restored.LoadState(data); err != nil {
				t.Fatal(err)
			}
			command(t, restored, Action{Kind: ToggleApplicationMaximized, ApplicationKey: surface.Key})
			if got := reconnected.resizes[len(reconnected.resizes)-1]; got.width != size[0] || got.height != size[1] {
				t.Fatal("persisted maximize restore lost the narrow size")
			}
		})
	}
}

func TestNarrowApplicationResizeGestureUsesOptInMinimumAndUndo(t *testing.T) {
	for _, size := range [][2]int{{190, 720}, {460, 86}, {230, 120}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w, apps := narrowApplicationWorkspace(t)
			x, y := visibleApplication(t, w, apps.surfaces[0])
			before, history := w.Document(), w.historyPosition
			if !w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y}) || w.pointer.kind != captureApplicationResize {
				t.Fatal("narrow resize did not capture")
			}
			dx := float32(size[0]-w.pointer.resizeWidth) / w.pointer.resizeRatioX
			dy := float32(size[1]-w.pointer.resizeHeight) / w.pointer.resizeRatioY
			w.Handle(experience.Event{Kind: experience.PointerMove, X: x + dx, Y: y + dy})
			w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x + dx, Y: y + dy})
			wantWidth, wantHeight := max(96, size[0]), max(64, size[1])
			p := w.Document().View.Application.Layouts[0]
			if p.Width != wantWidth || p.Height != wantHeight || w.historyPosition != history+1 || w.pointer.kind != captureNone {
				t.Fatalf("gesture did not commit one narrow resize: %+v", p)
			}
			if got := apps.resizes[len(apps.resizes)-1]; got.width != wantWidth || got.height != wantHeight {
				t.Fatalf("provider did not receive narrow gesture size: %+v", got)
			}
			command(t, w, Action{Kind: Undo})
			if w.Document() != before {
				t.Fatal("narrow resize undo changed unrelated workspace state")
			}
			if got := apps.resizes[len(apps.resizes)-1]; got.width != 960 || got.height != 600 {
				t.Fatal("narrow resize undo did not restore the default provider size")
			}
		})
	}
}

func TestLegacyApplicationMinimumSurvivesSmallSavedLayout(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	command(t, w, Action{Kind: ResizeApplication, ApplicationKey: surface.Key, Width: 190, Height: 120})
	if got := apps.resizes[len(apps.resizes)-1]; got.width != 720 || got.height != 440 {
		t.Fatalf("omitted minima changed the legacy provider floor: %+v", got)
	}
	_, _, width, height := w.applicationTransformFor(surface)
	if abs(width-4.6*720/960) > .0001 || abs(height-4.6*440/960) > .0001 {
		t.Fatal("legacy provider request and spatial geometry disagree")
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	restored := desktop(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	reconnected := &fakeApplications{surfaces: []experience.ApplicationSurface{surface}}
	restored.SetApplications(reconnected)
	if got := reconnected.resizes[len(reconnected.resizes)-1]; got.width != 720 || got.height != 440 {
		t.Fatal("saved small dimensions bypassed a legacy provider's minimum")
	}
	x, y := visibleApplication(t, restored, surface)
	if !restored.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y}) || restored.pointer.resizeWidth != 720 || restored.pointer.resizeHeight != 440 {
		t.Fatal("resize started from saved dimensions instead of the effective legacy size")
	}
	restored.Handle(experience.Event{Kind: experience.PointerMove, X: x - 1000, Y: y - 1000})
	if p := restored.Document().View.Application.Layouts[0]; p.Width != 720 || p.Height != 440 {
		t.Fatal("legacy resize gesture used the optional narrow minimum")
	}
	restored.Handle(experience.Event{Kind: experience.PointerCancel})
}

func TestApplicationSizeBoundsRemainTransactional(t *testing.T) {
	w, apps := narrowApplicationWorkspace(t)
	before := w.Document()
	for _, size := range [][2]int{{95, 64}, {96, 63}, {1921, 1080}, {1920, 1081}, {0, 64}, {96, 0}, {-1, -1}} {
		if err := w.Dispatch(Action{Kind: ResizeApplication, ApplicationKey: apps.surfaces[0].Key, Width: size[0], Height: size[1]}); err == nil || w.Document() != before {
			t.Fatalf("invalid size %v changed the document", size)
		}
		invalid := before
		invalid.View.Application.Layouts[0].Width, invalid.View.Application.Layouts[0].Height = size[0], size[1]
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid saved size %v passed validation", size)
		}
	}
}
