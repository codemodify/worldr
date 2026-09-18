package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/scene"
)

// The stage is a sparse coordinate reference beneath the assembly. It shares
// the camera and depth buffer with real content, and never participates in
// picking. Geometry is built once; presentation changes only its node alpha.
func stageMesh() (*scene.Mesh, error) {
	b := new(meshBuilder)
	var colors []scene.Color
	quad := func(a, c, d, e scene.Vec3, alpha float32) {
		b.quad(a, c, d, e)
		color := scene.Color{R: 1, G: 1, B: 1, A: alpha}
		colors = append(colors, color, color)
	}
	const floor = float32(-2.3)
	// Short segments let the lattice fade at its boundary without another
	// texture, shader, or frame-time geometry generation.
	for line := -6; line <= 6; line++ {
		position := float32(line) * .75
		width := float32(.007)
		if line == 0 {
			width = .014
		}
		for segment := -12; segment < 12; segment++ {
			lo, hi := float32(segment)*.4, float32(segment+1)*.4
			middle := (lo + hi) / 2
			fade := max(float32(0), 1-(position*position+middle*middle)/25)
			if fade == 0 {
				continue
			}
			alpha := .5 * fade * fade
			quad(scene.Vec3{X: lo, Y: floor, Z: position - width}, scene.Vec3{X: hi, Y: floor, Z: position - width}, scene.Vec3{X: hi, Y: floor, Z: position + width}, scene.Vec3{X: lo, Y: floor, Z: position + width}, alpha)
			quad(scene.Vec3{X: position - width, Y: floor, Z: lo}, scene.Vec3{X: position + width, Y: floor, Z: lo}, scene.Vec3{X: position + width, Y: floor, Z: hi}, scene.Vec3{X: position - width, Y: floor, Z: hi}, alpha)
		}
	}
	for ring := 0; ring < 2; ring++ {
		radius := float32(2.45) + float32(ring)*.85
		point := func(angle, r float32) scene.Vec3 {
			return scene.Vec3{X: r * float32(math.Cos(float64(angle))), Y: floor + .003, Z: r * float32(math.Sin(float64(angle)))}
		}
		for segment := 0; segment < 160; segment++ {
			a, z := float32(segment)*2*math.Pi/160, float32(segment+1)*2*math.Pi/160
			quad(point(a, radius-.011), point(z, radius-.011), point(z, radius+.011), point(a, radius+.011), .7-float32(ring)*.25)
		}
	}
	return scene.NewMesh(b.vertices, b.indices, colors)
}
