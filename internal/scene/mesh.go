package scene

import (
	"fmt"
	"math"
	"sort"

	"github.com/codemodify/worldr/internal/render"
)

// Mesh is immutable. Construction copies and validates local geometry, creates
// the GPU resource once, and builds a local-space hierarchy for input queries.
// Sharing a Mesh across nodes shares both GPU buffers and the picking index.
type Mesh struct {
	vertices []Vec3
	indices  []uint32
	geometry *render.Geometry
	order    []int
	bvh      []bvhNode
}

// NewMesh builds hard face normals and barycentric edge attributes once.
// faceColors must be empty or have exactly one color per source triangle.
// Degenerate triangles are omitted while Hit.Triangle retains source indices.
func NewMesh(vertices []Vec3, indices []uint32, faceColors []Color) (*Mesh, error) {
	return newMesh(vertices, nil, indices, faceColors)
}

// NewMeshWithNormals builds smooth shading from one normal per source vertex.
// Normals must be finite and nonzero; their length is normalized once. Duplicate
// source vertices where a hard edge needs different normals at the same point.
// Like NewMesh, construction owns its inputs, omits degenerate triangles, and
// retains original source triangle numbers and geometry for picking. Supplied
// normals affect lighting only; they do not change the shape or its bounds.
func NewMeshWithNormals(vertices, normals []Vec3, indices []uint32, faceColors []Color) (*Mesh, error) {
	if len(normals) != len(vertices) {
		return nil, fmt.Errorf("scene mesh normals must match vertex count")
	}
	normalized := make([]Vec3, len(normals))
	for i, normal := range normals {
		if !finite(normal.X) || !finite(normal.Y) || !finite(normal.Z) || normal == (Vec3{}) {
			return nil, fmt.Errorf("scene mesh normal %d must be finite and nonzero", i)
		}
		// Normalize in float64 so any finite nonzero float32 input is accepted,
		// including lengths beyond float32 range and very small normal vectors.
		x, y, z := float64(normal.X), float64(normal.Y), float64(normal.Z)
		length := math.Sqrt(x*x + y*y + z*z)
		normalized[i] = Vec3{float32(x / length), float32(y / length), float32(z / length)}
	}
	return newMesh(vertices, normalized, indices, faceColors)
}

func newMesh(vertices, normals []Vec3, indices []uint32, faceColors []Color) (*Mesh, error) {
	if len(vertices) == 0 || len(indices) == 0 || len(indices)%3 != 0 || uint64(len(vertices)) > math.MaxUint32 || uint64(len(indices)) > math.MaxUint32 {
		return nil, fmt.Errorf("scene mesh needs vertices and complete indexed triangles")
	}
	if len(faceColors) != 0 && len(faceColors) != len(indices)/3 {
		return nil, fmt.Errorf("scene mesh face colors must match triangle count")
	}
	for i, v := range vertices {
		if !finite(v.X) || !finite(v.Y) || !finite(v.Z) {
			return nil, fmt.Errorf("scene mesh vertex %d is not finite", i)
		}
	}
	for i, index := range indices {
		if uint64(index) >= uint64(len(vertices)) {
			return nil, fmt.Errorf("scene mesh index %d is out of range", i)
		}
	}
	m := &Mesh{vertices: append([]Vec3(nil), vertices...), indices: append([]uint32(nil), indices...)}
	gpuVertices := make([]render.MeshVertex, 0, len(indices))
	gpuIndices := make([]uint32, 0, len(indices))
	for face := 0; face < len(indices)/3; face++ {
		a, b, c := m.triangle(face)
		normal := b.Sub(a).Cross(c.Sub(a)).Normalize()
		color := Color{1, 1, 1, 1}
		if len(faceColors) > 0 {
			color = faceColors[face]
		}
		if !finite(color.R) || !finite(color.G) || !finite(color.B) || !finite(color.A) {
			return nil, fmt.Errorf("scene mesh face %d color is not finite", face)
		}
		if normal == (Vec3{}) || color.A <= 0 {
			continue
		}
		m.order = append(m.order, face)
		for corner, p := range [3]Vec3{a, b, c} {
			cornerNormal := normal
			if normals != nil {
				cornerNormal = normals[indices[face*3+corner]]
			}
			vertex := render.MeshVertex{X: p.X, Y: p.Y, Z: p.Z, NX: cornerNormal.X, NY: cornerNormal.Y, NZ: cornerNormal.Z, R: color.R, G: color.G, B: color.B, A: color.A}
			switch corner {
			case 0:
				vertex.BX = 1
			case 1:
				vertex.BY = 1
			case 2:
				vertex.BZ = 1
			}
			gpuIndices = append(gpuIndices, uint32(len(gpuVertices)))
			gpuVertices = append(gpuVertices, vertex)
		}
	}
	if len(gpuIndices) == 0 {
		return nil, fmt.Errorf("scene mesh has no visible nondegenerate triangles")
	}
	geometry, err := render.NewGeometry(gpuVertices, gpuIndices)
	if err != nil {
		return nil, err
	}
	m.geometry = geometry
	m.buildBVH(0, len(m.order))
	return m, nil
}

// Bounds returns immutable local bounds of the visible nondegenerate triangles.
// It uses the BVH root in constant time, without scanning retained vertices.
// Nil and zero meshes return two zero vectors.
func (m *Mesh) Bounds() (minimum, maximum Vec3) {
	if m == nil || len(m.bvh) == 0 {
		return
	}
	return m.bvh[0].bounds.min, m.bvh[0].bounds.max
}

func (m *Mesh) Geometry() *render.Geometry { return m.geometry }
func (m *Mesh) TriangleCount() int         { return len(m.order) }
func (m *Mesh) triangle(face int) (Vec3, Vec3, Vec3) {
	return m.vertices[m.indices[face*3]], m.vertices[m.indices[face*3+1]], m.vertices[m.indices[face*3+2]]
}

type bounds struct{ min, max Vec3 }
type bvhNode struct {
	bounds                    bounds
	left, right, start, count int
}

func emptyBounds() bounds {
	return bounds{Vec3{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}, Vec3{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}}
}
func (b *bounds) include(p Vec3) {
	b.min.X = min(b.min.X, p.X)
	b.min.Y = min(b.min.Y, p.Y)
	b.min.Z = min(b.min.Z, p.Z)
	b.max.X = max(b.max.X, p.X)
	b.max.Y = max(b.max.Y, p.Y)
	b.max.Z = max(b.max.Z, p.Z)
}
func (m *Mesh) buildBVH(start, end int) int {
	box, centroids := emptyBounds(), emptyBounds()
	for _, face := range m.order[start:end] {
		a, b, c := m.triangle(face)
		box.include(a)
		box.include(b)
		box.include(c)
		centroids.include(a.Add(b).Add(c).Mul(1.0 / 3))
	}
	index := len(m.bvh)
	m.bvh = append(m.bvh, bvhNode{bounds: box, start: start, count: end - start, left: -1, right: -1})
	if end-start <= 8 {
		return index
	}
	extent := centroids.max.Sub(centroids.min)
	axis := 0
	if extent.Y > extent.X {
		axis = 1
	}
	if component(extent, 2) > component(extent, axis) {
		axis = 2
	}
	if component(extent, axis) < 1e-7 {
		return index
	}
	partition := m.order[start:end]
	sort.Slice(partition, func(i, j int) bool {
		a, b, c := m.triangle(partition[i])
		d, e, f := m.triangle(partition[j])
		return component(a.Add(b).Add(c), axis) < component(d.Add(e).Add(f), axis)
	})
	mid := (start + end) / 2
	left, right := m.buildBVH(start, mid), m.buildBVH(mid, end)
	m.bvh[index].left, m.bvh[index].right, m.bvh[index].count = left, right, 0
	return index
}

func (b bounds) intersects(ray Ray, limit float32) bool {
	near, far := float32(0), limit
	for axis := 0; axis < 3; axis++ {
		origin, direction := component(ray.Origin, axis), component(ray.Direction, axis)
		lo, hi := component(b.min, axis), component(b.max, axis)
		if abs(direction) < 1e-12 {
			if origin < lo || origin > hi {
				return false
			}
			continue
		}
		a, z := (lo-origin)/direction, (hi-origin)/direction
		if a > z {
			a, z = z, a
		}
		near = max(near, a)
		far = min(far, z)
		if near > far {
			return false
		}
	}
	return true
}

func (m *Mesh) raycast(ray Ray, limit float32) (face int, distance float32, found bool) {
	distance = limit
	var visit func(int)
	visit = func(index int) {
		node := m.bvh[index]
		if !node.bounds.intersects(ray, distance) {
			return
		}
		if node.count == 0 {
			visit(node.left)
			visit(node.right)
			return
		}
		for _, candidate := range m.order[node.start : node.start+node.count] {
			a, b, c := m.triangle(candidate)
			if t, ok := rayTriangle(ray, a, b, c); ok && t <= distance {
				face, distance, found = candidate, t, true
			}
		}
	}
	if len(m.bvh) > 0 {
		visit(0)
	}
	return
}

func rayTriangle(ray Ray, a, b, c Vec3) (float32, bool) {
	edge1, edge2 := b.Sub(a), c.Sub(a)
	p := ray.Direction.Cross(edge2)
	determinant := edge1.Dot(p)
	// Relative tolerance remains stable when instances use nonuniform scales.
	tolerance := 1e-7 * float32(math.Sqrt(float64(edge1.Dot(edge1)*edge2.Dot(edge2)*ray.Direction.Dot(ray.Direction))))
	if abs(determinant) <= tolerance {
		return 0, false
	}
	inverse := 1 / determinant
	delta := ray.Origin.Sub(a)
	u := delta.Dot(p) * inverse
	if u < -1e-6 || u > 1.000001 {
		return 0, false
	}
	q := delta.Cross(edge1)
	v := ray.Direction.Dot(q) * inverse
	if v < -1e-6 || u+v > 1.000001 {
		return 0, false
	}
	t := edge2.Dot(q) * inverse
	return t, t >= 0 && t <= ray.MaxDistance
}

func component(v Vec3, axis int) float32 {
	if axis == 0 {
		return v.X
	}
	if axis == 1 {
		return v.Y
	}
	return v.Z
}
func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
