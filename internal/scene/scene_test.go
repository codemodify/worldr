package scene

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func testCamera() Camera {
	return Camera{Eye: Vec3{0, 0, 5}, Target: Vec3{}, FOV: math.Pi / 2, Near: 0.1, Far: 100}
}
func makeTestMesh(t testing.TB, vertices []Vec3, indices []uint32) *Mesh {
	t.Helper()
	mesh, err := NewMesh(vertices, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	return mesh
}
func testTriangle(t testing.TB) *Mesh {
	return makeTestMesh(t, []Vec3{{-0.4, -0.4, 0}, {0.4, -0.4, 0}, {0, 0.4, 0}}, []uint32{0, 1, 2})
}
func closeTo(a, b float32) bool { return abs(a-b) < 0.0001 }

func TestPickFollowsNestedRotationAndTranslation(t *testing.T) {
	s := NewScene()
	parent := s.Add(0, Node{Transform: RotateZ(math.Pi / 2)})
	child := s.Add(parent, Node{Transform: Translate(1, 0, 0), Mesh: testTriangle(t)})
	camera, viewport := testCamera(), Viewport{Width: 200, Height: 200}
	// The child's local x translation becomes world y after the parent turns.
	x, y, _, visible := camera.Project(Vec3{0, 1, 0}, viewport)
	if !visible || !closeTo(x, 100) || !closeTo(y, 80) {
		t.Fatalf("projected translated child = (%v,%v), visible %v", x, y, visible)
	}
	hit, ok := s.Pick(camera, viewport, x, y)
	if !ok || hit.Node != child || !closeTo(hit.Point.X, 0) || !closeTo(hit.Point.Y, 1) {
		t.Fatalf("pick of transformed geometry = %+v, %v", hit, ok)
	}
	if _, ok := s.Pick(camera, viewport, 120, 100); ok {
		t.Fatal("picked the child's untransformed position")
	}
}

func TestPickChoosesVisibleDepthIndependentOfInsertionOrder(t *testing.T) {
	s := NewScene()
	front := s.Add(0, Node{Transform: Translate(0, 0, 1), Mesh: testTriangle(t)})
	back := s.Add(0, Node{Transform: Translate(0, 0, -1), Mesh: testTriangle(t)})
	camera, viewport := testCamera(), Viewport{Width: 200, Height: 200}
	hit, ok := s.Pick(camera, viewport, 100, 100)
	if !ok || hit.Node != front || !closeTo(hit.Point.Z, 1) {
		t.Fatalf("expected front surface, got %+v, %v", hit, ok)
	}
	s.Node(front).Hidden = true
	hit, ok = s.Pick(camera, viewport, 100, 100)
	if !ok || hit.Node != back || !closeTo(hit.Point.Z, -1) {
		t.Fatalf("expected revealed back surface, got %+v, %v", hit, ok)
	}
}

func TestPerspectivePickReturnsWorldPointOnSlopedPlane(t *testing.T) {
	s := NewScene()
	// These vertices lie on z=x. A perspective-correct interpolation of the
	// screen-space barycentrics must reconstruct that plane after rotation.
	node := s.Add(0, Node{Mesh: makeTestMesh(t, []Vec3{{-2, -2, -2}, {2, -2, 2}, {0, 2, 0}}, []uint32{0, 1, 2})})
	camera, viewport := testCamera(), Viewport{Width: 640, Height: 480}
	want := Vec3{0.35, -0.4, 0.35}
	x, y, _, visible := camera.Project(want, viewport)
	if !visible {
		t.Fatal("test point should be visible")
	}
	hit, ok := s.Pick(camera, viewport, x, y)
	if !ok || hit.Node != node || hit.Point.Sub(want).Length() > 0.0001 {
		t.Fatalf("perspective pick = %+v, want %+v", hit, want)
	}
}

func TestNearPlaneClippingKeepsVisiblePortionAndRejectsBehindCamera(t *testing.T) {
	s := NewScene()
	visible := s.Add(0, Node{Mesh: makeTestMesh(t, []Vec3{{-0.7, -0.5, 0}, {0.7, -0.5, 0}, {0, 0.7, 1.5}}, []uint32{0, 1, 2})})
	s.Add(0, Node{Transform: Translate(0, 0, 3), Mesh: testTriangle(t)})
	camera := Camera{Eye: Vec3{0, 0, 2}, FOV: math.Pi / 2, Near: 1, Far: 10}
	vp := Viewport{X: 30, Y: 40, Width: 200, Height: 150}
	x, y, _, ok := camera.Project(Vec3{0, -0.2, 0.375}, vp)
	if !ok {
		t.Fatal("test point outside clipped triangle")
	}
	hit, ok := s.Pick(camera, vp, x, y)
	if !ok || hit.Node != visible {
		t.Fatalf("visible clipped portion cannot be picked: %+v, %v", hit, ok)
	}
}

func TestReparentRejectsCyclesAndRemoveDeletesSubtree(t *testing.T) {
	s := NewScene()
	parent := s.Add(0, Node{})
	child := s.Add(parent, Node{})
	leaf := s.Add(child, Node{Mesh: testTriangle(t)})
	if err := s.Reparent(parent, leaf); err == nil {
		t.Fatal("accepted hierarchy cycle")
	}
	if len(s.Children(0)) != 1 || len(s.Children(parent)) != 1 {
		t.Fatal("cycle rejection changed hierarchy")
	}
	s.Remove(child)
	if s.Node(child) != nil || s.Node(leaf) != nil || s.Node(parent) == nil || len(s.Children(parent)) != 0 {
		t.Fatal("subtree removal left dangling nodes")
	}
}

func TestPickNonuniformInstancesPreservesWorldDistance(t *testing.T) {
	s := NewScene()
	mesh := testTriangle(t)
	front := s.Add(0, Node{Mesh: mesh, Transform: Translate(0, 0, 1).Mul(Scale(2, 0.5, 0.01))})
	s.Add(0, Node{Mesh: mesh, Transform: Translate(0, 0, -1).Mul(Scale(1, 1, 100))})
	camera, vp := testCamera(), Viewport{X: 80, Y: 120, Width: 640, Height: 480}
	want := Vec3{0.2, 0.03, 1}
	x, y, _, visible := camera.Project(want, vp)
	if !visible {
		t.Fatal("test point not visible")
	}
	hit, ok := s.Pick(camera, vp, x, y)
	if !ok || hit.Node != front || hit.Point.Sub(want).Length() > 0.0001 {
		t.Fatalf("scaled-instance pick=%+v, want front at %+v", hit, want)
	}
	for _, p := range [][2]float32{{0, 0}, {79, 240}, {720, 240}, {200, 600}} {
		if _, ok := s.Pick(camera, vp, p[0], p[1]); ok {
			t.Fatalf("pick escaped viewport at %v", p)
		}
	}
}

func TestPickingRespectsNearAndFarPlanes(t *testing.T) {
	s := NewScene()
	mesh := testTriangle(t)
	s.Add(0, Node{Mesh: mesh, Transform: Translate(0, 0, 4.95)})
	visible := s.Add(0, Node{Mesh: mesh})
	s.Add(0, Node{Mesh: mesh, Transform: Translate(0, 0, -200)})
	camera, vp := testCamera(), Viewport{Width: 200, Height: 200}
	hit, ok := s.Pick(camera, vp, 100, 100)
	if !ok || hit.Node != visible {
		t.Fatalf("near-clipped surface blocked valid pick: %+v", hit)
	}
	s.Node(visible).Hidden = true
	if _, ok := s.Pick(camera, vp, 100, 100); ok {
		t.Fatal("picked geometry outside camera depth range")
	}
}

func TestMeshCopiesInputsAndExposesOnlyImmutableGeometry(t *testing.T) {
	vertices := []Vec3{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}
	indices := []uint32{0, 1, 2}
	colors := []Color{ColorHex(0xaabbcc, 1)}
	mesh, err := NewMesh(vertices, indices, colors)
	if err != nil {
		t.Fatal(err)
	}
	id := mesh.Geometry().ID()
	vertices[0] = Vec3{100, 100, 100}
	indices[0] = 2
	colors[0].A = 0
	exportedVertices, exportedIndices := mesh.Geometry().Vertices(), mesh.Geometry().Indices()
	exportedVertices[0].X = 1000
	exportedIndices[0] = 2
	if mesh.Geometry().ID() != id || mesh.Geometry().Vertices()[0].X != -1 || mesh.Geometry().Indices()[0] != 0 {
		t.Fatal("external mutation changed retained GPU geometry")
	}
	s := NewScene()
	node := s.Add(0, Node{Mesh: mesh})
	if hit, ok := s.Pick(testCamera(), Viewport{Width: 200, Height: 200}, 100, 100); !ok || hit.Node != node {
		t.Fatalf("external mutation changed picking geometry: %+v", hit)
	}
}

func TestMeshRejectsMalformedGeometry(t *testing.T) {
	for name, indices := range map[string][]uint32{"partial": {0, 1}, "out of range": {0, 1, 4}, "degenerate": {0, 0, 0}} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewMesh([]Vec3{{-1, -1, 0}, {1, -1, 0}, {0, 1, 0}}, indices, nil); err == nil {
				t.Fatal("accepted invalid geometry")
			}
		})
	}
}

func TestMultipleScenePassesKeepIndependentViewportAndCamera(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := NewScene()
	mesh := testTriangle(t)
	s.Add(0, Node{Mesh: mesh})
	c.Rect(0, 0, 640, 480, ColorHex(0, 1))
	s.Draw(c, testCamera(), Viewport{Width: 320, Height: 480})
	c.Text(10, 10, 16, "Between views", ColorHex(0xffffff, 1))
	otherCamera := testCamera()
	otherCamera.Eye = Vec3{2, 1, 5}
	s.Draw(c, otherCamera, Viewport{X: 320, Width: 320, Height: 480})
	frame := c.Frame()
	if len(frame.Commands) != 4 || frame.Commands[1].View.Viewport[0] != 0 || frame.Commands[3].View.Viewport[0] != 320 {
		t.Fatal("camera passes lost draw order or viewport")
	}
	if frame.Commands[1].Draws[0].Geometry != frame.Commands[3].Draws[0].Geometry {
		t.Fatal("shared mesh duplicated per view")
	}
	if frame.Commands[1].View.Projection == frame.Commands[3].View.Projection {
		t.Fatal("independent cameras collapsed into one view")
	}
}

func TestCanvasLayersPermitBackgroundSceneAndForeground(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Reset(200, 200)
	c.Rect(0, 0, 200, 200, ColorHex(0x102030, 1))
	s := NewScene()
	s.Add(0, Node{Mesh: testTriangle(t)})
	s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
	c.Text(20, 20, 16, "AV Ω → ± 1–3", ColorHex(0xffffff, 1))
	frame := c.Frame()
	if len(frame.Commands) != 3 || frame.Commands[0].Kind != render.OverlayCommand || frame.Commands[1].Kind != render.SceneCommand || frame.Commands[2].Kind != render.OverlayCommand {
		t.Fatalf("incorrect mixed scene ordering: %+v", frame.Commands)
	}
	if frame.Commands[0].First != 0 || frame.Commands[0].Count != 6 || frame.Commands[2].First != 6 {
		t.Fatal("3D mesh was flattened into the UI stream")
	}
	if len(frame.Commands[1].Draws) != 1 || frame.Commands[1].Draws[0].Geometry.IndexCount() != 3 {
		t.Fatal("scene lost its resident mesh reference")
	}
	if len(c.Frame().Commands) != 3 {
		t.Fatal("Frame duplicated pending commands")
	}
	// Raster coverage must contain intermediate values for actual antialiasing,
	// and typography must contain supported punctuation rather than placeholders.
	antialiased := false
	for _, coverage := range c.Atlas().Pixels {
		if coverage > 0 && coverage < 255 {
			antialiased = true
			break
		}
	}
	if !antialiased {
		t.Fatal("font atlas has no antialiased coverage")
	}
	for _, r := range []rune{'Ω', '→', '±', '–'} {
		if _, ok := c.glyphs[r]; !ok {
			t.Errorf("missing font glyph %q", r)
		}
	}
}

func TestMaterialRemainsPerInstanceAcrossHierarchyAndEdits(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := NewScene()
	parent := s.Add(0, Node{Material: render.Material{Specular: 1}})
	mesh := testTriangle(t)
	first := s.Add(parent, Node{Mesh: mesh})
	material := render.Material{Specular: .6, Roughness: .3, Metallic: .8, RimStrength: .1, RimColor: [3]float32{.2, .7, 1}}
	second := s.Add(parent, Node{Mesh: mesh, Material: material})
	s.Draw(c, testCamera(), Viewport{Width: 64, Height: 64})
	draws := c.Frame().Commands[0].Draws
	if draws[0].Material != (render.Material{}) || draws[1].Material != material || draws[0].Geometry != draws[1].Geometry {
		t.Fatal("materials were inherited, lost, or duplicated retained geometry")
	}
	s.Node(first).Material = material
	s.Node(second).Material = render.Material{}
	c.Reset(64, 64)
	s.Draw(c, testCamera(), Viewport{Width: 64, Height: 64})
	draws = c.Frame().Commands[0].Draws
	if draws[0].Material != material || draws[1].Material != (render.Material{}) {
		t.Fatal("node material edits did not update instance constants")
	}
}

func BenchmarkSceneEightThousandTriangles(b *testing.B) {
	c, err := NewCanvas()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	var vertices []Vec3
	var indices []uint32
	const cols, rows = 80, 50
	for y := 0; y <= rows; y++ {
		for x := 0; x <= cols; x++ {
			vertices = append(vertices, Vec3{float32(x-cols/2) * 0.05, float32(y-rows/2) * 0.05, 0})
		}
	}
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			a := uint32(y*(cols+1) + x)
			indices = append(indices, a, a+1, a+cols+1, a+1, a+cols+2, a+cols+1)
		}
	}
	s := NewScene()
	mesh := makeTestMesh(b, vertices, indices)
	id := s.Add(0, Node{Mesh: mesh, WireColor: ColorHex(0xffffff, 0.1), WireWidth: 0.6})
	camera, vp := testCamera(), Viewport{Width: 1440, Height: 900}
	s.Draw(c, camera, vp)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Reset(1440, 900)
		s.Node(id).Transform = RotateY(float32(i) * 0.01)
		s.Draw(c, camera, vp)
	}
}
