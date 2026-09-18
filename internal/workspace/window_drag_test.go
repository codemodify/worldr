package workspace

import (
	"fmt"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func windowDragWorkspace(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w := desktop(t)
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := 0; i < count; i++ {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{ID: uint64(40 + i), Key: fmt.Sprintf("drag:%d", i), Texture: texture})
	}
	w.SetApplications(apps)
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	w.Draw(1440, 900)
	return w, apps
}

func windowGripPoint(t *testing.T, w *Workspace, surface experience.ApplicationSurface) (float32, float32) {
	t.Helper()
	w.syncScene()
	transform := w.scene.Node(w.applicationNodes[surface.ID]).Transform
	for _, y := range []float32{.535, .565} {
		for _, x := range []float32{0, -.08, .08} {
			px, py, _, _ := w.camera.Project(transform.TransformPoint(scene.Vec3{X: x, Y: y}), w.viewport)
			if hit, ok := w.applicationDragTarget(px, py); ok && hit.ID == surface.ID {
				return px, py
			}
		}
	}
	t.Fatalf("window %q has no visible grip", surface.Key)
	return 0, 0
}

func TestWindowDragGripSelectsAndMovesAsOneUndo(t *testing.T) {
	w, apps := windowDragWorkspace(t, 2)
	x, y := windowGripPoint(t, w, apps.surfaces[1])
	before, history := w.Document(), w.historyPosition
	if !pointer(w, experience.PointerDown, x, y) || !w.pointer.windowDrag || w.OwnsKeyboard() {
		t.Fatal("grip did not start workspace drag without client focus")
	}
	for _, d := range []float32{10, 25, 50} {
		pointer(w, experience.PointerMove, x+d, y+d/2)
	}
	pointer(w, experience.PointerUp, x+50, y+25)
	after := w.Document()
	if after.View.Application.Layouts[1].X == before.View.Application.Layouts[1].X || after.View.Application.Active != apps.surfaces[1].Key {
		t.Fatal("grip failed to move and select its own window")
	}
	if after.View.Application.Layouts[0] != before.View.Application.Layouts[0] || after.View.Camera != before.View.Camera || len(apps.events) != 0 {
		t.Fatal("direct drag changed unrelated placement/camera or sent client input")
	}
	if w.historyPosition != history+1 || w.pointer.kind != captureNone {
		t.Fatal("direct drag did not finish as a single history entry")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo failed to restore placement and the previous selection together")
	}
}

func TestWindowDragOnlyExactSuperReservesContent(t *testing.T) {
	for _, mods := range []experience.Modifiers{0, experience.ModAlt, experience.ModSuper | experience.ModShift, experience.ModSuper} {
		t.Run(fmt.Sprint(mods), func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			x, y := visibleApplication(t, w, apps.surfaces[0])
			before := w.Document()
			w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Modifiers: mods})
			pointer(w, experience.PointerMove, x+35, y+10)
			pointer(w, experience.PointerUp, x+35, y+10)
			if mods == experience.ModSuper {
				if len(apps.events) != 0 || w.Document().View.Application.Layouts == before.View.Application.Layouts || w.OwnsKeyboard() {
					t.Fatal("exact Super+primary did not reserve the whole move gesture")
				}
			} else {
				if len(apps.events) < 3 || apps.events[0].event.Kind != experience.PointerDown || apps.events[0].event.Modifiers != mods || !w.OwnsKeyboard() {
					t.Fatal("ordinary client pointer gesture was intercepted or modified")
				}
				if w.Document().View.Application.Layouts != before.View.Application.Layouts {
					t.Fatal("client pointer input moved a workspace window")
				}
			}
		})
	}
}

func TestWindowDragGroupDepthAndExtraButtonStayOneGesture(t *testing.T) {
	w, apps := windowDragWorkspace(t, 2)
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
	command(t, w, Action{Kind: GroupApplications})
	x, y := windowGripPoint(t, w, apps.surfaces[1])
	before, history := w.Document(), w.historyPosition
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+35, y+15)
	preDepth := w.Document().View.Application.Layouts
	w.Handle(experience.Event{Kind: experience.PointerScroll, X: x + 35, Y: y + 15, ScrollY: 8})
	pointer(w, experience.PointerMove, x+35, y+15)
	afterDepth := w.Document().View.Application.Layouts
	for i := 0; i < 2; i++ {
		if abs(afterDepth[i].X-preDepth[i].X) > .0001 || abs(afterDepth[i].Y-preDepth[i].Y) > .0001 || abs(afterDepth[i].Depth-preDepth[i].Depth+.28) > .0001 {
			t.Fatal("depth wheel introduced lateral movement or failed to move the group")
		}
	}
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, X: x + 35, Y: y + 15})
	pointer(w, experience.PointerUp, x+35, y+15)
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x + 35, Y: y + 15})
	after := w.Document()
	a, b := before.View.Application.Layouts, after.View.Application.Layouts
	if math.Abs(float64((b[0].X-a[0].X)-(b[1].X-a[1].X))) > .0001 || math.Abs(float64((b[0].Y-a[0].Y)-(b[1].Y-a[1].Y))) > .0001 {
		t.Fatal("group members did not retain their relative placement")
	}
	if w.historyPosition != history+1 || len(apps.events) != 0 || len(w.windowDragButtons) != 0 || after.View.Camera != before.View.Camera {
		t.Fatal("depth/group drag leaked extra buttons, changed camera, or split its history")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("undo did not restore all dimensions of the group move")
	}
}

func TestWindowDragHandleRespectsSceneOcclusion(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	parent := w.scene.Node(w.applicationNodes[apps.surfaces[0].ID])
	blocker := w.scene.Add(0, scene.Node{Surface: apps.surfaces[0].Texture, Transform: parent.Transform.Mul(scene.Translate(0, .55, .1))})
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); !ok || hit.Node != blocker {
		t.Fatal("test foreground surface did not occlude the grip")
	}
	if _, ok := w.applicationDragTarget(x, y); ok {
		t.Fatal("an occluded handle remained draggable through foreground content")
	}
	w.scene.Remove(blocker)
	if surface, ok := w.applicationDragTarget(x, y); !ok || surface.ID != apps.surfaces[0].ID {
		t.Fatal("removing the occluder did not restore handle picking")
	}
}

func TestWindowDragCancellationDrainsHeldStroke(t *testing.T) {
	for _, reason := range []string{"escape", "resize", "close", "help"} {
		t.Run(reason, func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 2)
			target := apps.surfaces[1]
			x, y := windowGripPoint(t, w, target)
			before, history := w.Document(), w.historyPosition
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerMove, x+40, y+20)
			w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, X: x + 40, Y: y + 20})
			switch reason {
			case "escape":
				key(w, experience.KeyEscape, 0)
			case "resize":
				w.Draw(1200, 800)
				pointer(w, experience.PointerMove, x+40, y+20)
			case "close":
				apps.surfaces = apps.surfaces[:1]
				w.Update(0)
			case "help":
				key(w, experience.KeyF1, 0)
				w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyF1})
				key(w, experience.KeyEscape, 0)
			}
			if w.pointer.kind != captureNone || w.Document().View.Application.Layouts != before.View.Application.Layouts || w.historyPosition != history {
				t.Fatal("cancelled drag kept preview placement or recorded history")
			}
			w.Draw(w.width, w.height)
			cx, cy := visibleApplication(t, w, apps.surfaces[0])
			count := len(apps.events)
			pointer(w, experience.PointerMove, cx, cy)
			pointer(w, experience.PointerUp, cx, cy)
			w.Handle(experience.Event{Kind: experience.PointerScroll, X: cx, Y: cy, ScrollY: 8})
			w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: cx, Y: cy})
			if len(apps.events) != count || len(w.windowDragButtons) != 0 {
				t.Fatal("cancelled stroke leaked to an application or retained button ownership")
			}
			pointer(w, experience.PointerDown, cx, cy)
			pointer(w, experience.PointerUp, cx, cy)
			if !w.OwnsKeyboard() || len(apps.events) <= count {
				t.Fatal("draining a cancelled stroke blocked the next fresh client click")
			}
		})
	}
}

func TestWindowDragHandlesHideInReadingAndOverview(t *testing.T) {
	for _, action := range []ActionKind{ToggleApplicationReading, ToggleApplicationOverview} {
		t.Run(string(action), func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			command(t, w, Action{Kind: action})
			w.Draw(1440, 900)
			if !w.scene.Node(w.applicationDragHandles[apps.surfaces[0].ID]).Hidden {
				t.Fatal("reading or overview retained a spatial drag handle")
			}
			x, y := visibleApplication(t, w, apps.surfaces[0])
			w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModSuper, X: x, Y: y})
			if w.pointer.windowDrag {
				t.Fatal("reading or overview started a direct spatial drag")
			}
		})
	}
}

func TestWindowDragCancelOwnsEscapeThroughRefocus(t *testing.T) {
	w, apps := windowDragWorkspace(t, 1)
	x, y := windowGripPoint(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x+30, y+10)
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
	pointer(w, experience.PointerUp, x+30, y+10)
	cx, cy := visibleApplication(t, w, apps.surfaces[0])
	pointer(w, experience.PointerDown, cx, cy)
	pointer(w, experience.PointerUp, cx, cy)
	count := len(apps.events)
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true, Repeat: true})
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1})
	if len(apps.events) != count {
		t.Fatal("drag cancellation forwarded held Escape repeat/release to a newly focused client")
	}
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
	if len(apps.events) != count+1 {
		t.Fatal("drag cancellation swallowed a fresh client Escape stroke")
	}
}
