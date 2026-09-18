package scene

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func testTexture(t testing.TB) *render.Texture {
	t.Helper()
	texture, err := render.NewTexture(320, 180, make([]byte, 320*180*4))
	if err != nil {
		t.Fatal(err)
	}
	return texture
}

func TestSurfacePickMapsTexturePixelsThroughTransformedParents(t *testing.T) {
	s := NewScene()
	parentTransform := Translate(0.25, -0.1, 0.2).Mul(RotateZ(0.35)).Mul(Scale(1.5, 0.8, 1))
	childTransform := RotateY(-0.4).Mul(Scale(3, 2, 0.1))
	parent := s.Add(0, Node{Transform: parentTransform})
	child := s.Add(parent, Node{Transform: childTransform, Surface: testTexture(t)})
	world := parentTransform.Mul(childTransform)
	camera, vp := testCamera(), Viewport{X: 45, Y: 75, Width: 640, Height: 480}
	for _, point := range []Vec3{{-0.4, 0.4, 0}, {0.4, 0.4, 0}, {-0.4, -0.4, 0}, {0.4, -0.4, 0}, {0, 0, 0}} {
		wantWorld := world.TransformPoint(point)
		x, y, wantDepth, visible := camera.Project(wantWorld, vp)
		if !visible {
			t.Fatalf("test point %+v is clipped", point)
		}
		hit, ok := s.Pick(camera, vp, x, y)
		if !ok || hit.Node != child || !hit.Surface || hit.Triangle != -1 {
			t.Fatalf("surface pick for %+v = %+v, %v", point, hit, ok)
		}
		if hit.Point.Sub(wantWorld).Length() > 0.0001 || !closeTo(hit.Depth, wantDepth) {
			t.Fatalf("surface world point/depth = %+v, want %+v at %v", hit, wantWorld, wantDepth)
		}
		wantX, wantY := (point.X+0.5)*320, (0.5-point.Y)*180
		if abs(hit.PixelX-wantX) > 0.001 || abs(hit.PixelY-wantY) > 0.001 {
			t.Fatalf("surface pixel = (%v,%v), want (%v,%v)", hit.PixelX, hit.PixelY, wantX, wantY)
		}
	}
}

func TestSurfaceAndMeshOcclusionUsesSharedWorldDistance(t *testing.T) {
	for _, surfaceFirst := range []bool{false, true} {
		for _, surfaceInFront := range []bool{false, true} {
			s := NewScene()
			var surface, mesh NodeID
			surfaceZ := float32(-1)
			if surfaceInFront {
				surfaceZ = 1
			}
			addSurface := func() {
				surface = s.Add(0, Node{Surface: testTexture(t), Transform: Translate(0, 0, surfaceZ).Mul(Scale(2, 2, 100))})
			}
			addMesh := func() { mesh = s.Add(0, Node{Mesh: testTriangle(t), Transform: Scale(2, 2, 0.01)}) }
			if surfaceFirst {
				addSurface()
				addMesh()
			} else {
				addMesh()
				addSurface()
			}
			want := mesh
			if surfaceInFront {
				want = surface
			}
			hit, ok := s.Pick(testCamera(), Viewport{Width: 200, Height: 200}, 100, 100)
			if !ok || hit.Node != want || hit.Surface != surfaceInFront {
				t.Fatalf("surfaceFirst=%v, surfaceInFront=%v: pick=%+v, %v; want %v", surfaceFirst, surfaceInFront, hit, ok, want)
			}
		}
	}
}

func TestSurfaceMappingForCaptureIgnoresOcclusionAndReportsOutside(t *testing.T) {
	s := NewScene()
	surface := s.Add(0, Node{Surface: testTexture(t), Transform: Scale(2, 2, 1)})
	s.Add(0, Node{Mesh: testTriangle(t), Transform: Translate(0, 0, 1).Mul(Scale(4, 4, 1))})
	camera, vp := testCamera(), Viewport{Width: 400, Height: 400}
	hit, inside, ok := s.MapSurface(camera, vp, 200, 200, surface)
	if !ok || !inside || hit.Node != surface || !closeTo(hit.PixelX, 160) || !closeTo(hit.PixelY, 90) {
		t.Fatalf("captured surface was blocked by a mesh: %+v, inside=%v, ok=%v", hit, inside, ok)
	}
	x, y, _, _ := camera.Project(Vec3{1.5, 0, 0}, vp)
	hit, inside, ok = s.MapSurface(camera, vp, x, y, surface)
	if !ok || inside || abs(hit.PixelX-400) > 0.001 || abs(hit.PixelY-90) > 0.001 {
		t.Fatalf("outside captured surface mapping: %+v, inside=%v, ok=%v", hit, inside, ok)
	}
	if _, ok := s.Pick(camera, vp, x, y); ok {
		t.Fatal("uncaptured pick included an outside plane point")
	}
	if _, _, ok := s.MapSurface(camera, vp, -1, 200, surface); ok {
		t.Fatal("mapped pointer outside viewport")
	}
}

func TestCapturedSurfaceMappingContinuesOutsideViewport(t *testing.T) {
	s := NewScene()
	id := s.Add(0, Node{Surface: testTexture(t), Transform: Scale(2, 2, 1)})
	camera, vp := testCamera(), Viewport{X: 40, Y: 60, Width: 400, Height: 300}
	for _, x := range []float32{vp.X - 60, vp.X + vp.Width + 60} {
		y := vp.Y + vp.Height/2
		if _, _, ok := s.MapSurface(camera, vp, x, y, id); ok {
			t.Fatal("ordinary mapping accepted an outside viewport point")
		}
		hit, inside, ok := s.MapCapturedSurface(camera, vp, x, y, id)
		if !ok || inside || hit.Node != id || abs(hit.PixelY-90) > .002 {
			t.Fatalf("captured mapping lost outside point: %+v %v %v", hit, inside, ok)
		}
		if (x < vp.X && hit.PixelX >= 0) || (x > vp.X+vp.Width && hit.PixelX <= 320) {
			t.Fatal("captured texture coordinates did not extend beyond content")
		}
	}
	s.Node(id).Transform = Translate(0, 0, 4.95)
	if _, _, ok := s.MapCapturedSurface(camera, vp, vp.X-60, vp.Y+vp.Height/2, id); ok {
		t.Fatal("captured mapping crossed the camera near plane")
	}
	if _, _, ok := s.MapCapturedSurface(camera, vp, float32(math.NaN()), vp.Y, id); ok {
		t.Fatal("captured mapping accepted non-finite coordinates")
	}
}

func TestSurfaceMappingTracksContentResizeAndEqualDepthOrder(t *testing.T) {
	s := NewScene()
	texture := testTexture(t)
	s.Add(0, Node{Mesh: testTriangle(t)})
	surface := s.Add(0, Node{Surface: texture})
	camera, vp := testCamera(), Viewport{Width: 200, Height: 200}
	hit, ok := s.Pick(camera, vp, 100, 100)
	if !ok || hit.Node != surface || !closeTo(hit.PixelX, 160) || !closeTo(hit.PixelY, 90) {
		t.Fatalf("later equal-depth surface must match the GPU's less-or-equal test: %+v, %v", hit, ok)
	}
	if err := texture.Replace(640, 360, make([]byte, 640*360*4)); err != nil {
		t.Fatal(err)
	}
	hit, ok = s.Pick(camera, vp, 100, 100)
	if !ok || !closeTo(hit.PixelX, 320) || !closeTo(hit.PixelY, 180) {
		t.Fatalf("surface mapping kept stale texture dimensions: %+v, %v", hit, ok)
	}
	mesh := s.Add(0, Node{Mesh: testTriangle(t)})
	hit, ok = s.Pick(camera, vp, 100, 100)
	if !ok || hit.Node != mesh || hit.Surface {
		t.Fatalf("later equal-depth mesh must cover the surface: %+v, %v", hit, ok)
	}
}

func TestSurfacePickRespectsClippingHierarchyAndOpaquePixels(t *testing.T) {
	s := NewScene()
	// Fully transparent source pixels are still an opaque surface under the
	// initial content contract; CPU picking must not pass through them.
	texture := testTexture(t)
	s.Add(0, Node{Surface: texture, Transform: Translate(0, 0, 4.95)})
	s.Add(0, Node{Surface: texture, Transform: Translate(0, 0, -200)})
	parent := s.Add(0, Node{})
	visible := s.Add(parent, Node{Surface: texture})
	camera, vp := testCamera(), Viewport{Width: 200, Height: 200}
	if hit, ok := s.Pick(camera, vp, 100, 100); !ok || hit.Node != visible {
		t.Fatalf("camera clipping or opaque surface pick failed: %+v, %v", hit, ok)
	}
	for _, change := range []func(*Node){
		func(n *Node) { n.Hidden = true },
		func(n *Node) { n.Unpickable = true },
		func(n *Node) { n.Color.A = 0 },
		func(n *Node) { n.Transform = Scale(1, 1, 0) },
	} {
		n := s.Node(parent)
		saved := *n
		change(n)
		if hit, ok := s.Pick(camera, vp, 100, 100); ok {
			t.Fatalf("picked inaccessible surface: %+v", hit)
		}
		if hit, _, ok := s.MapSurface(camera, vp, 100, 100, visible); ok {
			t.Fatalf("captured inaccessible surface: %+v", hit)
		}
		*n = saved
	}
}

func TestSurfacePickIsTwoSidedAndRejectsParallelPlane(t *testing.T) {
	s := NewScene()
	id := s.Add(0, Node{Surface: testTexture(t), Transform: RotateY(math.Pi)})
	camera, vp := testCamera(), Viewport{Width: 200, Height: 200}
	x, y, _, _ := camera.Project(Vec3{0.25, 0.25, 0}, vp)
	hit, ok := s.Pick(camera, vp, x, y)
	if !ok || hit.Node != id || abs(hit.PixelX-80) > 0.001 || abs(hit.PixelY-45) > 0.001 {
		t.Fatalf("back-facing content plane mapping=%+v, %v", hit, ok)
	}
	// Use exact coefficients to make the plane edge-on to the center ray.
	s.Node(id).Transform = Mat4{0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1}
	if _, ok := s.Pick(camera, vp, 100, 100); ok {
		t.Fatal("picked a plane parallel to the ray")
	}
}

func TestSurfaceAndMeshDrawUseOneCameraPass(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Reset(200, 200)
	s := NewScene()
	mesh := testTriangle(t)
	texture := testTexture(t)
	parent := s.Add(0, Node{Transform: Translate(1, 2, 3), Color: Color{0.5, 0.5, 1, 1}})
	s.Add(parent, Node{Mesh: mesh})
	transform := RotateY(0.3).Mul(Scale(3, 2, 1))
	s.Add(parent, Node{Surface: texture, Transform: transform})
	s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
	frame := c.Frame()
	if len(frame.Commands) != 1 || frame.Commands[0].Kind != render.SceneCommand || len(frame.Commands[0].Draws) != 2 {
		t.Fatalf("mesh and content surface were separated into different depth passes: %+v", frame.Commands)
	}
	draws := frame.Commands[0].Draws
	if draws[0].Geometry != mesh.Geometry() || draws[0].Texture != nil || draws[1].Geometry != nil || draws[1].Texture != texture {
		t.Fatal("draw instances lost their distinct mesh/surface resource")
	}
	if draws[1].Model != [16]float32(Translate(1, 2, 3).Mul(transform)) || draws[1].Color != [4]float32{0.5, 0.5, 1, 1} {
		t.Fatal("content plane lost parent transform or tint")
	}
}

func TestNodeRejectsAmbiguousMeshAndSurface(t *testing.T) {
	s := NewScene()
	mesh, texture := testTriangle(t), testTexture(t)
	defer func() {
		if recover() == nil {
			t.Fatal("accepted both content resources on one node")
		}
	}()
	s.Add(0, Node{Mesh: mesh, Surface: texture})
}
