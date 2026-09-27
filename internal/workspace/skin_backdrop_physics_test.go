package workspace

import (
	"testing"
	"time"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinBackdropDiscardsHiddenWallImpactsWhileWindowsStillBounce(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 1)
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	d := w.Document()
	d.View.Application.Layouts[0].Depth = energyWallDepth + energyWindowClearance + .1
	w.install(d, false)
	before := *w.energyNet
	if !w.applyWindowImpulse(apps.surfaces[0].Key, 0, 0, -2) {
		t.Fatal("fixture did not start a rearward throw")
	}
	w.Update(time.Second)
	if len(w.energyWallImpacts) != 0 || *w.energyNet != before {
		t.Fatal("hidden wall accumulated or applied visual impact events")
	}
	if motion := w.windowThrow; motion == nil || !motion.wallImpactSent || w.Document().View.Application.Layouts[0].Depth <= energyWallDepth+energyWindowClearance {
		t.Fatal("suppressing hidden visuals changed the physical window bounce")
	}
	selected, err = skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.updateBackground(time.Millisecond)
	if w.energyNet.impactCount != before.impactCount {
		t.Fatal("returning to the ambient desktop replayed hidden impacts")
	}
}

func TestSkinBackdropClearsPendingVisualEventsAndCatContactBaseline(t *testing.T) {
	w, _ := windowThrowWorkspace(t, 1)
	w.environment.Cat, w.environment.CatCollisions = true, true
	contact, _, _ := ambientCatContactPhase(t, w, true)
	clear, _, _ := ambientCatContactPhase(t, w, false)
	w.ambientCat.phase = clear
	w.ambientCat.syncPose()
	w.updateAmbientCatCollisions()
	w.queueEnergyWallImpact(0, 0, 1, 0)
	if w.ambientCatCollisions.contacts == nil || len(w.energyWallImpacts) != 1 {
		t.Fatal("fixture did not establish pending ambient state")
	}
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Update(0)
	if len(w.energyWallImpacts) != 0 || w.ambientCatCollisions.contacts != nil || w.ambientCatCollisions.paws != [2]ambientCatPawScreen{} {
		t.Fatal("hiding ambient content retained stale impacts or paw contacts")
	}
	before := *w.energyNet
	w.impactEnergyWall(0, 0, 1)
	w.queueEnergyWallImpact(0, 0, 1, 0)
	w.updateAmbientCatCollisions()
	if len(w.energyWallImpacts) != 0 || *w.energyNet != before || w.ambientCatCollisions.contacts != nil {
		t.Fatal("a direct hidden ambient callback created visual state")
	}
	// Returning to an overlapping pose establishes a baseline; it must not
	// interpret changes made while the actors were hidden as a fresh impact.
	w.ambientCat.phase = contact
	w.ambientCat.syncPose()
	selected, err = skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.updateBackground(time.Nanosecond)
	if w.windowThrow != nil || w.ambientCatCollisions.contacts == nil {
		t.Fatal("resuming ambient content manufactured a cat impact")
	}
}
