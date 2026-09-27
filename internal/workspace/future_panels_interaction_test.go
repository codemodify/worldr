package workspace

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// Use the shipped layout, actual skin geometry, scene projection and input
// router. The provider records delivered input without starting five programs:
// these tests cover workspace manipulation rather than application rendering.
func futurePanelsWorkspace(t *testing.T) (*Workspace, *fakeApplications) {
	t.Helper()
	data, err := os.ReadFile("../../examples/future-panels/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	w := desktop(t)
	if err := w.LoadState(envelope.State); err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(testWindowSkin(t, "future-panels")); err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i, placement := range w.Document().View.Application.Layouts {
		if placement.Key == "" {
			continue
		}
		texture, err := render.NewTexture(placement.Width, placement.Height, make([]byte, placement.Width*placement.Height*4))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = texture.Close() })
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{
			ID: uint64(100 + i), Key: placement.Key, Title: placement.Key,
			Texture: texture, FrameStyle: experience.FrameCinematic,
		})
	}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	return w, apps
}

func futurePanelsSurface(t *testing.T, apps *fakeApplications, key string) experience.ApplicationSurface {
	t.Helper()
	for _, surface := range apps.surfaces {
		if surface.Key == key {
			return surface
		}
	}
	t.Fatalf("starter application %q is absent", key)
	return experience.ApplicationSurface{}
}

func futurePanelsGrip(t *testing.T, w *Workspace, surface experience.ApplicationSurface) (float32, float32) {
	t.Helper()
	w.Draw(1440, 900)
	grip := w.skinWindowMeshes(surface).layout.grip
	x, y := windowSkinPoint(t, w, surface, grip, .5, .5)
	if hit, ok := w.applicationDragTarget(x, y); !ok || hit.ID != surface.ID {
		t.Fatalf("authored grip for %s is obscured or not draggable", surface.Key)
	}
	return x, y
}

func TestFuturePanelsPresetMakesAllFiveApplicationsVisibleAndInteractive(t *testing.T) {
	w, apps := futurePanelsWorkspace(t)
	for _, key := range []string{"native:project-browser", "native:terminal", "native:research-workbench", "native:model", "native:axial-07"} {
		surface := futurePanelsSurface(t, apps, key)
		node := w.scene.Node(w.applicationNodes[surface.ID])
		if node == nil || node.Hidden {
			t.Fatalf("starter application %s is absent or hidden", key)
		}
		found := false
		// Search the actual projected content aperture. A point behind another
		// window or outside the viewport cannot count as a visible application.
		for localY := float32(.4); localY >= -.4 && !found; localY -= .1 {
			for localX := float32(-.4); localX <= .4 && !found; localX += .1 {
				world := node.Transform.TransformPoint(scene.Vec3{X: localX, Y: localY})
				x, y, _, visible := w.camera.Project(world, w.viewport)
				if !visible || x < w.viewport.X || x >= w.viewport.X+w.viewport.Width || y < w.viewport.Y || y >= w.viewport.Y+w.viewport.Height {
					continue
				}
				hit, ok := w.applicationHit(x, y)
				if !ok || hit.Node != w.applicationNodes[surface.ID] {
					continue
				}
				before := w.Document().View.Application.Layouts
				start := len(apps.events)
				pointer(w, experience.PointerDown, x, y)
				pointer(w, experience.PointerMove, x+1, y+1)
				pointer(w, experience.PointerUp, x+1, y+1)
				var pressed, released bool
				for _, delivered := range apps.events[start:] {
					if delivered.id == surface.ID {
						pressed = pressed || delivered.event.Kind == experience.PointerDown
						released = released || delivered.event.Kind == experience.PointerUp
					}
				}
				if !pressed || !released || w.applicationFocusedID != surface.ID || w.Document().View.Application.Layouts != before {
					t.Fatalf("normal content input for %s was intercepted or moved a window", key)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("starter application %s has no unobscured content inside the viewport", key)
		}
	}
}

func TestFuturePanelsGripThrowRegrabAndSingleUndo(t *testing.T) {
	w, apps := futurePanelsWorkspace(t)
	surface := futurePanelsSurface(t, apps, "native:terminal")
	index := w.Document().View.Application.index(surface.Key)
	before := w.Document()
	x, y := futurePanelsGrip(t, w, surface)
	timedWindowGestureAt(t, w, x, y, [4]uint32{1000, 1020, 1040, 1050})
	released := w.Document().View.Application.Layouts[index]
	w.Update(150 * time.Millisecond)
	coasted := w.Document().View.Application.Layouts[index]
	if coasted.X <= released.X || coasted == before.View.Application.Layouts[index] {
		t.Fatal("releasing the Future Panels grip did not coast in its release direction")
	}
	if w.Document().View.Camera != before.View.Camera || len(apps.events) != 0 {
		t.Fatal("grip throw changed the camera or leaked pointer events to the application")
	}
	x, y = futurePanelsGrip(t, w, surface)
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2000},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 2020},
	} {
		if !w.Handle(event) {
			t.Fatal("the coasting window could not be grabbed and released")
		}
	}
	stopped := w.Document()
	w.Update(time.Second)
	if w.Document() != stopped || stopped.View.Application.Layouts[index] != coasted {
		t.Fatal("regrabbing failed to stop the throw at its current position")
	}
	for i, placement := range stopped.View.Application.Layouts {
		if i != index && placement != before.View.Application.Layouts[i] {
			t.Fatal("throw or regrab moved another starter application")
		}
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo did not restore the grip drag and complete coast")
	}
	command(t, w, Action{Kind: Redo})
	w.Update(time.Second)
	if w.Document() != stopped {
		t.Fatal("redo did not preserve the stopped position or restarted its motion")
	}
}

func TestFuturePanelsHeldDepthScrollCoastsAndRemainsUndoable(t *testing.T) {
	w, apps := futurePanelsWorkspace(t)
	surface := futurePanelsSurface(t, apps, "native:terminal")
	index := w.Document().View.Application.index(surface.Key)
	before := w.Document()
	x, y := futurePanelsGrip(t, w, surface)
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1000},
		{Kind: experience.PointerScroll, X: x, Y: y, ScrollY: 18, Time: 1020},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y, Time: 1040},
	} {
		if !w.Handle(event) {
			t.Fatal("held depth-wheel input was not consumed by the window grip")
		}
	}
	released := w.Document().View.Application.Layouts[index]
	w.Update(80 * time.Millisecond)
	coasted := w.Document().View.Application.Layouts[index]
	if released.Depth >= before.View.Application.Layouts[index].Depth || coasted.Depth >= released.Depth {
		t.Fatal("depth-wheel release failed to coast rearward")
	}
	if abs(coasted.X-released.X) > .0001 || abs(coasted.Y-released.Y) > .0001 || len(apps.events) != 0 {
		t.Fatal("depth movement added lateral displacement or leaked input to the application")
	}
	w.Update(10 * time.Second)
	after := w.Document()
	if after.View.Camera != before.View.Camera || after.Validate() != nil {
		t.Fatal("depth throw changed the camera or left an invalid resting placement")
	}
	command(t, w, Action{Kind: Undo})
	if w.Document() != before {
		t.Fatal("one undo did not restore the held depth scroll and coast")
	}
	command(t, w, Action{Kind: Redo})
	w.Update(time.Second)
	if w.Document() != after {
		t.Fatal("redo replayed depth momentum instead of restoring its result")
	}
}
