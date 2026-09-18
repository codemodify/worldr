package workspace

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func TestCameraWheelChangesDistanceAndPersistsWithoutMovingContent(t *testing.T) {
	w := study(t)
	w.Draw(1440, 900)
	before, eye := w.Document(), w.camera.Eye
	assembly := w.scene.Node(w.nodes[1]).Transform
	panel := w.scene.Node(w.panelNode).Transform
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, X: 350, Y: 220, ScrollY: -15}) {
		t.Fatal("scene wheel did not zoom")
	}
	w.Draw(1440, 900)
	if w.camera.Eye.Length() >= eye.Length() || w.State().Zoom <= 0 || w.scene.Node(w.nodes[1]).Transform != assembly || w.scene.Node(w.panelNode).Transform != panel {
		t.Fatal("zoom did not change only the camera distance")
	}
	after := w.Document()
	after.View.Camera.Zoom = 0
	if after != before {
		t.Fatal("camera zoom changed other saved state")
	}
	data, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	restored := study(t)
	if err := restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	restored.Draw(1440, 900)
	if restored.Document() != w.Document() || restored.camera != w.camera {
		t.Fatal("zoom did not survive document restoration")
	}
	w.Update(time.Second)
	clock := w.State().Time
	command(t, w, Action{Kind: Undo})
	if w.State().Zoom != 0 || w.State().Time != clock {
		t.Fatal("undo zoom rewound unrelated playback")
	}
	command(t, w, Action{Kind: Redo})
	if w.State().Zoom <= 0 {
		t.Fatal("redo did not restore zoom")
	}
	command(t, w, Action{Kind: ResetView})
	if w.State().Zoom != 0 {
		t.Fatal("reset view did not restore camera distance")
	}
}

func TestCameraZoomBoundsAndInvalidStateAreTransactional(t *testing.T) {
	w := study(t)
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: math.MaxFloat32})
	if w.State().Zoom != .8 {
		t.Fatal("zoom in did not clamp")
	}
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: -math.MaxFloat32})
	if w.State().Zoom != -.8 {
		t.Fatal("zoom out did not clamp")
	}
	w.Draw(1440, 900)
	pointer(w, experience.PointerDown, 350, 220)
	pointer(w, experience.PointerMove, 370, 230)
	before, gesture, history := w.Document(), w.pointer, len(w.history)
	for _, invalid := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		if err := w.Dispatch(Action{Kind: ZoomCamera, DeltaZoom: invalid}); err == nil {
			t.Fatal("accepted invalid zoom action")
		}
		bad := before
		bad.View.Camera.Zoom = invalid
		if err := bad.Validate(); err == nil {
			t.Fatal("accepted invalid saved camera zoom")
		}
	}
	bad := before
	bad.View.Camera.Zoom = 1
	data, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.LoadState(data); err == nil {
		t.Fatal("accepted out-of-range saved camera zoom")
	}
	if w.Document() != before || w.pointer != gesture || len(w.history) != history {
		t.Fatal("invalid zoom modified state or committed an active gesture")
	}
}

func TestCameraWheelLeavesApplicationAndPlacementScrollAlone(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	sx, sy := visibleApplication(t, w, apps.surfaces[0])
	event := experience.Event{Kind: experience.PointerScroll, X: sx, Y: sy, ScrollY: -15}
	if !w.Handle(event) || w.State().Zoom != 0 || len(apps.events) == 0 || apps.events[len(apps.events)-1].event.Kind != experience.PointerScroll {
		t.Fatal("application wheel was stolen by camera zoom")
	}
	command(t, w, Action{Kind: ToggleApplicationPlacement})
	w.Draw(1440, 900)
	sx, sy = visibleApplication(t, w, apps.surfaces[0])
	event.X, event.Y = sx, sy
	before := w.Document().View.Application
	if !w.Handle(event) || w.State().Zoom != 0 || before == w.Document().View.Application {
		t.Fatal("placement wheel did not move the application independently")
	}
	for _, action := range []ActionKind{ToggleApplicationReading, ToggleApplicationOverview} {
		command(t, w, Action{Kind: action})
		w.Draw(1440, 900)
		w.Handle(experience.Event{Kind: experience.PointerScroll, X: 260, Y: 180, ScrollY: -15})
		if w.State().Zoom != 0 {
			t.Fatal("camera zoom changed in reading or overview mode")
		}
	}
}

func TestCameraWheelIgnoresControlsModifiersAndActiveDrag(t *testing.T) {
	w := study(t)
	w.Draw(2880, 1800)
	for _, event := range []experience.Event{
		{Kind: experience.PointerScroll, X: 100, Y: 100, ScrollY: -15},
		{Kind: experience.PointerScroll, X: 700, Y: 440, ScrollY: -15, Modifiers: experience.ModControl},
		{Kind: experience.PointerScroll, X: 700, Y: 440, ScrollX: 15},
		{Kind: experience.PointerScroll, X: 700, Y: 440, ScrollY: float32(math.NaN())},
	} {
		if w.Handle(event) || w.State().Zoom != 0 {
			t.Fatal("unrelated scroll changed camera zoom")
		}
	}
	pointer(w, experience.PointerDown, 700, 440)
	if w.Handle(experience.Event{Kind: experience.PointerScroll, X: 700, Y: 440, ScrollY: -15}) || w.State().Zoom != 0 {
		t.Fatal("wheel interrupted an active gesture")
	}
	pointer(w, experience.PointerUp, 700, 440)
	if !w.Handle(experience.Event{Kind: experience.PointerScroll, X: 700, Y: 440, ScrollY: -15}) || w.State().Zoom <= 0 {
		t.Fatal("scaled scene wheel did not zoom")
	}
}
