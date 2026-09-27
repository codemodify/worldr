package workspace

import (
	"fmt"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func orbitPadPoint(w *Workspace) (float32, float32) {
	return w.ox + (orbitPadDragBounds.x+orbitPadDragBounds.w/2)*w.scale,
		w.oy + (orbitPadDragBounds.y+orbitPadDragBounds.h/2)*w.scale
}

func orbitControlPoint(w *Workspace, target box) (float32, float32) {
	return w.ox + (target.x+target.w/2)*w.scale,
		w.oy + (target.y+target.h/2)*w.scale
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
	if orbitPadBounds.x != applicationDockBounds.x || orbitPadBounds.w != applicationDockBounds.w || orbitPadBounds.y < applicationDockBounds.y+applicationDockBounds.h {
		t.Fatal("scene controller is not a compact continuation below the launcher rail")
	}
	if (x-w.ox)/w.scale <= 720 {
		t.Fatal("scene rotation reticle is not on the right side")
	}
	before, history := w.Document(), w.historyPosition

	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureOrbitPad {
		t.Fatal("scaled orbit pad did not acquire its dedicated pointer capture")
	}
	if !pointer(w, experience.PointerMove, x+82*w.scale, y+27*w.scale) || w.Document().View.Camera == before.View.Camera {
		t.Fatal("orbit-pad drag did not preview camera rotation")
	}
	preview := w.Document().View.Camera
	if preview.Yaw >= before.View.Camera.Yaw || preview.Pitch >= before.View.Camera.Pitch {
		t.Fatalf("inside-sphere controller did not make the view follow a down-right drag: before=%+v after=%+v", before.View.Camera, preview)
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

func TestDesktopFormerBottomLeftOrbitPadAreaIsInert(t *testing.T) {
	w := desktop(t)
	w.Draw(1440, 900)
	former := box{32, 706, 172, 144}
	x, y := former.x+former.w/2, former.y+former.h/2
	before, history, historyLength := w.Document(), w.historyPosition, len(w.history)

	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y},
		{Kind: experience.PointerMove, Button: experience.ButtonPrimary, X: x + 70, Y: y - 24},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x + 70, Y: y - 24},
	} {
		if w.Handle(event) {
			t.Fatalf("former bottom-left scene rotation area consumed input: %+v", event)
		}
	}
	if w.pointer.kind != captureNone || w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
		t.Fatal("former bottom-left scene rotation area changed camera or history")
	}
}

func TestDesktopOrbitPadGearAndResetDoNotStartOrbit(t *testing.T) {
	if desktopResetButton.y != orbitSettingsButton.y || desktopResetButton.x >= orbitSettingsButton.x ||
		desktopResetButton.y+desktopResetButton.h > orbitPadDragBounds.y {
		t.Fatal("reset X and Settings are not separated above the orbit field")
	}
	for name, target := range map[string]box{
		"settings": orbitSettingsButton,
		"reset":    desktopResetButton,
	} {
		t.Run(name, func(t *testing.T) {
			w := desktop(t)
			w.Draw(2880, 1800)
			x, y := orbitControlPoint(w, target)
			before, history, historyLength := w.Document(), w.historyPosition, len(w.history)

			if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureNone || !w.orbitControl.active {
				t.Fatal("scene-controller button did not acquire its non-orbit press")
			}
			if w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
				t.Fatal("pressing a scene-controller button previewed an orbit or edited history")
			}
			if !w.Handle(experience.Event{Kind: experience.PointerCancel}) || w.pointer.kind != captureNone || w.orbitControl.active {
				t.Fatal("scene-controller button press did not cancel cleanly")
			}
			if w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
				t.Fatal("cancelling a scene-controller button changed the document")
			}
		})
	}
}

func TestDesktopResetButtonIsScaledAndOneUndoableEdit(t *testing.T) {
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w := desktop(t)
			w.Draw(dimensions.width, dimensions.height)
			command(t, w, Action{Kind: PanCamera, DeltaX: 4, DeltaY: -2, DeltaDepth: 1})
			before, history, historyLength := w.Document(), w.historyPosition, len(w.history)
			if before.View.Camera == initialModel().document().View.Camera {
				t.Fatal("fixture did not move the camera before Reset")
			}
			x, y := orbitControlPoint(w, desktopResetButton)
			if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureNone || w.orbitControl.target != orbitControlReset {
				t.Fatal("scaled Reset button did not acquire its completed-click gesture")
			}
			if !pointer(w, experience.PointerUp, x, y) || w.orbitControl.active {
				t.Fatal("scaled Reset button did not complete its click")
			}
			after := w.Document()
			if after.View.Camera != initialModel().document().View.Camera {
				t.Fatal("Reset did not restore the initial camera")
			}
			if w.historyPosition != history+1 || len(w.history) != historyLength+1 {
				t.Fatal("Reset was not recorded as exactly one edit")
			}
			command(t, w, Action{Kind: Undo})
			if w.Document() != before {
				t.Fatal("undo did not restore the view before Reset")
			}
			command(t, w, Action{Kind: Redo})
			if w.Document() != after {
				t.Fatal("redo did not restore the reset view")
			}
		})
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

func TestDesktopOrbitReticleIsInertWhileSceneCameraIsFramed(t *testing.T) {
	for _, mode := range []ActionKind{ToggleApplicationReading, ToggleApplicationOverview, ToggleApplicationPlacement} {
		t.Run(string(mode), func(t *testing.T) {
			w, _ := headerDesktopApplications(t, 1)
			command(t, w, Action{Kind: mode})
			w.Draw(1440, 900)
			before, history, historyLength := w.Document(), w.historyPosition, len(w.history)
			x, y := orbitPadPoint(w)
			if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureNone {
				t.Fatal("disabled reticle did not consume its press without starting an orbit")
			}
			pointer(w, experience.PointerMove, x+40, y+18)
			pointer(w, experience.PointerUp, x+40, y+18)
			if w.Document() != before || w.historyPosition != history || len(w.history) != historyLength {
				t.Fatal("disabled reticle changed a hidden camera or history")
			}
		})
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
