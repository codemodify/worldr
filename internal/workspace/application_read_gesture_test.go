package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func superApplicationClick(w *Workspace, x, y float32, down, up uint32) {
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: down})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: up})
}

func applicationClickEvents(events []applicationEvent) int {
	count := 0
	for _, delivered := range events {
		if delivered.event.Kind == experience.PointerDown || delivered.event.Kind == experience.PointerUp {
			count++
		}
	}
	return count
}

func TestSuperDoubleClickReadsInactiveTargetWithoutClientClicksOrFocus(t *testing.T) {
	w, apps := windowDragWorkspace(t, 2)
	target := apps.surfaces[1]
	x, y := visibleApplication(t, w, target)
	before, history := w.Document(), w.historyPosition

	superApplicationClick(w, x, y, 1000, 1010)
	if w.m.applicationState.Reading || w.m.applicationState.Active != target.Key || w.OwnsKeyboard() {
		t.Fatal("first reserved click did not select the pointed-to window without entering Read")
	}
	superApplicationClick(w, x, y, 1200, 1210)
	if !w.m.applicationState.Reading || w.m.applicationState.Active != target.Key {
		t.Fatal("Super+double-click did not enter Read on the inactive pointed-to window")
	}
	if w.OwnsKeyboard() || applicationClickEvents(apps.events) != 0 || len(apps.focus) != 0 {
		t.Fatal("reserved double-click leaked pointer input or keyboard focus to the client")
	}
	if w.historyPosition != history+2 {
		t.Fatalf("selection and Read should be two ordinary reversible actions, history advanced by %d", w.historyPosition-history)
	}
	command(t, w, Action{Kind: Undo})
	if w.m.applicationState.Reading || w.m.applicationState.Active != target.Key {
		t.Fatal("Undo did not reverse the existing Read action first")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("second Undo did not restore the selection before the inactive-target double-click")
	}
}

func TestFreshSuperDoubleClickLeavesReadAndMatchedSecondPressCanStillDrag(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	target := apps.surfaces[0]
	x, y := visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 1000, 1010)
	superApplicationClick(w, x, y, 1200, 1210)
	if !w.m.applicationState.Reading {
		t.Fatal("fixture did not enter Read")
	}
	w.Draw(1440, 900)
	x, y = visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 2000, 2010)
	if w.pointer.kind != captureNone || w.OwnsKeyboard() {
		t.Fatal("first exit click retained capture or focused the client")
	}
	superApplicationClick(w, x, y, 2200, 2210)
	if w.m.applicationState.Reading || applicationClickEvents(apps.events) != 0 || w.OwnsKeyboard() {
		t.Fatal("fresh Super+double-click did not leave Read as a workspace-owned gesture")
	}

	w.Draw(1440, 900)
	x, y = visibleApplication(t, w, target)
	before := w.Document().View.Application.Layouts[0]
	superApplicationClick(w, x, y, 3000, 3010)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 3200})
	w.Handle(experience.Event{Kind: experience.PointerMove, Modifiers: experience.ModSuper, X: x + 45, Y: y + 18, Time: 3230})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x + 45, Y: y + 18, Time: 3240})
	if w.m.applicationState.Reading || w.Document().View.Application.Layouts[0] == before {
		t.Fatal("a matched second press with motion toggled Read instead of preserving Super+drag")
	}
	// The canceled pair is gone: a third click begins a fresh pair.
	x, y = visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 3300, 3310)
	if w.m.applicationState.Reading {
		t.Fatal("a drag left the old double-click chain armed")
	}
}

func TestSuperDoubleClickJitterNeverPreviewsWindowMovement(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	before, history := w.Document(), w.historyPosition
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerMove, Modifiers: experience.ModSuper, X: x + 2, Y: y + 1, Time: 1005},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x + 2, Y: y + 1, Time: 1010},
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x + 1, Y: y + 1, Time: 1200},
		{Kind: experience.PointerMove, Modifiers: experience.ModSuper, X: x + 3, Y: y + 2, Time: 1205},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x + 3, Y: y + 2, Time: 1210},
	} {
		if !w.Handle(event) {
			t.Fatalf("reserved jitter event was not consumed: %+v", event)
		}
	}
	after := w.Document()
	if !after.View.Application.Reading || after.View.Application.Layouts != before.View.Application.Layouts {
		t.Fatal("click jitter moved the window or failed to activate Read")
	}
	if w.historyPosition != history+1 {
		t.Fatalf("jitter created a placement edit; got %d history entries, want one Read edit", w.historyPosition-history)
	}
	if applicationClickEvents(apps.events) != 0 {
		t.Fatal("jittering reserved clicks reached the client")
	}
}

func TestScrollDuringSecondSuperClickCancelsReadToggle(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	target := apps.surfaces[0]
	x, y := visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 1000, 1010)
	before := w.Document().View.Application.Layouts[0].Depth
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1200})
	w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: 4, Time: 1205})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1210})
	if w.m.applicationState.Reading || w.Document().View.Application.Layouts[0].Depth == before {
		t.Fatal("scroll during the second click toggled Read or lost the established held-drag depth gesture")
	}
	// That interrupted pair cannot combine with the next click.
	x, y = visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 1300, 1310)
	if w.m.applicationState.Reading {
		t.Fatal("scroll interruption left the matched click pair armed")
	}

	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y = visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 2000, 2010)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 2200})
	w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: 4, Time: 2205})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 2210})
	if !w.m.applicationState.Reading {
		t.Fatal("scroll during a Read-view click pair incorrectly returned to Space")
	}
}

func TestExtraButtonDuringSecondSuperClickCancelsReadToggle(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	superApplicationClick(w, x, y, 1000, 1010)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1200})
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1202})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1205})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1210})
	if w.m.applicationState.Reading || len(w.windowDragButtons) != 0 {
		t.Fatal("an intervening extra button did not cancel and fully drain the matched click pair")
	}
	if applicationClickEvents(apps.events) != 0 {
		t.Fatal("extra-button interruption leaked the reserved stroke to the client")
	}
}

func TestSuperDoubleClickChainNeedsTimedCompletedUninterruptedClicks(t *testing.T) {
	t.Run("zero first time", func(t *testing.T) {
		w, apps := windowDragWorkspace(t, 1)
		x, y := visibleApplication(t, w, apps.surfaces[0])
		superApplicationClick(w, x, y, 0, 1)
		superApplicationClick(w, x, y, 200, 210)
		if w.m.applicationState.Reading {
			t.Fatal("untimed synthetic first click armed a double-click")
		}
	})
	t.Run("uint32 wrap", func(t *testing.T) {
		w, apps := windowDragWorkspace(t, 1)
		x, y := visibleApplication(t, w, apps.surfaces[0])
		superApplicationClick(w, x, y, ^uint32(0)-255, ^uint32(0)-245)
		superApplicationClick(w, x, y, 0, 10)
		if !w.m.applicationState.Reading {
			t.Fatal("wrapping compositor timestamps broke a valid double-click")
		}
	})
	t.Run("intervening click and cancel", func(t *testing.T) {
		w, apps := windowDragWorkspace(t, 1)
		x, y := visibleApplication(t, w, apps.surfaces[0])
		superApplicationClick(w, x, y, 1000, 1010)
		pointer(w, experience.PointerDown, x, y)
		pointer(w, experience.PointerUp, x, y)
		superApplicationClick(w, x, y, 1200, 1210)
		if w.m.applicationState.Reading {
			t.Fatal("an intervening ordinary client click did not reset the chain")
		}
		w.Handle(experience.Event{Kind: experience.PointerCancel})
		superApplicationClick(w, x, y, 1300, 1310)
		if w.m.applicationState.Reading {
			t.Fatal("pointer cancellation did not reset the chain")
		}
	})
	for _, interruption := range []string{"move away and back", "scroll", "load"} {
		t.Run(interruption, func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			x, y := visibleApplication(t, w, apps.surfaces[0])
			state, err := w.SaveState()
			if err != nil {
				t.Fatal(err)
			}
			superApplicationClick(w, x, y, 1000, 1010)
			switch interruption {
			case "move away and back":
				w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 30, Y: y})
				w.Handle(experience.Event{Kind: experience.PointerMove, X: x, Y: y})
			case "scroll":
				w.Handle(experience.Event{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 1})
			case "load":
				if err := w.LoadState(state); err != nil {
					t.Fatal(err)
				}
				w.Draw(1440, 900)
				x, y = visibleApplication(t, w, apps.surfaces[0])
			}
			superApplicationClick(w, x, y, 1200, 1210)
			if w.m.applicationState.Reading {
				t.Fatalf("%s did not reset the first-click chain", interruption)
			}
		})
	}
	t.Run("help body", func(t *testing.T) {
		w, apps := windowDragWorkspace(t, 1)
		x, y := visibleApplication(t, w, apps.surfaces[0])
		superApplicationClick(w, x, y, 1000, 1010)
		// Exercise modal routing directly: help consumes this press before the
		// application gesture handler gets a chance to classify it.
		w.helpOpen = true
		w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 500, Y: 400, Time: 1100})
		w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 500, Y: 400, Time: 1110})
		w.helpOpen = false
		superApplicationClick(w, x, y, 1200, 1210)
		if w.m.applicationState.Reading {
			t.Fatal("a help-body click consumed before app routing did not reset the chain")
		}
	})
}

func putApplicationBehindPoint(t *testing.T, w *Workspace, surface experience.ApplicationSurface, x, y float32) {
	t.Helper()
	base := w.Document()
	i := base.View.Application.index(surface.Key)
	for vertical := float32(-8); vertical <= 8; vertical += .5 {
		for horizontal := float32(-8); horizontal <= 8; horizontal += .5 {
			next := base
			next.View.Application.Layouts[i].X = horizontal
			next.View.Application.Layouts[i].Y = vertical
			w.install(next, false)
			w.syncScene()
			if hit, ok := w.applicationHit(x, y); ok && w.applicationForNode(hit.Node).ID == surface.ID {
				return
			}
		}
	}
	t.Fatalf("could not place %s behind fixed point %.1f,%.1f", surface.Key, x, y)
}

func TestSuperReadGestureCannotStealFixedOrModalOverlayPresses(t *testing.T) {
	for _, overlay := range []string{"orbit", "dock"} {
		t.Run(overlay, func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			target := apps.surfaces[0]
			var x, y float32
			switch overlay {
			case "orbit":
				x = w.ox + (orbitPadBounds.x+orbitPadBounds.w/2)*w.scale
				y = w.oy + (orbitPadBounds.y+orbitPadBounds.h/2)*w.scale
			case "dock":
				launcher := &dockApplications{fakeApplications: apps, catalog: []experience.ApplicationLaunch{{Kind: "terminal", Title: "Terminal"}}, texture: target.Texture, next: 900}
				w.SetApplications(launcher)
				w.Draw(1440, 900)
				b := applicationDockButtonBounds(0)
				x = w.ox + (b.x+b.w/2)*w.scale
				y = w.oy + (b.y+b.h/2)*w.scale
			}
			putApplicationBehindPoint(t, w, target, x, y)
			if hit, ok := w.applicationHit(x, y); !ok || w.applicationForNode(hit.Node).ID != target.ID {
				t.Fatalf("%s fixture has no application behind it", overlay)
			}
			ax, ay := visibleApplication(t, w, target)
			superApplicationClick(w, ax, ay, 1000, 1010)
			w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1100})
			switch overlay {
			case "orbit":
				if w.pointer.kind != captureOrbitPad {
					t.Fatal("application behind rotation pad stole its Super press")
				}
			case "dock":
				if w.pointer.kind != captureApplicationDock {
					t.Fatal("application behind launcher rail stole its Super press")
				}
			}
			w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1110})
			if w.m.applicationState.Reading {
				t.Fatalf("%s overlay press activated Read", overlay)
			}
		})
	}

	w, apps := windowDragWorkspace(t, 1)
	w.openPortalAtlas()
	if !w.portals.open {
		t.Fatal("portal fixture did not open")
	}
	x, y := visibleApplication(t, w, apps.surfaces[0])
	before := w.m.applicationState.Reading
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 1000})
	if !w.portals.open || w.pointer.kind != captureNone || w.m.applicationState.Reading != before {
		t.Fatal("application behind open portal atlas stole its Super press")
	}
	if applicationClickEvents(apps.events) != 0 {
		t.Fatal("modal portal press leaked to its underlying client")
	}
}

func TestSuperDoubleClickOwnsOverviewPlaceAndPassiveReadTargets(t *testing.T) {
	for _, mode := range []ActionKind{ToggleApplicationOverview, ToggleApplicationPlacement} {
		t.Run(string(mode), func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 2)
			target := apps.surfaces[1]
			command(t, w, Action{Kind: mode})
			w.Draw(1440, 900)
			x, y := visibleApplication(t, w, target)
			superApplicationClick(w, x, y, 1000, 1010)
			superApplicationClick(w, x, y, 1200, 1210)
			view := w.m.applicationState
			if !view.Reading || view.Active != target.Key || view.Overview || view.Placing || applicationClickEvents(apps.events) != 0 || w.OwnsKeyboard() {
				t.Fatal("reserved double-click did not select and read the target cleanly from the workspace mode")
			}
		})
	}

	w, apps, photo := photoDragWorkspace(t, 1)
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, photo)
	before := w.Document().View.Application.Layouts[0]
	superApplicationClick(w, x, y, 2000, 2010)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y, Time: 2200})
	w.Handle(experience.Event{Kind: experience.PointerMove, Modifiers: experience.ModSuper, X: x + 50, Y: y + 18, Time: 2230})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x + 50, Y: y + 18, Time: 2240})
	if w.m.applicationState.Reading || w.Document().View.Application.Layouts[0] == before || applicationClickEvents(apps.events) != 0 {
		t.Fatal("Super+drag on passive Read content did not retain its established workspace movement")
	}
}

func TestSuperDoubleClickRetrievesAllMinimizedOverviewTarget(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	target := apps.surfaces[0]
	command(t, w, Action{Kind: ToggleApplicationMinimized, ApplicationKey: target.Key})
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	if w.application.ID != 0 {
		t.Fatal("all-minimized fixture unexpectedly retained a current application")
	}
	x, y := visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 1000, 1010)
	superApplicationClick(w, x, y, 1200, 1210)
	i := w.m.applicationState.index(target.Key)
	if i < 0 || w.m.applicationState.Layouts[i].Minimized || !w.m.applicationState.Reading || w.m.applicationState.Overview || w.application.ID != target.ID {
		t.Fatal("Super+double-click did not retrieve and read an all-minimized Overview target")
	}
	if applicationClickEvents(apps.events) != 0 || w.OwnsKeyboard() {
		t.Fatal("all-minimized retrieval leaked input or focus to the client")
	}
}

func TestSuperDoubleClickReadLeavesUnrelatedWindowThrowMoving(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	moving, target := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, moving)
	w.Update(40 * time.Millisecond)
	x, y := visibleApplication(t, w, target)
	superApplicationClick(w, x, y, 2000, 2010)
	superApplicationClick(w, x, y, 2200, 2210)
	if !w.m.applicationState.Reading || !throwingSurface(w, moving.ID) {
		t.Fatal("reading one window stopped an unrelated coasting window")
	}
	before := w.Document().View.Application.Layouts[0]
	w.Update(80 * time.Millisecond)
	if !throwingSurface(w, moving.ID) || w.Document().View.Application.Layouts[0] == before {
		t.Fatal("unrelated window did not continue its own coast after Read toggled")
	}
}
