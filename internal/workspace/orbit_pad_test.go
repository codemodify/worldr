package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func orbitPadPoint(w *Workspace) (float32, float32) {
	return w.ox + (orbitPadBounds.x+orbitPadBounds.w/2)*w.scale,
		w.oy + (orbitPadBounds.y+orbitPadBounds.h/2)*w.scale
}

func TestDesktopEmptySpaceDoesNotOrbit(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	before, history := w.Document(), w.historyPosition

	for i, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 700, Y: 430},
		{Kind: experience.PointerMove, Button: experience.ButtonPrimary, X: 790, Y: 455},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 790, Y: 455},
	} {
		if w.Handle(event) {
			t.Fatalf("empty desktop consumed pointer event %d as an implicit orbit", i)
		}
	}
	if w.pointer.kind != captureNone || w.Document() != before || w.historyPosition != history {
		t.Fatal("empty-space drag changed the desktop camera or edit history")
	}
}

func TestDesktopOrbitPadIsScaledAndOneUndoableGesture(t *testing.T) {
	w := desktop(t)
	w.Draw(1600, 900) // A horizontal letterbox gives the hit target a non-zero offset.
	x, y := orbitPadPoint(w)
	before, history := w.Document(), w.historyPosition

	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureOrbitPad {
		t.Fatal("scaled orbit pad did not acquire its dedicated pointer capture")
	}
	if !pointer(w, experience.PointerMove, x+82*w.scale, y+27*w.scale) || w.Document().View.Camera == before.View.Camera {
		t.Fatal("orbit-pad drag did not preview camera rotation")
	}
	if !pointer(w, experience.PointerUp, x+82*w.scale, y+27*w.scale) || w.pointer.kind != captureNone {
		t.Fatal("orbit-pad release did not finish its pointer capture")
	}
	after := w.Document()
	if w.historyPosition != history+1 || !w.CanUndo() {
		t.Fatal("orbit-pad drag was not recorded as one edit")
	}
	withoutCamera := after
	withoutCamera.View.Camera = before.View.Camera
	if withoutCamera != before {
		t.Fatal("orbit-pad drag changed state outside the camera")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo did not restore the camera before the orbit-pad drag")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != after {
		t.Fatal("redo did not restore the completed orbit-pad drag")
	}
}

func TestDesktopOrbitPadCancelRestoresPreviewWithoutHistory(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	x, y := orbitPadPoint(w)
	before, history, historyLength := w.Document(), w.historyPosition, len(w.history)

	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x-70, y+22)
	if w.pointer.kind != captureOrbitPad || w.Document().View.Camera == before.View.Camera {
		t.Fatal("fixture failed to start an orbit-pad preview")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerCancel}) {
		t.Fatal("orbit-pad cancellation was not consumed")
	}
	if w.pointer.kind != captureNone || w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
		t.Fatal("cancelled orbit-pad drag retained camera state or edit history")
	}
}

func TestAxialEmptySceneStillUsesDirectOrbit(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	before := w.Document()

	if !pointer(w, experience.PointerDown, 600, 400) || w.pointer.kind != captureOrbit {
		t.Fatal("AXIAL scene lost its direct orbit capture")
	}
	pointer(w, experience.PointerMove, 680, 425)
	if w.Document().View.Camera == before.View.Camera {
		t.Fatal("AXIAL direct scene drag did not preview an orbit")
	}
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	if w.Document() != before || w.pointer.kind != captureNone {
		t.Fatal("cancelling AXIAL direct orbit did not restore its camera")
	}
}

func TestOrbitPadDoesNotStopIndependentWindowThrow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	startWindowThrow(t, w, apps.surfaces[0])
	w.Update(40 * time.Millisecond)
	motion := w.windowThrow
	before := w.Document().View.Application.Layouts[0]
	x, y := orbitPadPoint(w)

	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+45, y-18)
	pointer(w, experience.PointerUp, x+45, y-18)
	if w.windowThrow != motion || !throwingSurface(w, apps.surfaces[0].ID) {
		t.Fatal("orbit-pad gesture stopped an unrelated coasting window")
	}
	w.Update(60 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == before || !throwingSurface(w, apps.surfaces[0].ID) {
		t.Fatal("coasting window did not keep advancing through orbit-pad input")
	}
}
