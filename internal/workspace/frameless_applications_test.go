package workspace

import (
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func photoDragWorkspace(t *testing.T, count int) (*Workspace, *fakeApplications, experience.ApplicationSurface) {
	t.Helper()
	w, apps := windowDragWorkspace(t, count)
	photo := &apps.surfaces[count-1]
	photo.Frameless, photo.DragContent, photo.AppID, photo.Title = true, true, "worldr.photo-viewer", "Photo / specimen.png"
	w.Update(0)
	w.Draw(1440, 900)
	return w, apps, *photo
}

func TestFramelessPhotoHasNoFrameOrGripAcrossViewsAndRestoresFlags(t *testing.T) {
	for _, mode := range []string{"space", "read", "overview"} {
		t.Run(mode, func(t *testing.T) {
			w, apps, photo := photoDragWorkspace(t, 1)
			if mode == "read" {
				command(t, w, Action{Kind: ToggleApplicationReading})
			} else if mode == "overview" {
				command(t, w, Action{Kind: ToggleApplicationOverview})
			}
			frameID, gripID := w.applicationFrames[photo.ID], w.applicationDragHandles[photo.ID]
			frame := w.Draw(1440, 900)
			border, grip := w.scene.Node(frameID), w.scene.Node(gripID)
			if !border.Hidden || !grip.Hidden || border.Glow != [3]float32{} || len(frameDraws(w, frame)) != 0 {
				t.Fatal("frameless photo retained a border, halo or drag grip")
			}
			apps.surfaces[0].Frameless, apps.surfaces[0].DragContent = false, false
			w.Update(0)
			frame = w.Draw(1440, 900)
			if w.applicationFrames[photo.ID] != frameID || w.applicationDragHandles[photo.ID] != gripID || border.Hidden || len(frameDraws(w, frame)) != 1 || grip.Hidden != (mode != "space") {
				t.Fatal("changing surface flags failed to restore the retained ordinary window frame")
			}
		})
	}
}

func TestFramelessPhotoSpatialSizeBoundsPreserveAspect(t *testing.T) {
	for _, size := range [][2]int{{120, 800}, {800, 120}, {200, 200}} {
		w, apps, photo := photoDragWorkspace(t, 1)
		texture, err := render.NewTexture(size[0], size[1], make([]byte, size[0]*size[1]*4))
		if err != nil {
			t.Fatal(err)
		}
		apps.surfaces[0].Texture = texture
		w.Update(0)
		photo = w.applicationSurfaces[0]
		_, _, width, height := w.applicationTransformFor(photo)
		if width > 4.6001 || height > 3.0001 || math.Abs(float64(width/height)-float64(size[0])/float64(size[1])) > .0001 {
			t.Fatalf("photo escaped its spatial box or changed aspect: %dx%d -> %gx%g", size[0], size[1], width, height)
		}
		if err := w.ActivateApplication(photo.Key); err != nil {
			t.Fatal(err)
		}
		w.Draw(1440, 900)
		if w.OwnsKeyboard() {
			t.Fatal("passive photo acquired application keyboard ownership")
		}
		for _, corner := range []scene.Vec3{{X: -.5, Y: -.5}, {X: .5, Y: .5}} {
			x, y, _, visible := w.camera.Project(w.scene.Node(w.applicationNodes[photo.ID]).Transform.TransformPoint(corner), w.viewport)
			if !visible || x < w.viewport.X || x > w.viewport.X+w.viewport.Width || y < w.viewport.Y || y > w.viewport.Y+w.viewport.Height {
				t.Fatal("Read framing cropped a tall or wide photo")
			}
		}
		if size[1] > size[0] {
			photo.Frameless, photo.DragContent = false, false
			_, _, _, ordinaryHeight := w.applicationTransformFor(photo)
			if ordinaryHeight <= 3 {
				t.Fatal("photo size bounds changed ordinary application sizing")
			}
		}
	}
}

func TestPhotoContentDragSelectsMovesAndUndoesWithoutClientInput(t *testing.T) {
	w, apps, photo := photoDragWorkspace(t, 2)
	x, y := visibleApplication(t, w, photo)
	before, history := w.Document(), w.historyPosition
	w.Handle(experience.Event{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 12})
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonSecondary, X: x, Y: y})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: x, Y: y})
	if w.Document() != before || len(apps.events) != 0 {
		t.Fatal("photo wheel/right-click changed content, view or client input")
	}
	pointer(w, experience.PointerDown, x, y)
	if !w.pointer.windowDrag || w.OwnsKeyboard() {
		t.Fatal("primary photo press did not start a workspace-owned content drag")
	}
	pointer(w, experience.PointerMove, x+48, y+24)
	pointer(w, experience.PointerUp, x+48, y+24)
	after := w.Document()
	if after.View.Application.Active != photo.Key || after.View.Application.Layouts[1] == before.View.Application.Layouts[1] || after.View.Application.Layouts[0] != before.View.Application.Layouts[0] || after.View.Camera != before.View.Camera || len(apps.events) != 0 || w.historyPosition != history+1 {
		t.Fatal("photo drag did not stay a single isolated selection/placement edit")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("photo drag Undo failed to restore placement and selection")
	}
	command(t, w, Action{Kind: Redo})
	if w.Document() != after {
		t.Fatal("photo drag Redo failed to restore its final placement")
	}
}

func TestPhotoDragFromReadAndOverviewPreservesGrabAndOneUndo(t *testing.T) {
	for _, mode := range []ActionKind{ToggleApplicationReading, ToggleApplicationOverview} {
		t.Run(string(mode), func(t *testing.T) {
			w, apps, photo := photoDragWorkspace(t, 1)
			command(t, w, Action{Kind: mode})
			w.Draw(1440, 900)
			local := scene.Vec3{X: -.21, Y: .13}
			x, y, _, _ := w.camera.Project(w.scene.Node(w.applicationNodes[photo.ID]).Transform.TransformPoint(local), w.viewport)
			before, history := w.Document(), w.historyPosition
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerMove, x+1, y+1)
			pointer(w, experience.PointerUp, x+1, y+1)
			if w.Document() != before || w.historyPosition != history {
				t.Fatal("a click or pointer tremor moved the photo out of its current view")
			}
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerMove, x+55, y+22)
			w.Draw(1440, 900)
			if w.m.applicationState.Reading || w.m.applicationState.Overview || !w.pointer.windowDrag {
				t.Fatal("photo drag did not transition from its view into Space")
			}
			px, py, _, _ := w.camera.Project(w.scene.Node(w.applicationNodes[photo.ID]).Transform.TransformPoint(local), w.viewport)
			if abs(px-x-55) > .1 || abs(py-y-22) > .1 {
				t.Fatalf("view transition lost the grabbed photo point: delta %g/%g", px-x-55, py-y-22)
			}
			camera := w.camera
			pointer(w, experience.PointerMove, x+85, y+32)
			pointer(w, experience.PointerUp, x+85, y+32)
			w.Draw(1440, 900)
			if w.camera != camera || w.historyPosition != history+1 || len(apps.events) != 0 {
				t.Fatal("photo drag moved its camera, leaked client input or split Undo")
			}
			after := w.Document()
			command(t, w, Action{Kind: Undo})
			if w.Document() != before {
				t.Fatal("Undo failed to restore both the prior photo view and its placement")
			}
			command(t, w, Action{Kind: Redo})
			if w.Document() != after {
				t.Fatal("Redo failed to restore the photo's Space placement")
			}
		})
	}
}

func TestPhotoContentDragCancellationAndOcclusion(t *testing.T) {
	for _, reason := range []string{"escape", "resize", "close"} {
		t.Run(reason, func(t *testing.T) {
			w, apps, photo := photoDragWorkspace(t, 2)
			if err := w.ActivateApplication(photo.Key); err != nil {
				t.Fatal(err)
			}
			w.Draw(1440, 900)
			x, y := visibleApplication(t, w, photo)
			before, history := w.Document(), w.historyPosition
			pointer(w, experience.PointerDown, x, y)
			pointer(w, experience.PointerMove, x+50, y+15)
			switch reason {
			case "escape":
				key(w, experience.KeyEscape, 0)
			case "resize":
				w.Draw(1200, 800)
				pointer(w, experience.PointerMove, x+50, y+15)
			case "close":
				apps.surfaces = apps.surfaces[:1]
				w.Update(0)
			}
			if w.pointer.kind != captureNone || w.Document().View.Application.Layouts != before.View.Application.Layouts || w.historyPosition != history || !w.m.applicationState.Reading {
				t.Fatal("canceling a photo drag retained its placement or lost the original Read view")
			}
			count := len(apps.events)
			pointer(w, experience.PointerUp, x+50, y+15)
			if len(apps.events) != count || len(w.windowDragButtons) != 0 {
				t.Fatal("canceled photo release reached an application or kept pointer ownership")
			}
		})
	}
	w, apps, photo := photoDragWorkspace(t, 1)
	x, y := visibleApplication(t, w, photo)
	parent := w.scene.Node(w.applicationNodes[photo.ID])
	blocker := w.scene.Add(0, scene.Node{Surface: photo.Texture, Transform: parent.Transform.Mul(scene.Translate(0, 0, .1))})
	pointer(w, experience.PointerDown, x, y)
	if w.pointer.windowDrag || len(apps.events) != 0 {
		t.Fatal("photo content was grabbed through a foreground surface")
	}
	pointer(w, experience.PointerUp, x, y)
	w.scene.Remove(blocker)
	pointer(w, experience.PointerDown, x, y)
	if !w.pointer.windowDrag {
		t.Fatal("removing the occluder did not restore photo content dragging")
	}
	pointer(w, experience.PointerUp, x, y)
}

func TestPhotoContentThrowSharesViewTransitionUndo(t *testing.T) {
	w, apps, photo := photoDragWorkspace(t, 1)
	command(t, w, Action{Kind: SetReducedMotion, Enabled: false})
	command(t, w, Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	x, y := visibleApplication(t, w, photo)
	before, history := w.Document(), w.historyPosition
	timedWindowGestureAt(t, w, x, y, [4]uint32{1000, 1020, 1040, 1050})
	if w.windowThrow == nil || w.m.applicationState.Reading || w.historyPosition != history+1 {
		t.Fatal("moving photo release did not reserve one throw/view-transition edit")
	}
	released := w.Document().View.Application.Layouts
	w.Update(200 * time.Millisecond)
	if w.Document().View.Application.Layouts == released || len(apps.events) != 0 {
		t.Fatal("photo did not coast independently after release")
	}
	w.Update(5 * time.Second)
	if w.windowThrow != nil || w.historyPosition != history+1 {
		t.Fatal("photo throw did not settle into the same undo entry")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("Undo of completed throw did not restore photo placement and Read view")
	}
}
