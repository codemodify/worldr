package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func TestSuperDragOnEmptyDesktopPansCameraAsOneEdit(t *testing.T) {
	w := desktop(t)
	desktopApplications(t, w)
	w.Draw(1600, 900)
	var x, y float32
	for py := w.viewport.Y + 30; py < w.viewport.Y+w.viewport.Height-160 && x == 0; py += 70 {
		for px := w.viewport.X + 30; px < w.viewport.X+w.viewport.Width-260; px += 90 {
			if _, hit := w.scene.Pick(w.camera, w.viewport, px, py); !hit {
				x, y = px, py
				break
			}
		}
	}
	if x == 0 {
		t.Fatal("fixture could not find empty workspace around its live window")
	}
	before, history := w.Document(), w.historyPosition
	beforeX, beforeY, _, _ := w.camera.Project(scene.Vec3{}, w.viewport)

	consumed := w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y})
	if !consumed || w.pointer.kind != captureWorkspacePan {
		t.Fatalf("Super+primary on empty workspace did not start camera pan at %.1f,%.1f: consumed=%t capture=%d", x, y, consumed, w.pointer.kind)
	}
	if !pointer(w, experience.PointerMove, x+120, y+55) {
		t.Fatal("camera pan did not retain pointer capture")
	}
	during := w.Document()
	if during.View.Camera == before.View.Camera || during.View.Camera.Yaw != before.View.Camera.Yaw || during.View.Camera.Pitch != before.View.Camera.Pitch || during.View.Camera.Zoom != before.View.Camera.Zoom {
		t.Fatal("pan did not change only the camera target")
	}
	if during.View.Application != before.View.Application {
		t.Fatal("workspace pan moved or selected an application")
	}
	if !pointer(w, experience.PointerUp, x+120, y+55) || w.pointer.kind != captureNone || w.historyPosition != history+1 {
		t.Fatal("camera pan was not committed as one gesture")
	}
	w.Draw(1600, 900)
	afterX, afterY, _, _ := w.camera.Project(scene.Vec3{}, w.viewport)
	if afterX <= beforeX || afterY <= beforeY {
		t.Fatalf("workspace did not follow a right/down drag: before %.1f,%.1f after %.1f,%.1f", beforeX, beforeY, afterX, afterY)
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo did not restore the camera before workspace pan")
	}
}

func TestSuperWheelMovesOnlyHoveredWindowDepth(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	startWindowThrow(t, w, apps.surfaces[0])
	if !throwingSurface(w, apps.surfaces[0].ID) {
		t.Fatal("fixture did not start independent window momentum")
	}
	x, y := visibleApplication(t, w, apps.surfaces[1])
	before, selection, eventCount := w.Document(), w.m.applicationState.Selected, len(apps.events)
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: 10}) {
		t.Fatal("Super+wheel over a window was not consumed")
	}
	after := w.Document()
	if after.View.Application.Layouts[0] != before.View.Application.Layouts[0] || math.Abs(float64(after.View.Application.Layouts[1].Depth-before.View.Application.Layouts[1].Depth+.35)) > .0001 {
		t.Fatal("depth gesture moved the wrong window or used the wrong delta")
	}
	if after.View.Application.Selected != selection || after.View.Camera != before.View.Camera || len(apps.events) != eventCount || w.OwnsKeyboard() {
		t.Fatal("depth gesture changed selection/camera/focus or sent client input")
	}
	if !throwingSurface(w, apps.surfaces[0].ID) {
		t.Fatal("depth gesture stopped another window's momentum")
	}
	coasting := after.View.Application.Layouts[0]
	w.Update(50 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == coasting {
		t.Fatal("unrelated window did not continue coasting after depth gesture")
	}
}

func TestSuperWheelOverWindowControlsStillChangesHoveredDepth(t *testing.T) {
	w, apps := windowDragWorkspace(t, 2)
	target := apps.surfaces[1]
	x, y := applicationWindowControlPoint(t, w, target, windowControlRead)
	view := w.Document().View.Application
	i := view.index(target.Key)
	before := view.Layouts[i]
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModSuper, X: x, Y: y, ScrollY: -8}) {
		t.Fatal("Super+wheel over window controls was not consumed")
	}
	after := w.Document().View.Application.Layouts[i]
	if after.Depth <= before.Depth || after.Maximized || after.Minimized || w.pointer.kind != captureNone {
		t.Fatal("window chrome intercepted hovered-window depth navigation")
	}
}

func TestSuperSecondaryDragResizesWindowAndUndoRestoresProvider(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	x, y := visibleApplication(t, w, surface)
	before, history, events := w.Document(), w.historyPosition, len(apps.events)
	_, _, beforeWidth, beforeHeight := w.applicationTransformFor(surface)

	if !w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y}) || w.pointer.kind != captureApplicationResize {
		t.Fatal("Super+secondary did not start window resize")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 110, Y: y + 70}) {
		t.Fatal("window resize lost pointer capture")
	}
	during := w.Document().View.Application.Layouts[0]
	if during.Width <= 960 || during.Height <= 600 {
		t.Fatalf("resize did not grow both logical dimensions: %+v", during)
	}
	if last := apps.resizes[len(apps.resizes)-1]; last.id != surface.ID || last.width != during.Width || last.height != during.Height {
		t.Fatalf("provider did not receive preview size: %+v / %+v", last, during)
	}
	w.Draw(1440, 900)
	_, _, resizedWidth, resizedHeight := w.applicationTransformFor(surface)
	if resizedWidth <= beforeWidth || resizedHeight <= beforeHeight {
		t.Fatal("logical resize did not visibly resize the spatial window")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x + 110, Y: y + 70}) || w.pointer.kind != captureNone || w.historyPosition != history+1 {
		t.Fatal("resize did not finish as one undoable gesture")
	}
	if len(apps.events) != events || w.OwnsKeyboard() {
		t.Fatal("workspace resize leaked pointer input or keyboard focus to the client")
	}
	resized := w.Document().View.Application.Layouts[0]
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	restored := desktop(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	reconnected := &fakeApplications{surfaces: []experience.ApplicationSurface{surface}}
	reconnected.surfaces[0].ID = 9040
	restored.SetApplications(reconnected)
	if last := reconnected.resizes[len(reconnected.resizes)-1]; last.width != resized.Width || last.height != resized.Height {
		t.Fatalf("saved custom size was not requested after reconnect: %+v / %+v", last, resized)
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo did not restore window size and workspace state")
	}
	if last := apps.resizes[len(apps.resizes)-1]; last.width != 960 || last.height != 600 {
		t.Fatalf("resize undo did not restore compact provider size: %+v", last)
	}
}

func TestBottomRightGripResizesWithOrdinaryPrimaryDrag(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	w.syncScene()
	root := w.scene.Node(w.applicationNodes[surface.ID])
	x, y, _, _ := w.camera.Project(root.Transform.TransformPoint(scene.Vec3{X: .525, Y: -.525}), w.viewport)
	if target, ok := w.applicationResizeTarget(x, y); !ok || target.ID != surface.ID {
		t.Fatal("bottom-right resize grip was not pickable")
	}
	beforeEvents := len(apps.events)
	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureApplicationResize || w.pointer.resizeButton != 272 {
		t.Fatal("ordinary primary press on resize grip did not start resize")
	}
	pointer(w, experience.PointerMove, x+75, y+45)
	pointer(w, experience.PointerUp, x+75, y+45)
	placement := w.Document().View.Application.Layouts[0]
	if placement.Width <= 960 || placement.Height <= 600 || len(apps.events) != beforeEvents || w.OwnsKeyboard() {
		t.Fatal("primary resize grip did not resize exclusively in the workspace")
	}
}

func projectApplicationCorner(w *Workspace, surface experience.ApplicationSurface, corner scene.Vec3) (float32, float32) {
	w.syncScene()
	point := w.scene.Node(w.applicationNodes[surface.ID]).Transform.TransformPoint(corner)
	x, y, _, _ := w.camera.Project(point, w.viewport)
	return x, y
}

func TestBottomRightResizeKeepsTopLeftAnchored(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	topX, topY := projectApplicationCorner(w, surface, scene.Vec3{X: -.5, Y: .5})
	bottomX, bottomY := projectApplicationCorner(w, surface, scene.Vec3{X: .5, Y: -.5})
	root := w.scene.Node(w.applicationNodes[surface.ID])
	x, y, _, _ := w.camera.Project(root.Transform.TransformPoint(scene.Vec3{X: .525, Y: -.525}), w.viewport)

	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureApplicationResize {
		t.Fatal("bottom-right grip did not start resize")
	}
	const dx, dy = float32(90), float32(55)
	pointer(w, experience.PointerMove, x+dx, y+dy)
	w.Draw(1440, 900)
	afterTopX, afterTopY := projectApplicationCorner(w, surface, scene.Vec3{X: -.5, Y: .5})
	afterBottomX, afterBottomY := projectApplicationCorner(w, surface, scene.Vec3{X: .5, Y: -.5})
	if math.Hypot(float64(afterTopX-topX), float64(afterTopY-topY)) > .05 {
		t.Fatalf("resize moved the anchored top-left corner: before %.3f,%.3f after %.3f,%.3f", topX, topY, afterTopX, afterTopY)
	}
	// Perspective and the grip's small overhang make the projected delta an
	// approximation, but the content corner must follow the whole gesture. A
	// centered resize would move it by only half this distance.
	if math.Abs(float64(afterBottomX-bottomX-dx)) > 15 || math.Abs(float64(afterBottomY-bottomY-dy)) > 15 {
		t.Fatalf("bottom-right did not follow resize pointer: pointer %.1f,%.1f corner %.1f,%.1f", dx, dy, afterBottomX-bottomX, afterBottomY-bottomY)
	}
	pointer(w, experience.PointerUp, x+dx, y+dy)
}

func TestWidePresetKeepsWorldSizeAndFirstResizeIsContinuous(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	compactTransform, _, compactWidth, compactHeight := w.applicationTransformFor(surface)

	command(t, w, Action{Kind: ToggleApplicationSize})
	w.Draw(1440, 900)
	wideTransform, _, wideWidth, wideHeight := w.applicationTransformFor(surface)
	if got := apps.resizes[len(apps.resizes)-1]; got.width != 1440 || got.height != 900 {
		t.Fatalf("Wide preset did not resize the provider: %+v", got)
	}
	if wideTransform != compactTransform || wideWidth != compactWidth || wideHeight != compactHeight {
		t.Fatalf("Wide provider resolution changed world geometry: compact %.3fx%.3f wide %.3fx%.3f", compactWidth, compactHeight, wideWidth, wideHeight)
	}

	root := w.scene.Node(w.applicationNodes[surface.ID])
	x, y, _, _ := w.camera.Project(root.Transform.TransformPoint(scene.Vec3{X: .525, Y: -.525}), w.viewport)
	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureApplicationResize {
		t.Fatal("Wide window resize grip did not capture")
	}
	pointer(w, experience.PointerMove, x+24, y+18)
	w.Draw(1440, 900)
	p := w.Document().View.Application.Layouts[0]
	if p.Width <= 1440 || p.Height <= 900 || !p.Wide || p.Maximized {
		t.Fatalf("first Wide resize did not become a continuous custom Wide size: %+v", p)
	}
	_, _, resizedWidth, resizedHeight := w.applicationTransformFor(surface)
	wantWidth := compactWidth * float32(p.Width) / 1440
	wantHeight := compactHeight * float32(p.Height) / 900
	if math.Abs(float64(resizedWidth-wantWidth)) > .0001 || math.Abs(float64(resizedHeight-wantHeight)) > .0001 {
		t.Fatalf("first Wide resize jumped scale: got %.4fx%.4f want %.4fx%.4f", resizedWidth, resizedHeight, wantWidth, wantHeight)
	}
	if resizedWidth-compactWidth > .5 || resizedHeight-compactHeight > .5 {
		t.Fatalf("small first resize produced a world-size jump: %.4fx%.4f to %.4fx%.4f", compactWidth, compactHeight, resizedWidth, resizedHeight)
	}
	pointer(w, experience.PointerUp, x+24, y+18)
}

func TestWindowResizeCancelRestoresSizeAndDrainsRelease(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	x, y := visibleApplication(t, w, apps.surfaces[0])
	before, history := w.Document(), w.historyPosition
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y})
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x - 500, Y: y - 500})
	if p := w.Document().View.Application.Layouts[0]; p.Width != minApplicationWidth || p.Height != minApplicationHeight {
		t.Fatalf("resize did not clamp to minimum dimensions: %+v", p)
	}
	if !key(w, experience.KeyEscape, 0) || w.Document() != before || w.historyPosition != history {
		t.Fatal("Escape did not cancel resize without history")
	}
	count := len(apps.events)
	if !w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x - 500, Y: y - 500}) || len(apps.events) != count || len(w.windowDragButtons) != 0 {
		t.Fatal("cancelled resize release leaked to the client")
	}
}

func TestWindowResizePreservesIndependentMomentumThroughCheckpointCancelAndUndo(t *testing.T) {
	w, apps := windowThrowWorkspace(t, 2)
	coasting, resized := apps.surfaces[0], apps.surfaces[1]
	startWindowThrow(t, w, coasting)
	w.Update(40 * time.Millisecond)
	original := w.Document().View.Application.Layouts[1]
	x, y := visibleApplication(t, w, resized)

	if !w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y}) {
		t.Fatal("resize did not start while its sibling was coasting")
	}
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 100, Y: y + 60})
	w.Update(100 * time.Millisecond)
	current := w.Document().View.Application
	if current.Layouts[1] == original || !throwingSurface(w, coasting.ID) {
		t.Fatal("fixture did not retain a resize preview and independent momentum")
	}
	coastAtCheckpoint := current.Layouts[0]
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	restored := desktop(t)
	if err = restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	checkpoint := restored.Document().View.Application
	if checkpoint.Layouts[0] != coastAtCheckpoint || checkpoint.Layouts[1] != original {
		t.Fatal("checkpoint rewound sibling momentum or included the held resize")
	}

	if !key(w, experience.KeyEscape, 0) || w.pointer.kind != captureNone {
		t.Fatal("Escape did not cancel the active resize before handling momentum")
	}
	cancelled := w.Document().View.Application
	if cancelled.Layouts[0] != coastAtCheckpoint || cancelled.Layouts[1] != original || !throwingSurface(w, coasting.ID) {
		t.Fatal("resize cancellation rewound or stopped the independently coasting sibling")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x + 100, Y: y + 60}) {
		t.Fatal("cancelled resize did not drain its held button release")
	}
	w.Update(50 * time.Millisecond)
	if w.Document().View.Application.Layouts[0] == coastAtCheckpoint {
		t.Fatal("sibling momentum did not continue after resize cancellation")
	}

	x, y = visibleApplication(t, w, resized)
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, Modifiers: experience.ModSuper, X: x, Y: y})
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 85, Y: y + 45})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x + 85, Y: y + 45})
	resizedPlacement := w.Document().View.Application.Layouts[1]
	if resizedPlacement == original {
		t.Fatal("second resize did not commit")
	}
	w.Update(time.Minute)
	finalCoast := w.Document().View.Application.Layouts[0]
	if w.windowThrow != nil {
		t.Fatal("fixture's independent momentum did not reach rest")
	}
	command(t, w, Action{Kind: Undo})
	afterUndo := w.Document().View.Application
	if afterUndo.Layouts[0] != finalCoast || afterUndo.Layouts[1] != original {
		t.Fatal("resize undo rewound the sibling's final position or failed to restore its target")
	}
}

func TestSuperPrimaryOnResizeGripMovesWindowInsteadOfPanningWorkspace(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	surface := apps.surfaces[0]
	w.syncScene()
	root := w.scene.Node(w.applicationNodes[surface.ID])
	x, y, _, _ := w.camera.Project(root.Transform.TransformPoint(scene.Vec3{X: .525, Y: -.525}), w.viewport)
	if target, ok := w.applicationResizeTarget(x, y); !ok || target.ID != surface.ID {
		t.Fatal("fixture did not hit the resize grip")
	}
	if !w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y}) ||
		w.pointer.kind != captureApplicationPlacement || !w.pointer.windowDrag {
		t.Fatal("Super+primary on a resize grip did not start window movement")
	}
	if w.pointer.kind == captureWorkspacePan {
		t.Fatal("window resize grip was mistaken for empty workspace")
	}
	key(w, experience.KeyEscape, 0)
}
