package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func ambientCatContactPhase(t *testing.T, w *Workspace, want bool) (time.Duration, int, experience.ApplicationSurface) {
	t.Helper()
	w.layout(w.width, w.height)
	w.syncScene()
	for phase := time.Duration(0); phase < ambientCatCycle; phase += 20 * time.Millisecond {
		if ambientCatBehavior(phase).activity != ambientCatRunning {
			continue
		}
		w.ambientCat.phase = phase
		w.ambientCat.syncPose()
		anyContact := false
		for leg := 0; leg < 2; leg++ {
			point, ok := w.catWorldPaw(leg)
			if !ok {
				continue
			}
			x, y, _, visible := w.camera.Project(point, w.viewport)
			surface, hit := w.catApplicationAt(x, y)
			anyContact = anyContact || visible && hit
			if want && visible && hit {
				return phase, leg, surface
			}
		}
		if !want && !anyContact {
			return phase, 0, experience.ApplicationSurface{}
		}
	}
	t.Fatalf("could not find ambient cat contact=%t in one behavior cycle", want)
	return 0, 0, experience.ApplicationSurface{}
}

func TestAmbientCatCollisionNeedsFreshContactThenMovesAndUndoesWindow(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	w.environment.Cat = true
	w.environment.CatCollisions = true
	contactPhase, _, surface := ambientCatContactPhase(t, w, true)
	if surface.ID != apps.surfaces[0].ID {
		t.Fatal("contact search did not find the visible application")
	}
	w.ambientCat.phase = contactPhase
	w.ambientCat.syncPose()
	w.resetAmbientCatCollisions()
	w.updateAmbientCatCollisions()
	if w.windowThrow != nil || w.CanUndo() {
		t.Fatal("initial overlap manufactured a cat impact")
	}
	w.updateAmbientCatCollisions()
	if w.windowThrow != nil {
		t.Fatal("sustained overlap generated a duplicate cat impact")
	}

	clearPhase, _, _ := ambientCatContactPhase(t, w, false)
	w.ambientCat.phase = clearPhase
	w.ambientCat.syncPose()
	w.updateAmbientCatCollisions()
	before, history := w.Document(), w.historyPosition
	w.ambientCat.phase = contactPhase
	w.ambientCat.syncPose()
	w.updateAmbientCatCollisions()
	if w.windowThrow == nil || !throwingSurface(w, surface.ID) || w.historyPosition != history+1 || w.ambientCat.swatRemaining == 0 {
		t.Fatal("fresh paw contact did not start one undoable throw and swat")
	}
	if w.Document().View.Application.Active != before.View.Application.Active || w.Document().View.Application.Selected != before.View.Application.Selected || w.applicationFocusedID != 0 {
		t.Fatal("ambient paw impact changed focus or selection")
	}

	// The preference gates future contacts, not the physical motion already in
	// flight. Disable it while observing the closed-form throw settle.
	w.environment.CatCollisions = false
	for step := 0; w.windowThrow != nil && step < 40; step++ {
		w.Update(100 * time.Millisecond)
	}
	after := w.Document()
	if w.windowThrow != nil || after.View.Application.Layouts == before.View.Application.Layouts {
		t.Fatal("cat impulse did not move and naturally settle the window")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document().View.Application.Layouts != before.View.Application.Layouts {
		t.Fatal("one Undo did not restore the pre-impact placement")
	}
}

func TestWindowImpulsePreservesUnrelatedThrowAndRebasesMovingTarget(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	a, b := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, a)
	w.Update(80 * time.Millisecond)
	original := w.windowThrow
	if original == nil || !throwingSurface(w, a.ID) {
		t.Fatal("fixture did not start the first throw")
	}
	originalEdit, originalElapsed := original.editID, original.elapsed
	if !w.applyWindowImpulse(b.Key, -1.6, .8, 0) {
		t.Fatal("cat-style impulse did not start an independent throw")
	}
	if !throwingSurface(w, a.ID) || !throwingSurface(w, b.ID) || original.editID != originalEdit || original.elapsed != originalElapsed {
		t.Fatal("starting an independent impulse stopped or rebased another window")
	}

	beforeX, beforeY, beforeZ := windowThrowInstantVelocity(original)
	if !w.applyWindowImpulse(a.Key, .7, -.45, .12) {
		t.Fatal("impulse did not join a moving target")
	}
	if original.editID != originalEdit || original.elapsed != 0 || !throwingSurface(w, b.ID) {
		t.Fatal("target rebase replaced its edit or stopped an unrelated throw")
	}
	afterX, afterY, afterZ := windowThrowInstantVelocity(original)
	if math.Abs(afterX-(beforeX+.7)) > 1e-6 || math.Abs(afterY-(beforeY-.45)) > 1e-6 || math.Abs(afterZ-(beforeZ+.12)) > 1e-6 {
		t.Fatalf("rebased velocity did not add the impulse: before=(%g,%g,%g) after=(%g,%g,%g)", beforeX, beforeY, beforeZ, afterX, afterY, afterZ)
	}

	firstBefore, secondBefore := w.Document().View.Application.Layouts[0], w.Document().View.Application.Layouts[1]
	w.Update(100 * time.Millisecond)
	firstAfter, secondAfter := w.Document().View.Application.Layouts[0], w.Document().View.Application.Layouts[1]
	if firstAfter == firstBefore || secondAfter == secondBefore {
		t.Fatal("linked target and unrelated impulse did not both continue moving")
	}
}

func TestAmbientCatDoesNotImpulseAWindowHeldForMoveOrResize(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	selected := w.m.applicationState.movementSelectionFor(apps.surfaces[0].Key)
	w.pointer = pointerCapture{kind: captureApplicationPlacement, windowSelection: selected}
	if !w.ambientCatTargetHeld(selected) {
		t.Fatal("held placement was not protected from a cat impulse")
	}
	w.pointer = pointerCapture{kind: captureApplicationResize, resizeKey: apps.surfaces[0].Key}
	if !w.ambientCatTargetHeld(selected) {
		t.Fatal("held resize was not protected from a cat impulse")
	}
	w.pointer = pointerCapture{}
	if w.ambientCatTargetHeld(selected) {
		t.Fatal("idle window was treated as held")
	}
}

func TestWindowImpulseCarriesAnExplicitGroupRigidly(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	document := w.Document()
	document.View.Application.Layouts[0].Group = 1
	document.View.Application.Layouts[1].Group = 1
	w.install(document, false)
	before := w.Document().View.Application.Layouts
	if !w.applyWindowImpulse(apps.surfaces[0].Key, 1.4, -.8, .2) {
		t.Fatal("group impulse was rejected")
	}
	w.Update(120 * time.Millisecond)
	after := w.Document().View.Application.Layouts
	dx0, dy0, dz0 := after[0].X-before[0].X, after[0].Y-before[0].Y, after[0].Depth-before[0].Depth
	dx1, dy1, dz1 := after[1].X-before[1].X, after[1].Y-before[1].Y, after[1].Depth-before[1].Depth
	if math.Abs(float64(dx0-dx1)) > 1e-6 || math.Abs(float64(dy0-dy1)) > 1e-6 || math.Abs(float64(dz0-dz1)) > 1e-6 {
		t.Fatalf("group did not retain its arrangement: first=(%g,%g,%g) second=(%g,%g,%g)", dx0, dy0, dz0, dx1, dy1, dz1)
	}
}
