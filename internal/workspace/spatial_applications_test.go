package workspace

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func nativeMeshFixture(t *testing.T) (*Workspace, *fakeApplications, *scene.Mesh) {
	t.Helper()
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	mesh, err := scene.NewMesh([]scene.Vec3{{X: -.15, Y: -.1}, {X: .15, Y: -.1}, {Y: .15}}, []uint32{0, 1, 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{surfaces: []experience.ApplicationSurface{{ID: 1, Key: "native:model", Texture: texture, Spatial: &experience.SpatialContent{Objects: []experience.SpatialObject{{ID: 7, Node: scene.Node{Mesh: mesh, Transform: scene.Translate(0, 0, .2)}}}}}, {ID: 2, Key: "native:terminal", Texture: texture}}}
	w.SetApplications(apps)
	w.Draw(1440, 900)
	_ = w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: "native:model"})
	_ = w.Dispatch(Action{Kind: ToggleApplicationReading})
	w.Draw(1440, 900)
	return w, apps, mesh
}

func TestNativeSpatialMeshSharesPassAndReceivesObjectPicking(t *testing.T) {
	w, apps, mesh := nativeMeshFixture(t)
	frame := w.Draw(1440, 900)
	found := false
	for _, cmd := range frame.Commands {
		if cmd.Kind != render.SceneCommand {
			continue
		}
		hasMesh, hasPlane := false, false
		for _, d := range cmd.Draws {
			hasMesh = hasMesh || d.Geometry == mesh.Geometry()
			hasPlane = hasPlane || d.Texture == apps.surfaces[0].Texture
		}
		found = found || hasMesh && hasPlane
	}
	if !found {
		t.Fatal("native geometry is not in the content plane's depth pass")
	}
	point := w.spatialApplicationTransform(apps.surfaces[0]).TransformPoint(scene.Vec3{Z: .2})
	x, y, _, visible := w.camera.Project(point, w.viewport)
	if !visible {
		t.Fatal("fixture not visible")
	}
	apps.events = nil
	w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y})
	if !w.OwnsKeyboard() || apps.focus[len(apps.focus)-1] != 1 {
		t.Fatal("mesh pick did not route keyboard ownership to its application")
	}
	var down experience.Event
	for _, event := range apps.events {
		if event.event.Kind == experience.PointerDown {
			if event.id != 1 {
				t.Fatal("mesh event routed to sibling")
			}
			down = event.event
		}
	}
	if down.SpatialObject != 7 || down.SpatialTriangle != 0 || math.Abs(float64(down.SpatialPoint[2]-.2)) > .0001 {
		t.Fatalf("missing native picking coordinates: %+v", down)
	}
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 20, Y: y + 15})
	if got := apps.events[len(apps.events)-1]; got.id != 1 || got.event.SpatialObject != 7 {
		t.Fatal("mesh pointer capture lost provider/object identity", got)
	}
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x + 20, Y: y + 15})
	if w.applicationCaptureObject != 0 {
		t.Fatal("object capture stuck after release")
	}
}

func TestNativeSpatialReconciliationRetainsResourcesAndRemovesClosedGeometry(t *testing.T) {
	w, apps, mesh := nativeMeshFixture(t)
	old := w.applicationSpatial[1].nodes[7]
	apps.surfaces[0].Spatial.Objects[0].Node.Transform = scene.Translate(.1, 0, .3)
	w.Draw(1440, 900)
	if w.applicationSpatial[1].nodes[7] != old || w.scene.Node(old).Mesh != mesh {
		t.Fatal("transform change recreated retained geometry")
	}
	apps.surfaces = apps.surfaces[1:]
	frame := w.Draw(1440, 900)
	if w.scene.Node(old) != nil || w.applicationSpatial[1] != nil {
		t.Fatal("closed native app retained scene nodes")
	}
	for _, cmd := range frame.Commands {
		for _, d := range cmd.Draws {
			if d.Geometry == mesh.Geometry() {
				t.Fatal("closed app geometry remained in submitted frame")
			}
		}
	}
	if len(w.applicationSurfaces) != 1 || w.applicationSurfaces[0].ID != 2 {
		t.Fatal("closing native app removed sibling")
	}
}

func TestNativeSpatialHierarchyReplacementAndInvalidContent(t *testing.T) {
	w, apps, mesh := nativeMeshFixture(t)
	s := apps.surfaces[0].Spatial
	s.Objects = []experience.SpatialObject{{ID: 1}, {ID: 2, Parent: 1, Node: scene.Node{Mesh: mesh}}}
	w.Draw(1440, 900)
	s.Objects = []experience.SpatialObject{{ID: 2}, {ID: 1, Parent: 2, Node: scene.Node{Mesh: mesh}}}
	w.Draw(1440, 900)
	m := w.applicationSpatial[1]
	children := w.scene.Children(m.nodes[2])
	if len(children) != 1 || children[0] != m.nodes[1] {
		t.Fatal("reversed hierarchy did not install atomically")
	}
	s.Objects[1].Parent = 9
	w.Draw(1440, 900)
	if w.applicationSpatial[1] != nil || w.scene.Node(w.applicationNodes[1]) == nil {
		t.Fatal("invalid scene must withdraw meshes while retaining controls plane")
	}
}

func TestNativeSpatialLabelsFollowTheirOwningApplicationAcrossViews(t *testing.T) {
	w, apps, _ := nativeMeshFixture(t)
	const annotation = "OWNER ANNOTATION"
	apps.surfaces[0].Spatial.Labels = []experience.SpatialLabel{{
		Text: annotation, Position: scene.Vec3{Z: .3}, Color: scene.ColorHex(0xc4efff, 1),
	}}

	visibleThisFrame := func() bool {
		w.Draw(1440, 900)
		if w.labels == nil {
			return false
		}
		for key, label := range w.labels.cache {
			if key.text == annotation && label.frame == w.labels.frame {
				return true
			}
		}
		return false
	}

	if !visibleThisFrame() {
		t.Fatal("Reading hid the active application's spatial annotation")
	}
	command(t, w, Action{Kind: ToggleApplicationReading})
	if !visibleThisFrame() {
		t.Fatal("Space hid a visible application's authored spatial annotation")
	}
	command(t, w, Action{Kind: ToggleApplicationOverview})
	if !visibleThisFrame() {
		t.Fatal("Overview hid a visible application's authored spatial annotation")
	}
	command(t, w, Action{Kind: ToggleApplicationOverview})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key})
	command(t, w, Action{Kind: ToggleApplicationReading})
	if visibleThisFrame() {
		t.Fatal("Reading another application painted a hidden owner's annotation")
	}
}
