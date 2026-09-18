package scene

import (
	"math"
	"slices"
	"testing"
)

func foldedMeshData() ([]Vec3, []Vec3, []uint32) {
	return []Vec3{{-.9, -.9, .25}, {0, -.9, .5}, {0, .9, .5}, {.9, -.9, .25}},
		[]Vec3{{-6, 0, 8}, {0, 0, 10}, {0, 0, 10}, {6, 0, 8}},
		[]uint32{0, 1, 2, 1, 3, 2}
}

func TestExplicitNormalsPreserveGeometryAndHardFaceDefault(t *testing.T) {
	vertices, normals, indices := foldedMeshData()
	smooth, err := NewMeshWithNormals(vertices, normals, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	hard, err := NewMesh(vertices, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	smoothVertices, hardVertices := smooth.Geometry().Vertices(), hard.Geometry().Vertices()
	if smoothVertices[1].NX != 0 || smoothVertices[1].NZ != 1 || smoothVertices[3].NX != 0 || smoothVertices[3].NZ != 1 {
		t.Fatal("the shared crease vertex did not retain its common smooth normal")
	}
	if hardVertices[1].NX >= 0 || hardVertices[3].NX <= 0 {
		t.Fatal("NewMesh lost its independent hard face normals")
	}
	for i, got := range smoothVertices {
		if !closeTo(got.NX*got.NX+got.NY*got.NY+got.NZ*got.NZ, 1) {
			t.Fatalf("normal %d is not unit length: %+v", i, got)
		}
		got.NX, got.NY, got.NZ = hardVertices[i].NX, hardVertices[i].NY, hardVertices[i].NZ
		if got != hardVertices[i] {
			t.Fatalf("custom normals altered position, color, or wire attributes at vertex %d", i)
		}
	}
	if !slices.Equal(smooth.Geometry().Indices(), hard.Geometry().Indices()) {
		t.Fatal("custom normals changed visible geometry indices")
	}
}

func TestExplicitNormalsOwnInputsAndPreserveSourcePicking(t *testing.T) {
	vertices, normals, indices := foldedMeshData()
	indices = append([]uint32{0, 0, 0}, indices...) // Omitted face must keep later source indices.
	colors := []Color{ColorHex(0xff0000, 1), ColorHex(0x80b0d0, 1), ColorHex(0xa0d0f0, 1)}
	smooth, err := NewMeshWithNormals(vertices, normals, indices, colors)
	if err != nil {
		t.Fatal(err)
	}
	hard, err := NewMesh(vertices, indices, colors)
	if err != nil {
		t.Fatal(err)
	}
	if smooth.TriangleCount() != 2 {
		t.Fatal("supplied normals retained a degenerate triangle")
	}
	ray := Ray{Origin: Vec3{.2, 0, 3}, Direction: Vec3{0, 0, -1}, MaxDistance: 10}
	face, distance, found := smooth.raycast(ray, 10)
	hardFace, hardDistance, hardFound := hard.raycast(ray, 10)
	if !found || !hardFound || face != 2 || face != hardFace || distance != hardDistance {
		t.Fatalf("lighting normals altered source picking: smooth=(%d,%g,%t) hard=(%d,%g,%t)", face, distance, found, hardFace, hardDistance, hardFound)
	}
	before, beforeIndices := smooth.Geometry().Vertices(), smooth.Geometry().Indices()
	vertices[1], normals[1], indices[3], colors[1].A = Vec3{99, 99, 99}, Vec3{1, 0, 0}, 3, 0
	exported, exportedIndices := smooth.Geometry().Vertices(), smooth.Geometry().Indices()
	exported[1].NZ, exportedIndices[1] = -1, 99
	if !slices.Equal(before, smooth.Geometry().Vertices()) || !slices.Equal(beforeIndices, smooth.Geometry().Indices()) {
		t.Fatal("caller mutation changed retained smooth geometry")
	}
	afterFace, afterDistance, afterFound := smooth.raycast(ray, 10)
	if afterFace != face || afterDistance != distance || afterFound != found {
		t.Fatal("caller mutation changed original-geometry picking")
	}
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := NewScene()
	s.Add(0, Node{Mesh: smooth, Transform: Translate(-1, 0, 0)})
	s.Add(0, Node{Mesh: smooth, Transform: Translate(1, 0, 0)})
	s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
	draws := c.Frame().Commands[0].Draws
	if len(draws) != 2 || draws[0].Geometry != draws[1].Geometry || draws[0].Geometry != smooth.Geometry() {
		t.Fatal("instances duplicated the immutable smooth-normal resource")
	}
}

func TestExplicitNormalsRejectMalformedInputs(t *testing.T) {
	vertices, normals, indices := foldedMeshData()
	for name, malformed := range map[string][]Vec3{
		"missing": nil, "short": normals[:3], "long": append(append([]Vec3(nil), normals...), Vec3{Z: 1}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewMeshWithNormals(vertices, malformed, indices, nil); err == nil {
				t.Fatal("accepted a normal count different from the source vertices")
			}
		})
	}
	for _, invalid := range []Vec3{{}, {X: float32(math.NaN())}, {Y: float32(math.Inf(1))}, {Z: float32(math.Inf(-1))}} {
		bad := append([]Vec3(nil), normals...)
		bad[2] = invalid
		if _, err := NewMeshWithNormals(vertices, bad, indices, nil); err == nil {
			t.Fatalf("accepted non-finite or zero normal %+v", invalid)
		}
	}
	for _, invalid := range [][]uint32{{0, 1}, {0, 1, 99}, {0, 0, 0}} {
		if _, err := NewMeshWithNormals(vertices, normals, invalid, nil); err == nil {
			t.Fatal("custom normals bypassed geometry validation")
		}
	}
}

func TestExplicitNormalsNormalizeFiniteExtremeMagnitudes(t *testing.T) {
	vertices, normals, indices := foldedMeshData()
	normals[0] = Vec3{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	normals[1] = Vec3{Z: math.SmallestNonzeroFloat32}
	mesh, err := NewMeshWithNormals(vertices, normals, indices, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range mesh.Geometry().Vertices() {
		if !finite(v.NX) || !finite(v.NY) || !finite(v.NZ) || !closeTo(v.NX*v.NX+v.NY*v.NY+v.NZ*v.NZ, 1) {
			t.Fatalf("finite input produced a non-unit GPU normal at vertex %d: %+v", i, v)
		}
	}
}
