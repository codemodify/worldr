package workspace

import (
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

func sameWindowChromeShape(a, b *scene.Mesh) bool {
	if a == nil || b == nil {
		return a == b
	}
	av, bv := a.Geometry().Vertices(), b.Geometry().Vertices()
	if len(av) != len(bv) || !reflect.DeepEqual(a.Geometry().Indices(), b.Geometry().Indices()) {
		return false
	}
	for i := range av {
		if av[i].X != bv[i].X || av[i].Y != bv[i].Y || av[i].Z != bv[i].Z {
			return false
		}
	}
	return true
}

func assertSafeWindowBorderMesh(t *testing.T, mesh *scene.Mesh) {
	t.Helper()
	geometry := mesh.Geometry()
	vertices, indices := geometry.Vertices(), geometry.Indices()
	if len(indices) == 0 || len(indices)%3 != 0 {
		t.Fatal("window border has invalid triangle geometry")
	}
	minZ, maxZ, walls := float32(1), float32(-1), 0
	for i := 0; i < len(indices); i += 3 {
		triangle := make([]scene.Vec3, 3)
		triangleMinZ, triangleMaxZ := float32(1), float32(-1)
		for j, index := range indices[i : i+3] {
			if int(index) >= len(vertices) {
				t.Fatalf("triangle %d references missing vertex %d", i/3, index)
			}
			vertex := vertices[index]
			if !finite(float64(vertex.X)) || !finite(float64(vertex.Y)) || !finite(float64(vertex.Z)) {
				t.Fatalf("triangle %d contains nonfinite geometry", i/3)
			}
			if vertex.Z > 0 {
				t.Fatalf("triangle %d extends in front of client pixels at z=%g", i/3, vertex.Z)
			}
			triangle[j] = scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z}
			minZ, maxZ = min(minZ, vertex.Z), max(maxZ, vertex.Z)
			triangleMinZ, triangleMaxZ = min(triangleMinZ, vertex.Z), max(triangleMaxZ, vertex.Z)
		}
		if area := terminalTriangleContentArea(triangle); area > 1e-9 {
			t.Fatalf("triangle %d covers client pixels: area %g vertices=%v", i/3, area, triangle)
		}
		if triangleMinZ < triangleMaxZ {
			walls++
		}
	}
	if minZ >= -.09 || maxZ != 0 || walls == 0 {
		t.Fatalf("window border lacks a front plane with real rearward depth: z=%g..%g walls=%d", minZ, maxZ, walls)
	}
}

func TestWindowBorderStylesUseDistinctCachedSafeGeometry(t *testing.T) {
	w := desktop(t)
	seen := make(map[*scene.Mesh]windowBorderStyle, len(windowBorderChoices))
	for _, choice := range windowBorderChoices {
		choice := choice
		t.Run(string(choice.style), func(t *testing.T) {
			mesh := w.windowBorderFrameMesh(choice.style)
			if mesh == nil || mesh != w.windowBorderFrameMesh(choice.style) {
				t.Fatal("window border did not retain and reuse its mesh")
			}
			if previous, duplicate := seen[mesh]; duplicate {
				t.Fatalf("window border shares its mesh with %q", previous)
			}
			seen[mesh] = choice.style

			assertSafeWindowBorderMesh(t, mesh)
		})
	}
	if len(seen) != len(windowBorderChoices) {
		t.Fatalf("got %d distinct meshes for %d styles", len(seen), len(windowBorderChoices))
	}
}

func TestWindowBorderStylesRetainRatioCorrectGeometryAcrossResizeRange(t *testing.T) {
	w := desktop(t)
	for _, choice := range windowBorderChoices {
		choice := choice
		t.Run(string(choice.style), func(t *testing.T) {
			seen := make(map[*scene.Mesh]bool)
			for _, aspect := range []float32{.67, 1, 1.6, 2.4, 4.35} {
				mesh := w.windowBorderFrameMeshForAspect(choice.style, aspect)
				if mesh != w.windowBorderFrameMeshForAspect(choice.style, aspect) {
					t.Fatalf("aspect %.2f did not reuse its retained border mesh", aspect)
				}
				if seen[mesh] {
					t.Fatalf("aspect %.2f unexpectedly reused geometry from a different ratio", aspect)
				}
				seen[mesh] = true
				assertSafeWindowBorderMesh(t, mesh)
			}
		})
	}
}

func TestWindowBorderStyleSwitchIsGlobalAndPreservesRuntimeNodes(t *testing.T) {
	w := desktop(t)
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{surfaces: []experience.ApplicationSurface{
		{ID: 101, Key: "frame:default", AppID: "worldr.generic", Title: "Default", Texture: texture},
		{ID: 102, Key: "frame:cinematic", AppID: "worldr.external", Title: "Cinematic", Texture: texture, FrameStyle: experience.FrameCinematic},
		{ID: 103, Key: "frame:photo", AppID: "worldr.external", Title: "Photo", Texture: texture, FrameStyle: experience.FramePhotoBracket, DragContent: true},
		{ID: 104, Key: "frame:frameless", AppID: "worldr.external", Title: "Frameless", Texture: texture, FrameStyle: experience.FrameCinematic, Frameless: true},
	}}
	w.SetApplications(apps)
	w.Draw(1440, 900)

	type nodeSet struct {
		root, frame, grip, controls, resize scene.NodeID
	}
	nodes := make(map[uint64]nodeSet, len(apps.surfaces))
	type chromeSet struct{ grip, controls, resize *scene.Mesh }
	chrome := make(map[uint64]chromeSet, len(apps.surfaces))
	for _, surface := range apps.surfaces {
		nodes[surface.ID] = nodeSet{
			root: w.applicationNodes[surface.ID], frame: w.applicationFrames[surface.ID],
			grip: w.applicationDragHandles[surface.ID], controls: w.applicationWindowControls[surface.ID],
			resize: w.applicationResizeHandles[surface.ID],
		}
		chrome[surface.ID] = chromeSet{
			grip: w.scene.Node(w.applicationDragHandles[surface.ID]).Mesh, controls: w.scene.Node(w.applicationWindowControls[surface.ID]).Mesh,
			resize: w.scene.Node(w.applicationResizeHandles[surface.ID]).Mesh,
		}
	}
	photoMesh := w.scene.Node(nodes[103].frame).Mesh
	if photoMesh == nil || photoMesh != w.photoFrameMesh {
		t.Fatal("photo fixture did not begin with its dedicated bracket")
	}
	beforeDocument, beforeHistory, beforeHistoryLength := w.Document(), w.historyPosition, len(w.history)
	beforeEvents, beforeFocus, beforeResizes := len(apps.events), len(apps.focus), len(apps.resizes)
	beforeRevision := texture.Revision()

	for _, choice := range windowBorderChoices {
		w.windows.Border = choice.style
		frame := w.Draw(1440, 900)
		selected := w.windowBorderFrameMesh(choice.style)
		for _, id := range []uint64{101, 102} {
			if border := w.scene.Node(nodes[id].frame); border == nil || border.Hidden || border.Mesh != selected {
				t.Fatalf("%q was not applied globally to managed window %d", choice.style, id)
			}
			current := chromeSet{
				grip: w.scene.Node(nodes[id].grip).Mesh, controls: w.scene.Node(nodes[id].controls).Mesh,
				resize: w.scene.Node(nodes[id].resize).Mesh,
			}
			baseline := chrome[id]
			if !sameWindowChromeShape(current.grip, baseline.grip) || !sameWindowChromeShape(current.controls, baseline.controls) || !sameWindowChromeShape(current.resize, baseline.resize) {
				t.Fatalf("%q changed a window chrome pick shape for window %d", choice.style, id)
			}
			if choice.style != windowBorderInstrument && (current.grip == baseline.grip || current.controls == baseline.controls || current.resize == baseline.resize) {
				t.Fatalf("%q left Instrument chrome attached to window %d", choice.style, id)
			}
		}
		if border := w.scene.Node(nodes[103].frame); border == nil || border.Hidden || border.Mesh != photoMesh {
			t.Fatalf("%q replaced or hid the photo bracket", choice.style)
		}
		if border := w.scene.Node(nodes[104].frame); border == nil || !border.Hidden || border.Glow != [3]float32{} {
			t.Fatalf("%q exposed frameless decoration", choice.style)
		}
		if draws := frameDraws(w, frame); len(draws) != 3 {
			t.Fatalf("%q submitted %d visible frames, want the two managed borders and photo bracket", choice.style, len(draws))
		}
		for _, surface := range apps.surfaces {
			want := nodes[surface.ID]
			got := nodeSet{
				root: w.applicationNodes[surface.ID], frame: w.applicationFrames[surface.ID],
				grip: w.applicationDragHandles[surface.ID], controls: w.applicationWindowControls[surface.ID],
				resize: w.applicationResizeHandles[surface.ID],
			}
			if got != want {
				t.Fatalf("%q recreated chrome nodes for surface %d: got %+v want %+v", choice.style, surface.ID, got, want)
			}
		}
	}

	if w.Document() != beforeDocument || w.historyPosition != beforeHistory || len(w.history) != beforeHistoryLength ||
		texture.Revision() != beforeRevision || len(apps.events) != beforeEvents || len(apps.focus) != beforeFocus || len(apps.resizes) != beforeResizes {
		t.Fatal("live border switching changed document/history, client pixels, input, focus, or provider size")
	}
}

func TestWindowBorderStylesFitInsideReadCamera(t *testing.T) {
	for _, choice := range windowBorderChoices {
		choice := choice
		t.Run(string(choice.style), func(t *testing.T) {
			w, apps := windowDragWorkspace(t, 1)
			apps.surfaces[0].AppID = "worldr.generic"
			apps.surfaces[0].FrameStyle = experience.FrameDefault
			w.windows.Border = choice.style
			command(t, w, Action{Kind: ToggleApplicationReading})
			w.Draw(1200, 800)

			surface := apps.surfaces[0]
			root := w.scene.Node(w.applicationNodes[surface.ID])
			border := w.scene.Node(w.applicationFrames[surface.ID])
			if root == nil || border == nil || border.Mesh != w.windowBorderFrameMesh(choice.style) {
				t.Fatal("Read did not install the selected border")
			}
			for _, vertex := range border.Mesh.Geometry().Vertices() {
				point := root.Transform.Mul(border.Transform).TransformPoint(scene.Vec3{X: vertex.X, Y: vertex.Y, Z: vertex.Z})
				x, y, _, visible := w.camera.Project(point, w.viewport)
				if !visible || x < w.viewport.X || x > w.viewport.X+w.viewport.Width || y < w.viewport.Y || y > w.viewport.Y+w.viewport.Height {
					t.Fatalf("selected border is clipped by Read at %.2f,%.2f", x, y)
				}
			}
			if !w.scene.Node(w.applicationDragHandles[surface.ID]).Hidden ||
				!w.scene.Node(w.applicationWindowControls[surface.ID]).Hidden ||
				!w.scene.Node(w.applicationResizeHandles[surface.ID]).Hidden {
				t.Fatal("Read exposed functional window chrome")
			}
		})
	}
}
