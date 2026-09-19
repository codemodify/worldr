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

func TestWindowMaximizeRestoresExactCustomSizeAsOneEdit(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	key := apps.surfaces[0].Key
	command(t, w, Action{Kind: ResizeApplication, ApplicationKey: key, Width: 1137, Height: 731})
	i := w.m.applicationState.index(key)
	original := w.m.applicationState.Layouts[i]
	history, events := w.historyPosition, len(apps.events)

	clickApplicationWindowControl(t, w, apps.surfaces[0], windowControlMaximize)
	p := w.m.applicationState.Layouts[i]
	if !p.Maximized || p.Minimized || p.Width != maximizedApplicationWidth || p.Height != maximizedApplicationHeight || p.RestoreWidth != original.Width || p.RestoreHeight != original.Height || p.RestoreWide != original.Wide {
		t.Fatalf("maximize did not retain and apply exact dimensions: %+v", p)
	}
	if w.historyPosition != history+1 || len(apps.events) != events {
		t.Fatal("maximize was not one workspace edit or leaked pointer input")
	}
	if got := apps.resizes[len(apps.resizes)-1]; got.id != apps.surfaces[0].ID || got.width != maximizedApplicationWidth || got.height != maximizedApplicationHeight {
		t.Fatalf("maximize did not configure the client to maximum size: %+v", got)
	}
	command(t, w, Action{Kind: Undo})
	if got := w.m.applicationState.Layouts[i]; got.Width != original.Width || got.Height != original.Height || got.Maximized {
		t.Fatalf("undo did not restore the pre-maximize size: %+v", got)
	}
	command(t, w, Action{Kind: Redo})
	if got := w.m.applicationState.Layouts[i]; !got.Maximized || got.Width != maximizedApplicationWidth || got.Height != maximizedApplicationHeight {
		t.Fatalf("redo did not restore the maximized size: %+v", got)
	}

	clickApplicationWindowControl(t, w, apps.surfaces[0], windowControlMaximize)
	p = w.m.applicationState.Layouts[i]
	if p.Width != original.Width || p.Height != original.Height || p.Wide != original.Wide || p.Maximized || p.RestoreWidth != 0 || p.RestoreHeight != 0 || p.RestoreWide {
		t.Fatalf("maximize restore lost the exact custom dimensions: %+v", p)
	}
	if got := apps.resizes[len(apps.resizes)-1]; got.width != original.Width || got.height != original.Height {
		t.Fatalf("restore did not configure the client to its prior size: %+v", got)
	}
}

func TestWindowMinimizeLeavesDiscoverableSpatialStripAndRestores(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	surface := apps.surfaces[0]
	i := w.m.applicationState.index(surface.Key)
	history, events := w.historyPosition, len(apps.events)

	clickApplicationWindowControl(t, w, surface, windowControlMinimize)
	w.Draw(1440, 900)
	root := w.scene.Node(w.applicationNodes[surface.ID])
	frame := w.scene.Node(w.applicationFrames[surface.ID])
	grip := w.scene.Node(w.applicationDragHandles[surface.ID])
	if !w.m.applicationState.Layouts[i].Minimized || root == nil || root.Surface != nil || frame == nil || !frame.Hidden || grip == nil || grip.Hidden || grip.Mesh != w.applicationMinimizedBarMesh {
		t.Fatal("minimize did not collapse the live window into its spatial strip")
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
	point := root.Transform.Mul(grip.Transform).TransformPoint(scene.Vec3{X: 0, Y: .543})
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		t.Fatal("minimized strip is not discoverable in the spatial scene")
	}
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); !ok || hit.Node != w.applicationDragHandles[surface.ID] {
		t.Fatalf("minimized strip cannot be picked: %+v %t", hit, ok)
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

	clickApplicationWindowControl(t, w, surface, windowControlMinimize)
	w.Draw(1440, 900)
	if w.m.applicationState.Layouts[i].Minimized || root.Surface != surface.Texture || frame.Hidden || grip.Mesh == w.applicationMinimizedBarMesh {
		t.Fatal("minimize control did not restore the complete live window")
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
	w, apps := multipleApplications(t, 2)
	closer := &closingApplications{fakeApplications: apps}
	w.SetApplications(closer)
	w.Draw(1440, 900)
	target, sibling := apps.surfaces[0], apps.surfaces[1]
	x, y := visibleApplication(t, w, sibling)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("fixture did not focus the sibling window")
	}

	clickApplicationWindowControl(t, w, target, windowControlMinimize)
	if w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("minimizing an inactive window stole sibling keyboard focus")
	}
	clickApplicationWindowControl(t, w, target, windowControlClose)
	if len(closer.closed) != 1 || closer.closed[0] != target.ID || w.applicationFocusedID != sibling.ID || !w.OwnsKeyboard() {
		t.Fatal("closing an inactive window stole sibling focus or targeted the wrong surface")
	}
}
