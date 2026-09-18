package scene

import "testing"

func TestMeshBoundsUseVisibleImmutableGeometry(t *testing.T) {
	vertices := []Vec3{{-2, -3, -4}, {3, 1, 2}, {-1, 4, 5}, {100, 100, 100}, {101, 100, 100}, {100, 101, 100}}
	mesh, err := NewMesh(vertices, []uint32{0, 1, 2, 3, 4, 5, 3, 3, 3}, []Color{{1, 1, 1, 1}, {1, 1, 1, 0}, {1, 1, 1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	vertices[0] = Vec3{1000, 1000, 1000}
	minimum, maximum := mesh.Bounds()
	if minimum != (Vec3{-2, -3, -4}) || maximum != (Vec3{3, 4, 5}) {
		t.Fatalf("bounds include hidden/degenerate/input-mutated geometry: %v..%v", minimum, maximum)
	}
	minimum.X = 200
	again, _ := mesh.Bounds()
	if again.X != -2 {
		t.Fatal("returned bounds alias immutable mesh")
	}
	var nilMesh *Mesh
	a, b := nilMesh.Bounds()
	if a != (Vec3{}) || b != (Vec3{}) {
		t.Fatal("nil mesh bounds")
	}
}
