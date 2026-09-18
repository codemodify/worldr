package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/scene"
)

// dnaMesh is a stylized double helix, not an atomic model. Its two continuous
// rails and paired rungs are real smooth-shaded tubes. Construction is bounded
// at 12,160 triangles; animation only needs to transform the retained mesh.
func dnaMesh() (*scene.Mesh, error) {
	const (
		height       = float32(10)
		helixRadius  = float32(1.05)
		turnAngle    = float32(5 * math.Pi)
		railRadius   = float32(.035)
		railSegments = 240
		railSides    = 10
		rungCount    = 40
	)
	b := dnaGeometry{
		vertices: make([]scene.Vec3, 0, 7540),
		normals:  make([]scene.Vec3, 0, 7540),
		indices:  make([]uint32, 0, 12160*3),
		colors:   make([]scene.Color, 0, 12160),
	}
	strandColors := [2]scene.Color{scene.ColorHex(0x69e5f3, 1), scene.ColorHex(0x269eaf, 1)}
	for strand := 0; strand < 2; strand++ {
		first := uint32(len(b.vertices))
		for segment := 0; segment <= railSegments; segment++ {
			u := float32(segment) / railSegments
			angle := turnAngle*u + float32(strand)*math.Pi
			cos, sin := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
			center := scene.Vec3{X: helixRadius * cos, Y: height * (u - .5), Z: helixRadius * sin}
			radial := scene.Vec3{X: cos, Z: sin}
			tangent := scene.Vec3{X: -helixRadius * turnAngle * sin, Y: height, Z: helixRadius * turnAngle * cos}.Normalize()
			across := tangent.Cross(radial).Normalize()
			for side := 0; side < railSides; side++ {
				phase := float64(side) * 2 * math.Pi / railSides
				normal := radial.Mul(float32(math.Cos(phase))).Add(across.Mul(float32(math.Sin(phase))))
				b.vertices = append(b.vertices, center.Add(normal.Mul(railRadius)))
				b.normals = append(b.normals, normal)
			}
		}
		for segment := 0; segment < railSegments; segment++ {
			color := strandColors[strand]
			color.A = dnaEndFade(height * ((float32(segment)+.5)/railSegments - .5))
			for side := 0; side < railSides; side++ {
				a := first + uint32(segment*railSides+side)
				c := first + uint32(segment*railSides+(side+1)%railSides)
				b.quad(a, c, c+railSides, a+railSides, color)
			}
		}
	}
	for rung := 0; rung < rungCount; rung++ {
		u := (float32(rung) + .5) / rungCount
		angle, y := turnAngle*u, height*(u-.5)
		radial := scene.Vec3{X: float32(math.Cos(float64(angle))), Z: float32(math.Sin(float64(angle)))}
		center := scene.Vec3{Y: y}
		for strand := 0; strand < 2; strand++ {
			sign := float32(1 - 2*strand)
			color := strandColors[strand]
			color.A = .65 * dnaEndFade(y)
			// A narrow seam makes each crossbar read as a pair without implying
			// a particular sequence or displaying invented biological data.
			start := center.Add(radial.Mul(sign * helixRadius))
			end := center.Add(radial.Mul(sign * .025))
			b.cylinder(start, end, .018, 8, color)
		}
	}
	return scene.NewMeshWithNormals(b.vertices, b.normals, b.indices, b.colors)
}

func dnaEndFade(y float32) float32 {
	distance := float32(5) - float32(math.Abs(float64(y)))
	t := max(float32(0), min(float32(1), distance/1.1))
	return t * t * (3 - 2*t)
}

type dnaGeometry struct {
	vertices, normals []scene.Vec3
	indices           []uint32
	colors            []scene.Color
}

func (b *dnaGeometry) quad(a, c, d, e uint32, color scene.Color) {
	b.indices = append(b.indices, a, c, d, a, d, e)
	b.colors = append(b.colors, color, color)
}

func (b *dnaGeometry) cylinder(start, end scene.Vec3, radius float32, sides int, color scene.Color) {
	axis := end.Sub(start).Normalize()
	up := scene.Vec3{Y: 1} // Every rung lies in the horizontal plane.
	across := axis.Cross(up).Normalize()
	first := uint32(len(b.vertices))
	for _, center := range [2]scene.Vec3{start, end} {
		for side := 0; side < sides; side++ {
			angle := float64(side) * 2 * math.Pi / float64(sides)
			normal := up.Mul(float32(math.Cos(angle))).Add(across.Mul(float32(math.Sin(angle))))
			b.vertices = append(b.vertices, center.Add(normal.Mul(radius)))
			b.normals = append(b.normals, normal)
		}
	}
	for side := 0; side < sides; side++ {
		a, c := first+uint32(side), first+uint32((side+1)%sides)
		b.quad(a, c, c+uint32(sides), a+uint32(sides), color)
	}
	// End caps use separate vertices so their normals stay flat while the
	// circumference retains smooth cylindrical shading.
	for cap, center := range [2]scene.Vec3{start, end} {
		normal := axis.Mul(float32(2*cap - 1))
		middle := uint32(len(b.vertices))
		b.vertices = append(b.vertices, center)
		b.normals = append(b.normals, normal)
		for side := 0; side < sides; side++ {
			b.vertices = append(b.vertices, b.vertices[first+uint32(cap*sides+side)])
			b.normals = append(b.normals, normal)
		}
		for side := 0; side < sides; side++ {
			a, c := middle+1+uint32(side), middle+1+uint32((side+1)%sides)
			if cap == 0 {
				a, c = c, a
			}
			b.indices = append(b.indices, middle, a, c)
			b.colors = append(b.colors, color)
		}
	}
}
