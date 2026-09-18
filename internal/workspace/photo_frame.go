package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func photoFrameSurface(surface experience.ApplicationSurface) bool {
	if surface.FrameStyle != "" {
		return surface.FrameStyle == experience.FramePhotoBracket
	}
	return surface.AppID == "worldr.photo-viewer"
}

// The open bracket belongs to the scene, not the image texture. Its left rail
// and partial top/bottom returns sit entirely outside the photograph; the right
// side stays open. Shared retained geometry follows placement and image aspect.
func photoFrameMesh() (*scene.Mesh, error) {
	var g frameGeometry
	cyan := scene.ColorHex(0x87daeb, .96)
	light := scene.ColorHex(0xd0f9ff, .96)
	dim := scene.ColorHex(0x397c98, .70)
	fine := scene.ColorHex(0x70cadf, .70)
	// Trace the open bracket's complete outer and inner silhouette. Its wall is
	// entirely behind the photograph and stops with the short returns, keeping
	// the right side visually open while giving the retained bracket real depth.
	g.wall(.10, scene.ColorHex(0x24556a, .86), true,
		framePoint{-.091, .510}, framePoint{-.105, .513}, framePoint{-.394, .519},
		framePoint{-.502, .519}, framePoint{-.530, .502}, framePoint{-.536, .483},
		framePoint{-.536, -.483}, framePoint{-.530, -.502}, framePoint{-.502, -.519},
		framePoint{-.394, -.519}, framePoint{-.105, -.513}, framePoint{-.091, -.510},
		framePoint{-.098, -.510}, framePoint{-.394, -.507}, framePoint{-.502, -.507},
		framePoint{-.513, -.484}, framePoint{-.513, .484}, framePoint{-.502, .507},
		framePoint{-.394, .507}, framePoint{-.098, .510},
	)

	g.rect(-.536, -.483, -.530, .483, dim)
	g.rect(-.530, -.484, -.521, .484, cyan)
	g.rect(-.524, -.473, -.521, .473, light)
	// The recessed inside edge adds depth without touching any image pixels.
	g.rect(-.514, -.445, -.513, .445, dim)
	for _, sign := range []float32{-1, 1} {
		// Rounded quarter bends join the upright to its short, heavier feet.
		const segments = 10
		for i := 0; i < segments; i++ {
			a := math.Pi/2 + float64(i)*math.Pi/(2*segments)
			b := math.Pi/2 + float64(i+1)*math.Pi/(2*segments)
			point := func(angle float64, rx, ry float32) framePoint {
				return framePoint{-.502 + rx*float32(math.Cos(angle)), sign * (.484 + ry*float32(math.Sin(angle)))}
			}
			g.polygon(cyan, point(a, .028, .035), point(b, .028, .035), point(b, .019, .023), point(a, .019, .023))
		}
		plate := func(left, bottom, right, top float32, color scene.Color) {
			if sign < 0 {
				bottom, top = -top, -bottom
			}
			g.rect(left, bottom, right, top, color)
		}
		plate(-.502, .507, -.394, .519, cyan)
		plate(-.502, .515, -.409, .518, light)
		// Slim extensions end before the middle of the top and bottom edges.
		plate(-.394, .510, -.105, .513, fine)
		plate(-.388, .506, -.23, .507, dim)
		g.polygon(fine, framePoint{-.105, sign * .510}, framePoint{-.091, sign * .510}, framePoint{-.098, sign * .513}, framePoint{-.105, sign * .513})
	}
	return g.mesh()
}
