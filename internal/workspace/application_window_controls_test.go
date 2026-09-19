package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func applicationWindowControlPoint(t *testing.T, w *Workspace, surface experience.ApplicationSurface, kind applicationWindowControl) (float32, float32) {
	t.Helper()
	w.Draw(1440, 900)
	var target box
	for _, control := range applicationWindowControlBoxes {
		if control.kind == kind {
			target = control.box
			break
		}
	}
	root := w.scene.Node(w.applicationNodes[surface.ID])
	grip := w.scene.Node(w.applicationDragHandles[surface.ID])
	control := w.scene.Node(w.applicationWindowControls[surface.ID])
	if root == nil || grip == nil || control == nil || target == (box{}) {
		t.Fatal("window control is not attached")
	}
	point := root.Transform.Mul(grip.Transform).Mul(control.Transform).TransformPoint(scene.Vec3{X: target.x + target.w/2, Y: target.y + target.h/2})
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible || x < w.viewport.X || x > w.viewport.X+w.viewport.Width || y < w.viewport.Y || y > w.viewport.Y+w.viewport.Height {
		t.Fatalf("window control %d is outside the viewport at %.1f,%.1f", kind, x, y)
	}
	gotSurface, gotKind, node, ok := w.applicationWindowControlAt(x, y)
	if !ok || gotSurface.ID != surface.ID || gotKind != kind || node != w.applicationWindowControls[surface.ID] {
		t.Fatalf("window control is not perspective-pickable: surface=%d kind=%d node=%d hit=%t", gotSurface.ID, gotKind, node, ok)
	}
	return x, y
}

func clickApplicationWindowControl(t *testing.T, w *Workspace, surface experience.ApplicationSurface, kind applicationWindowControl) {
	t.Helper()
	x, y := applicationWindowControlPoint(t, w, surface, kind)
	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureApplicationWindowControl {
		t.Fatalf("window control %d did not capture its press", kind)
	}
	if !pointer(w, experience.PointerUp, x, y) {
		t.Fatalf("window control %d did not consume its release", kind)
	}
}

func TestWindowSquareControlMatchesSuperDoubleClickRead(t *testing.T) {
	controlWorkspace, controlApps := multipleApplications(t, 2)
	gestureWorkspace, gestureApps := multipleApplications(t, 2)
	target := controlApps.surfaces[1]
	before := controlWorkspace.Document().View.Application.Layouts[1]
	controlHistory, controlEvents, controlResizes := controlWorkspace.historyPosition, len(controlApps.events), len(controlApps.resizes)

	clickApplicationWindowControl(t, controlWorkspace, target, windowControlRead)
	view := controlWorkspace.Document().View.Application
	if !view.Reading || view.Active != target.Key || view.Overview || view.Placing {
		t.Fatalf("square window control did not read its target: %+v", view)
	}
	if view.Layouts[1] != before || len(controlApps.resizes) != controlResizes {
		t.Fatal("square window control resized the application instead of entering Read")
	}
	if controlWorkspace.historyPosition != controlHistory+2 || len(controlApps.events) != controlEvents || controlWorkspace.OwnsKeyboard() {
		t.Fatal("square window control did not preserve the direct-window Read input contract")
	}

	gestureTarget := gestureApps.surfaces[1]
	x, y := visibleApplication(t, gestureWorkspace, gestureTarget)
	superApplicationClick(gestureWorkspace, x, y, 1000, 1010)
	superApplicationClick(gestureWorkspace, x, y, 1200, 1210)
	if controlWorkspace.Document() != gestureWorkspace.Document() {
		t.Fatal("square window control and Super+double-click produced different workspace state")
	}
	if _, ok := controlWorkspace.buttonAction(1260, 45); ok {
		t.Fatal("removed header Read button still has an active hit target")
	}
}

func TestWindowMinimizeHidesAllChromeAndOverviewRestores(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	surface := apps.surfaces[0]
	i := w.m.applicationState.index(surface.Key)
	history, events := w.historyPosition, len(apps.events)

	formerControlX, formerControlY := applicationWindowControlPoint(t, w, surface, windowControlMinimize)
	if !pointer(w, experience.PointerDown, formerControlX, formerControlY) || !pointer(w, experience.PointerUp, formerControlX, formerControlY) {
		t.Fatal("minimize control did not consume its click")
	}
	w.Draw(1440, 900)
	root := w.scene.Node(w.applicationNodes[surface.ID])
	frame := w.scene.Node(w.applicationFrames[surface.ID])
	grip := w.scene.Node(w.applicationDragHandles[surface.ID])
	controls := w.scene.Node(w.applicationWindowControls[surface.ID])
	resize := w.scene.Node(w.applicationResizeHandles[surface.ID])
	if !w.m.applicationState.Layouts[i].Minimized || root == nil || root.Surface != nil || frame == nil || !frame.Hidden || grip == nil || !grip.Hidden || controls == nil || !controls.Hidden || resize == nil || !resize.Hidden {
		t.Fatal("minimize did not hide the live window and all of its chrome")
	}
	if _, _, _, ok := w.applicationWindowControlAt(formerControlX, formerControlY); ok {
		t.Fatal("hidden minimized titlebar retained a window-control hit target")
	}
	if len(w.applicationSurfaces) != 1 || w.historyPosition != history+1 || len(apps.events) != events {
		t.Fatal("minimize closed the provider surface, split history, or leaked client input")
	}
	state, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	restored := study(t)
	if err = restored.LoadState(state); err != nil || !restored.Document().View.Application.Layouts[i].Minimized {
		t.Fatal("minimized placement did not survive workspace persistence", err)
	}
	command(t, w, Action{Kind: Undo})
	w.Draw(1440, 900)
	if w.m.applicationState.Layouts[i].Minimized || root.Surface != surface.Texture {
		t.Fatal("undo did not restore the minimized live window")
	}
	command(t, w, Action{Kind: Redo})
	w.Draw(1440, 900)
	if !w.m.applicationState.Layouts[i].Minimized || root.Surface != nil {
		t.Fatal("redo did not collapse the live window again")
	}

	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	if root.Surface != surface.Texture || root.Hidden || frame.Hidden || !grip.Hidden || !controls.Hidden || !resize.Hidden {
		t.Fatal("Overview did not reveal the minimized content without exposing window chrome")
	}
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: surface.Key})
	command(t, w, Action{Kind: ToggleApplicationOverview})
	w.Draw(1440, 900)
	if w.m.applicationState.Layouts[i].Minimized || root.Surface != surface.Texture || frame.Hidden || grip.Hidden || controls.Hidden || resize.Hidden {
		t.Fatal("Overview selection did not restore the complete live window")
	}
}

func TestUndoActiveWindowMinimizeRestoresExactSelectionAfterLiveSync(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	before := w.Document()
	active := before.View.Application.Active
	if active != apps.surfaces[0].Key || before.View.Application.Selected != 1 {
		t.Fatal("fixture did not begin with one exact active selection")
	}

	command(t, w, Action{Kind: ToggleApplicationMinimized, ApplicationKey: active})
	w.Update(0)
	if view := w.Document().View.Application; view.Active != apps.surfaces[1].Key || view.Selected != 1<<1 {
		t.Fatalf("live sync did not select the visible sibling: active=%q selected=%032b", view.Active, view.Selected)
	}

	command(t, w, Action{Kind: Undo})
	if got := w.Document(); got != before {
		t.Fatalf("undo did not restore the exact pre-minimize document:\n got: %+v\nwant: %+v", got, before)
	}
}

func TestWindowCloseCancelsOnlyTargetInputAndWaitsForProviderWithdrawal(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	w.Draw(1440, 900)
	target := apps.surfaces[0]
	x, y := visibleApplication(t, w, target)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.applicationFocusedID != target.ID {
		t.Fatal("fixture did not focus close target")
	}
	before, history, events := w.Document(), w.historyPosition, len(apps.events)
	closer.check = func(id uint64) {
		if id != target.ID || w.applicationFocusedID != 0 || w.applicationCapturedID != 0 || w.OwnsKeyboard() {
			t.Fatalf("close reached provider before target input cancellation: id=%d", id)
		}
	}
	clickApplicationWindowControl(t, w, target, windowControlClose)
	if len(closer.closed) != 1 || closer.closed[0] != target.ID || w.Document() != before || w.historyPosition != history {
		t.Fatal("window close changed the document or targeted the wrong live surface")
	}
	if len(w.applicationSurfaces) != 2 || w.scene.Node(w.applicationNodes[target.ID]) == nil {
		t.Fatal("workspace withdrew a window before its provider accepted close")
	}
	for _, sent := range apps.events[events:] {
		if sent.id != target.ID || sent.event.Kind != experience.PointerCancel && sent.event.Kind != experience.KeyboardCancel {
			t.Fatalf("window chrome leaked input to application content: %+v", sent)
		}
	}
	apps.surfaces = apps.surfaces[1:]
	w.Update(0)
	if len(w.applicationSurfaces) != 1 || w.scene.Node(w.applicationNodes[target.ID]) != nil {
		t.Fatal("provider withdrawal did not retire the closed window")
	}
}

func TestInactiveWindowControlsPreserveSiblingKeyboardFocus(t *testing.T) {
	w, apps := multipleApplications(t, 3)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	w.Draw(1440, 900)
	minimizeTarget, closeTarget, sibling := apps.surfaces[0], apps.surfaces[1], apps.surfaces[2]
	x, y := visibleApplication(t, w, sibling)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("fixture did not focus the sibling window")
	}

	clickApplicationWindowControl(t, w, minimizeTarget, windowControlMinimize)
	if w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("minimizing an inactive window stole sibling keyboard focus")
	}
	clickApplicationWindowControl(t, w, closeTarget, windowControlClose)
	if len(closer.closed) != 1 || closer.closed[0] != closeTarget.ID || w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("closing an inactive window stole sibling focus or targeted the wrong surface")
	}
}
