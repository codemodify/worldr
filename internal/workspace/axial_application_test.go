package workspace

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func TestHostedAxialIsAReusableSpatialApplication(t *testing.T) {
	manager, err := NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if launches := manager.ApplicationLaunches(); len(launches) != 1 || launches[0].Kind != "axial" {
		t.Fatalf("launch catalog: %+v", launches)
	}
	key, err := manager.LaunchApplication("axial")
	if err != nil || key != AxialApplicationKey {
		t.Fatalf("launch: %q %v", key, err)
	}
	surfaces := manager.Surfaces()
	if len(surfaces) != 1 || surfaces[0].Texture == nil || surfaces[0].Spatial == nil || surfaces[0].AppID != "worldr.axial" || surfaces[0].FrameStyle != experience.FrameCinematic {
		t.Fatalf("hosted surface: %+v", surfaces)
	}
	if got := len(surfaces[0].Spatial.Objects); got != 6 {
		t.Fatalf("spatial object count=%d", got)
	}
	firstID := surfaces[0].ID
	manager.Focus(firstID)
	manager.Send(firstID, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, SpatialObject: 4, X: 200, Y: 120})
	manager.Send(firstID, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, SpatialObject: 4, X: 200, Y: 120})
	manager.Send(firstID, experience.Event{Kind: experience.KeyInput, Key: experience.KeyE, Pressed: true})
	state := manager.SessionStates()
	if len(state) != 1 || state[0].Selected != 2 || !state[0].Exploded {
		t.Fatalf("interactive state: %+v", state)
	}

	texture := surfaces[0].Texture.ID()
	geometry := axialGeometryIDs(manager)
	manager.CloseApplication(firstID)
	if len(manager.Surfaces()) != 0 {
		t.Fatal("closed hosted app retained its surface")
	}
	if retired := manager.RetiredTextures(); len(retired) != 1 || retired[0] != texture {
		t.Fatalf("retired textures: %v", retired)
	}
	if retired := manager.RetiredGeometryIDs(); !sameAxialGeometry(retired, geometry) {
		t.Fatalf("retired geometry: got=%v want=%v", retired, geometry)
	}
	if _, err := manager.LaunchApplication("axial"); err != nil {
		t.Fatal(err)
	}
	if next := manager.Surfaces()[0].ID; next == firstID {
		t.Fatal("reopened hosted app reused its runtime identity")
	}
	if replacement := axialGeometryIDs(manager); sameAxialGeometry(replacement, geometry) {
		t.Fatalf("reopened hosted app reused retired geometry: %v", replacement)
	}
}

func TestHostedAxialSessionRoundTripAndRetirement(t *testing.T) {
	manager, err := NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	want := AxialApplicationState{Key: AxialApplicationKey, Time: 12.5, Yaw: -.4, Pitch: .2, Zoom: .2, Selected: 0, Exploded: true}
	if _, err := manager.Restore(want); err != nil {
		t.Fatal(err)
	}
	got := manager.SessionStates()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("restored state: %+v", got)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if retired := manager.RetiredGeometryIDs(); len(retired) != 4 {
		t.Fatalf("retired geometry: %v", retired)
	}
	if len(manager.RetiredTextures()) != 1 {
		t.Fatal("hosted surface texture was not retired")
	}
}

func TestHostedAxialSpatialPickRoutesThroughWorkspace(t *testing.T) {
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	manager, err := NewAxialApplicationManager()
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.LaunchApplication("axial"); err != nil {
		t.Fatal(err)
	}
	w.SetApplications(manager)
	defer w.SetApplications(nil)
	w.Draw(1440, 900)
	surface := manager.Surfaces()[0]

	// Project real retained triangle centers until the workspace's depth pick
	// identifies the shaft. This crosses the complete scene-to-provider route,
	// rather than injecting an app-local object ID into the provider directly.
	x, y, ok := visibleAxialObjectPoint(w, surface, 4)
	if !ok {
		t.Fatal("hosted AXIAL shaft has no visible pick point")
	}
	before := manager.SessionStates()[0]
	if !w.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y}) {
		t.Fatal("workspace did not consume AXIAL mesh press")
	}
	w.Handle(experience.Event{Kind: experience.PointerMove, X: x + 12, Y: y + 7})
	w.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x + 12, Y: y + 7})
	after := manager.SessionStates()[0]
	if after.Selected != 2 || after.Yaw == before.Yaw || after.Pitch == before.Pitch {
		t.Fatalf("workspace mesh route did not select and orbit AXIAL: before=%+v after=%+v", before, after)
	}
}

func visibleAxialObjectPoint(w *Workspace, surface experience.ApplicationSurface, object uint64) (float32, float32, bool) {
	mount := w.applicationSpatial[surface.ID]
	if mount == nil {
		return 0, 0, false
	}
	id := mount.nodes[object]
	node := w.scene.Node(id)
	if node == nil || node.Mesh == nil {
		return 0, 0, false
	}
	transform := w.spatialApplicationTransform(surface)
	var chain []scene.Mat4
	for current := object; current != 0; current = mount.parents[current] {
		chain = append(chain, w.scene.Node(mount.nodes[current]).Transform)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		transform = transform.Mul(chain[i])
	}
	vertices := node.Mesh.Geometry().Vertices()
	for i := 0; i+2 < len(vertices); i += 3 {
		point := scene.Vec3{
			X: (vertices[i].X + vertices[i+1].X + vertices[i+2].X) / 3,
			Y: (vertices[i].Y + vertices[i+1].Y + vertices[i+2].Y) / 3,
			Z: (vertices[i].Z + vertices[i+1].Z + vertices[i+2].Z) / 3,
		}
		x, y, _, visible := w.camera.Project(transform.TransformPoint(point), w.viewport)
		if !visible {
			continue
		}
		mapped, _, hit := w.mapApplication(experience.Event{X: x, Y: y}, false)
		if hit && mapped.SpatialObject == object {
			return x, y, true
		}
	}
	return 0, 0, false
}

func axialGeometryIDs(manager *AxialApplicationManager) []uint64 {
	result := make([]uint64, 0, 4)
	for _, mesh := range manager.meshes {
		if mesh != nil {
			result = append(result, mesh.Geometry().ID())
		}
	}
	if manager.accent != nil {
		result = append(result, manager.accent.Geometry().ID())
	}
	return result
}

func sameAxialGeometry(a, b []uint64) bool {
	if len(a) != 4 || len(b) != 4 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
