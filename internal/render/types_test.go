package render

import "testing"

func TestGeometryOwnsImmutableData(t *testing.T) {
	vertices := []MeshVertex{{X: -1}, {X: 1}, {Y: 1}}
	indices := []uint32{0, 1, 2}
	g, err := NewGeometry(vertices, indices)
	if err != nil {
		t.Fatal(err)
	}
	vertices[0].X = 99
	indices[0] = 2
	copyV, copyI := g.Vertices(), g.Indices()
	if copyV[0].X != -1 || copyI[0] != 0 {
		t.Fatal("caller mutation changed geometry")
	}
	copyV[0].X = 88
	copyI[0] = 1
	if g.Vertices()[0].X != -1 || g.Indices()[0] != 0 {
		t.Fatal("accessor exposed mutable resource data")
	}
	other, err := NewGeometry(vertices, indices)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID() == g.ID() || g.ID() == 0 {
		t.Fatal("resource identities must be unique")
	}
	if _, err := NewGeometry(vertices, []uint32{0, 1, 9}); err == nil {
		t.Fatal("out-of-bounds geometry accepted")
	}
}
