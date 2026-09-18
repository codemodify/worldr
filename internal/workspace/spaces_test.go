package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/scene"
)

func TestNamedSpacesRetainIndependentContentsCamerasAndFocus(t *testing.T) {
	w, apps, _ := nativeMeshFixture(t)
	if err := w.Dispatch(Action{Kind: CreateSpace, SpaceName: "Research"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Dispatch(Action{Kind: MoveToSpace, Space: 1}); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if !w.scene.Node(w.applicationNodes[1]).Hidden || w.scene.Node(w.applicationNodes[2]).Hidden {
		t.Fatal("moving model to another space did not isolate contents")
	}
	if err := w.Dispatch(Action{Kind: OrbitCamera, DeltaX: 12, DeltaY: 6}); err != nil {
		t.Fatal(err)
	}
	mainCamera := w.Document().View.Camera
	if err := w.Dispatch(Action{Kind: SwitchSpace, Space: 1}); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.scene.Node(w.applicationNodes[1]).Hidden || !w.scene.Node(w.applicationNodes[2]).Hidden || w.OwnsKeyboard() {
		t.Fatal("switching space mixed visibility or typing focus")
	}
	if err := w.ActivateApplication("native:model"); err != nil {
		t.Fatal(err)
	}
	if !w.OwnsKeyboard() || apps.focus[len(apps.focus)-1] != 1 {
		t.Fatal("explicit tool activation did not focus model")
	}
	if err := w.Dispatch(Action{Kind: OrbitCamera, DeltaX: -35, DeltaY: 10}); err != nil {
		t.Fatal(err)
	}
	researchCamera := w.Document().View.Camera
	if err := w.Dispatch(Action{Kind: SwitchSpace, Space: 0}); err != nil {
		t.Fatal(err)
	}
	if w.Document().View.Camera != mainCamera || w.OwnsKeyboard() {
		t.Fatal("return lost saved camera or leaked application focus")
	}
	if err := w.Dispatch(Action{Kind: SwitchSpace, Space: 1}); err != nil {
		t.Fatal(err)
	}
	if w.Document().View.Camera != researchCamera {
		t.Fatal("research camera was replaced by main camera")
	}
	data, err := w.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if restored.Document() != w.Document() {
		t.Fatal("spaces changed during session serialization")
	}
}

func TestSpaceTransfersAreUndoableAndKeepGroupTogether(t *testing.T) {
	w, _, _ := nativeMeshFixture(t)
	if err := w.Dispatch(Action{Kind: CreateSpace, SpaceName: "Build"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Action{{Kind: SelectApplication, ApplicationKey: "native:terminal", Additive: true}, {Kind: GroupApplications}, {Kind: MoveToSpace, Space: 1}} {
		if err := w.Dispatch(a); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range w.Document().View.Application.Layouts {
		if p.Key != "" && p.Space != 1 {
			t.Fatal("group split across spaces")
		}
	}
	if err := w.Dispatch(Action{Kind: Undo}); err != nil {
		t.Fatal(err)
	}
	for _, p := range w.Document().View.Application.Layouts {
		if p.Key != "" && p.Space != 0 {
			t.Fatal("undo did not return group")
		}
	}
	before := w.Document()
	for _, a := range []Action{{Kind: CreateSpace, SpaceName: "main"}, {Kind: RenameSpace, Space: 1, SpaceName: "\n"}, {Kind: SwitchSpace, Space: 15}, {Kind: MoveToSpace, Space: 15}} {
		if err := w.Dispatch(a); err == nil {
			t.Fatal("accepted invalid space action", a)
		}
		if w.Document() != before {
			t.Fatal("invalid action mutated document")
		}
	}
}

func TestNativeSpatialMeshesRespectOtherWindowOcclusion(t *testing.T) {
	w, apps, _ := nativeMeshFixture(t)
	if err := w.Dispatch(Action{Kind: ToggleApplicationReading}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w.m.applicationState.Layouts[i].X = 0
		w.m.applicationState.Layouts[i].Y = 0
		w.m.applicationState.Layouts[i].Depth = float32(i) * 2
	}
	w.Draw(1440, 900)
	point := w.spatialApplicationTransform(apps.surfaces[0]).TransformPoint(scene.Vec3{Z: .2})
	x, y, _, _ := w.camera.Project(point, w.viewport)
	hit, ok := w.applicationHit(x, y)
	if !ok || w.applicationForNode(hit.Node).ID != 2 {
		t.Fatal("native mesh picked through an opaque foreground window")
	}
	w.m.applicationState.Layouts[0].Depth = 3
	w.Draw(1440, 900)
	point = w.spatialApplicationTransform(apps.surfaces[0]).TransformPoint(scene.Vec3{Z: .2})
	x, y, _, _ = w.camera.Project(point, w.viewport)
	hit, ok = w.applicationHit(x, y)
	if !ok || w.applicationForNode(hit.Node).ID != 1 || hit.Surface {
		t.Fatal("foreground native mesh lost picking to image plane")
	}
}
