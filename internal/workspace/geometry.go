package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/scene"
)

// meshBuilder constructs closed surfaces rather than screen-space decoration.
type meshBuilder struct {
	vertices []scene.Vec3
	normals  []scene.Vec3
	indices  []uint32
}

func (b *meshBuilder) quad(a, c, d, e scene.Vec3) {
	i := uint32(len(b.vertices))
	b.vertices = append(b.vertices, a, c, d, a, d, e)
	for _, triangle := range [][3]scene.Vec3{{a, c, d}, {a, d, e}} {
		normal := triangle[1].Sub(triangle[0]).Cross(triangle[2].Sub(triangle[0])).Normalize()
		if normal == (scene.Vec3{}) {
			// The constructor omits degenerate triangles, but their unused
			// source vertices must still carry a valid normal.
			normal = scene.Vec3{X: 1}
		}
		b.normals = append(b.normals, normal, normal, normal)
	}
	b.indices = append(b.indices, i, i+1, i+2, i+3, i+4, i+5)
}

func (b *meshBuilder) curvedQuad(a, c, d, e, firstNormal, lastNormal scene.Vec3) {
	b.quad(a, c, d, e)
	n := len(b.normals)
	copy(b.normals[n-6:], []scene.Vec3{firstNormal, firstNormal, lastNormal, firstNormal, lastNormal, lastNormal})
}

func point(x, r, a float32) scene.Vec3 {
	return scene.Vec3{X: x, Y: r * float32(math.Cos(float64(a))), Z: r * float32(math.Sin(float64(a)))}
}

func (b *meshBuilder) tube(x0, x1, inner, outer, start, end float32, steps int) {
	for i := 0; i < steps; i++ {
		a := start + (end-start)*float32(i)/float32(steps)
		c := start + (end-start)*float32(i+1)/float32(steps)
		// Only curved walls share radial normals. End caps, cutaway seams,
		// collars and blade edges keep their deliberately hard boundaries.
		b.curvedQuad(point(x0, outer, a), point(x1, outer, a), point(x1, outer, c), point(x0, outer, c), point(0, 1, a), point(0, 1, c))
		b.curvedQuad(point(x0, inner, c), point(x1, inner, c), point(x1, inner, a), point(x0, inner, a), point(0, -1, c), point(0, -1, a))
		b.quad(point(x0, inner, a), point(x0, outer, a), point(x0, outer, c), point(x0, inner, c))
		b.quad(point(x1, inner, c), point(x1, outer, c), point(x1, outer, a), point(x1, inner, a))
	}
	if end-start < 6.28 {
		b.quad(point(x0, inner, start), point(x1, inner, start), point(x1, outer, start), point(x0, outer, start))
		b.quad(point(x0, outer, end), point(x1, outer, end), point(x1, inner, end), point(x0, inner, end))
	}
}

func housingMesh() (*scene.Mesh, error) {
	b := new(meshBuilder)
	// A sixty degree opening makes the blade stack visible from above.
	start, end := float32(math.Pi/6), float32(11*math.Pi/6)
	b.tube(-0.75, 0.8, 1.73, 1.84, start, end, 60)
	b.tube(-0.8, -0.63, 1.70, 1.95, start, end, 60)
	b.tube(0.68, 0.88, 1.70, 1.95, start, end, 60)
	for i := 0; i < 7; i++ {
		x := -0.48 + float32(i)*0.16
		b.tube(x, x+0.035, 1.835, 1.90, start, end, 60)
	}
	return scene.NewMeshWithNormals(b.vertices, b.normals, b.indices, nil)
}

// Two narrow light bands follow the housing lips, including the cutaway.
// They are retained scene geometry, so perspective, explosion and occlusion
// apply naturally. The housing remains the sole interaction target.
func housingAccentMesh() (*scene.Mesh, error) {
	b := new(meshBuilder)
	start, end := float32(math.Pi/6), float32(11*math.Pi/6)
	b.tube(-.785, -.75, 1.951, 1.964, start, end, 60)
	b.tube(.825, .86, 1.951, 1.964, start, end, 60)
	return scene.NewMeshWithNormals(b.vertices, b.normals, b.indices, nil)
}

func rotorMesh() (*scene.Mesh, error) {
	b := new(meshBuilder)
	b.tube(-0.35, 0.35, 0.25, 0.68, 0, 2*math.Pi, 40)
	for i := 0; i < 18; i++ {
		a := float32(i) * 2 * math.Pi / 18
		p := [8]scene.Vec3{
			point(-0.28, 0.62, a), point(0.20, 1.56, a+0.08), point(0.30, 1.56, a+0.21), point(-0.18, 0.62, a+0.17),
			point(-0.23, 0.62, a), point(0.25, 1.56, a+0.08), point(0.35, 1.56, a+0.21), point(-0.13, 0.62, a+0.17),
		}
		b.quad(p[0], p[1], p[2], p[3])
		b.quad(p[7], p[6], p[5], p[4])
		for j := 0; j < 4; j++ {
			k := (j + 1) % 4
			b.quad(p[j], p[j+4], p[k+4], p[k])
		}
	}
	return scene.NewMeshWithNormals(b.vertices, b.normals, b.indices, nil)
}

func shaftMesh() (*scene.Mesh, error) {
	b := new(meshBuilder)
	b.tube(-2.7, 2.7, 0, 0.24, 0, 2*math.Pi, 32)
	b.tube(-1.30, -1.12, 0.22, 0.38, 0, 2*math.Pi, 32)
	b.tube(1.12, 1.30, 0.22, 0.38, 0, 2*math.Pi, 32)
	return scene.NewMeshWithNormals(b.vertices, b.normals, b.indices, nil)
}
