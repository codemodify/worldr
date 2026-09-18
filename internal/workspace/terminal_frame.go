package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

// Files shares the terminal's cinematic rails and grip. Photo and media
// viewers keep their own presentation styles.
func cinematicFrameSurface(surface experience.ApplicationSurface) bool {
	if surface.FrameStyle != "" {
		return surface.FrameStyle == experience.FrameCinematic
	}
	switch surface.AppID {
	case "worldr.model-inspector", "worldr.project-browser", "worldr.native-terminal", "foot", "footclient", "org.kde.konsole":
		return true
	}
	return false
}

func (w *Workspace) frameMeshFor(surface experience.ApplicationSurface) *scene.Mesh {
	if photoFrameSurface(surface) {
		if w.photoFrameMesh == nil {
			mesh, err := photoFrameMesh()
			if err != nil {
				panic(err) // Invalid constant frame geometry is a programming error.
			}
			w.photoFrameMesh = mesh
		}
		return w.photoFrameMesh
	}
	if !cinematicFrameSurface(surface) {
		return w.applicationFrameMesh
	}
	if w.terminalFrameMesh == nil {
		mesh, err := terminalFrameMesh()
		if err != nil {
			panic(err) // Invalid constant frame geometry is a programming error.
		}
		w.terminalFrameMesh = mesh
	}
	return w.terminalFrameMesh
}

type framePoint struct{ x, y float32 }
type frameGeometry struct {
	vertices []scene.Vec3
	indices  []uint32
	colors   []scene.Color
}

// Convex plates and joined strips are retained geometry, with no bitmap border
// or per-frame tessellation. Coordinates are local to the application's plane.
func (g *frameGeometry) polygon(color scene.Color, points ...framePoint) {
	if len(points) < 3 {
		return
	}
	base := uint32(len(g.vertices))
	for _, p := range points {
		g.vertices = append(g.vertices, scene.Vec3{X: p.x, Y: p.y})
	}
	for i := uint32(1); i+1 < uint32(len(points)); i++ {
		g.indices = append(g.indices, base, base+i, base+i+1)
		g.colors = append(g.colors, color)
	}
}

func (g *frameGeometry) rect(left, bottom, right, top float32, color scene.Color) {
	g.polygon(color, framePoint{left, bottom}, framePoint{right, bottom}, framePoint{right, top}, framePoint{left, top})
}

// wall adds an explicit rearward side surface along a front-plane path. It is
// opt-in so decorative strokes and the separately pickable drag grip remain
// planar. No rear cap is emitted: these translucent depth-read-only frames do
// not self-write depth, and a cap would blend twice with their front details.
func (g *frameGeometry) wall(depth float32, color scene.Color, closed bool, points ...framePoint) {
	if depth <= 0 || len(points) < 2 {
		return
	}
	segments := len(points) - 1
	if closed {
		segments++
	}
	for i := 0; i < segments; i++ {
		a, b := points[i], points[(i+1)%len(points)]
		base := uint32(len(g.vertices))
		g.vertices = append(g.vertices,
			scene.Vec3{X: a.x, Y: a.y},
			scene.Vec3{X: b.x, Y: b.y},
			scene.Vec3{X: b.x, Y: b.y, Z: -depth},
			scene.Vec3{X: a.x, Y: a.y, Z: -depth},
		)
		g.indices = append(g.indices, base, base+1, base+2, base, base+2, base+3)
		g.colors = append(g.colors, color, color)
	}
}

func (g *frameGeometry) stroke(width float32, color scene.Color, points ...framePoint) {
	// The workspace configures compact and wide terminals at the same 8:5
	// aspect. Correct that aspect when constructing equal-width angled rails.
	const aspect = float32(1.6)
	normals := make([]framePoint, len(points)-1)
	for i := range normals {
		dx, dy := points[i+1].x-points[i].x, (points[i+1].y-points[i].y)/aspect
		length := float32(math.Hypot(float64(dx), float64(dy)))
		normals[i] = framePoint{-dy / length, dx / length}
	}
	left, right := make([]framePoint, len(points)), make([]framePoint, len(points))
	for i, p := range points {
		normal := normals[min(i, len(normals)-1)]
		if i > 0 && i < len(points)-1 {
			previous := normals[i-1]
			denominator := 1 + previous.x*normal.x + previous.y*normal.y
			normal = framePoint{(normal.x + previous.x) / denominator, (normal.y + previous.y) / denominator}
		}
		dx, dy := normal.x*width/2, normal.y*width/2*aspect
		left[i], right[i] = framePoint{p.x + dx, p.y + dy}, framePoint{p.x - dx, p.y - dy}
	}
	for i := 0; i+1 < len(points); i++ {
		g.polygon(color, left[i], right[i], right[i+1], left[i+1])
	}
}

func (g *frameGeometry) mesh() (*scene.Mesh, error) {
	return scene.NewMesh(g.vertices, g.indices, g.colors)
}

// Clip an outer octagon to disjoint bands outside the full rectangular content
// aperture. Even the chamfered chassis leaves the application's corners intact.
func clipFramePolygon(points []framePoint, axis int, limit float32, greater bool) []framePoint {
	coordinate := func(p framePoint) float32 {
		if axis == 0 {
			return p.x
		}
		return p.y
	}
	inside := func(p framePoint) bool {
		if greater {
			return coordinate(p) >= limit
		}
		return coordinate(p) <= limit
	}
	var result []framePoint
	for i, current := range points {
		previous := points[(i+len(points)-1)%len(points)]
		if inside(previous) != inside(current) {
			t := (limit - coordinate(previous)) / (coordinate(current) - coordinate(previous))
			result = append(result, framePoint{previous.x + (current.x-previous.x)*t, previous.y + (current.y-previous.y)*t})
		}
		if inside(current) {
			result = append(result, current)
		}
	}
	return result
}

func terminalFrameMesh() (*scene.Mesh, error) {
	var g frameGeometry
	navy := scene.ColorHex(0x082c39, .96)
	cyan := scene.ColorHex(0x0ac8f5, 1)
	dim := scene.ColorHex(0x087594, .84)
	fine := scene.ColorHex(0x5fcee3, .60)
	outer := []framePoint{{-.494, .57}, {.494, .57}, {.548, .484}, {.548, -.484}, {.494, -.57}, {-.494, -.57}, {-.548, -.484}, {-.548, .484}}
	const aperture = float32(.505)
	const depth = float32(.15)
	// Only the chassis silhouette gains depth. Fine rails and inlays stay on
	// the face, preserving their crisp single-layer color and the grip geometry.
	g.wall(depth, scene.ColorHex(0x04202a, .94), true, outer...)
	for _, edge := range [][2]framePoint{
		{{-aperture, -aperture}, {aperture, -aperture}},
		{{-aperture, -aperture}, {-aperture, aperture}},
		{{aperture, -aperture}, {aperture, aperture}},
		{{-aperture, aperture}, {-.19, aperture}},
		{{.19, aperture}, {aperture, aperture}},
	} {
		g.wall(depth, scene.ColorHex(0x15566a, .82), false, edge[0], edge[1])
	}
	top := clipFramePolygon(outer, 1, aperture, true)
	// The central top plate is a separate drag target, with no coplanar
	// decorative chassis below it to double-blend or obscure its hit region.
	g.polygon(navy, clipFramePolygon(top, 0, -.19, false)...)
	g.polygon(navy, clipFramePolygon(top, 0, .19, true)...)
	g.polygon(navy, clipFramePolygon(outer, 1, -aperture, false)...)
	middle := clipFramePolygon(clipFramePolygon(outer, 1, aperture, false), 1, -aperture, true)
	g.polygon(navy, clipFramePolygon(middle, 0, -aperture, false)...)
	g.polygon(navy, clipFramePolygon(middle, 0, aperture, true)...)

	for _, sign := range []float32{-1, 1} {
		path := func(points ...framePoint) []framePoint {
			for i := range points {
				points[i].x *= sign
			}
			return points
		}
		// Two separated cyan rails wrap each chamfer, with a slim inner line
		// defining the rectangular content aperture.
		g.stroke(.0035, cyan, path(framePoint{.19, .557}, framePoint{.490, .557}, framePoint{.536, .483}, framePoint{.536, -.483}, framePoint{.490, -.557}, framePoint{.19, -.557})...)
		g.stroke(.0011, dim, path(framePoint{.33, .541}, framePoint{.484, .541}, framePoint{.526, .474}, framePoint{.526, -.474}, framePoint{.484, -.541}, framePoint{.36, -.541})...)
		g.stroke(.001, fine, path(framePoint{.19, .508}, framePoint{.507, .508}, framePoint{.507, -.508}, framePoint{.19, -.508})...)
		// Broad, short corner inlays contrast with the fine continuous rails.
		g.polygon(dim, path(framePoint{.36, .548}, framePoint{.482, .548}, framePoint{.494, .529}, framePoint{.37, .529})...)
		g.stroke(.0035, cyan, path(framePoint{.43, -.552}, framePoint{.487, -.552}, framePoint{.507, -.520})...)
		g.stroke(.001, dim, path(framePoint{.550, .41}, framePoint{.550, .483}, framePoint{.497, .568}, framePoint{.452, .568})...)
	}
	// A recessed connecting rail completes the silhouette when Read mode
	// hides the grip. In space it follows just outside the grab plate's edges.
	g.stroke(.0018, cyan, framePoint{-.192, .557}, framePoint{-.167, .513}, framePoint{.167, .513}, framePoint{.192, .557})
	// A quiet mechanical scale on the right and three interrupted left rails.
	for i := 0; i <= 20; i++ {
		y := float32(i)*.032 - .32
		length := float32(.005)
		if i%5 == 0 {
			length = .010
		}
		g.rect(.512, y, .512+length, y+.0015, fine)
	}
	for _, y := range []float32{-.22, -.018, .184} {
		g.rect(-.528, y, -.518, y+.036, dim)
		g.rect(-.528, y+.041, -.524, y+.054, cyan)
	}
	// A recessed lower rail with open ends, as in the reference's layered
	// instrumentation frames. These marks are decoration, not invented data.
	g.polygon(dim, framePoint{-.35, -.53}, framePoint{.35, -.53}, framePoint{.331, -.556}, framePoint{-.331, -.556})
	g.rect(-.29, -.538, .29, -.534, cyan)
	g.rect(-.13, -.557, .13, -.555, fine)
	return g.mesh()
}

func terminalDragHandleMesh() (*scene.Mesh, error) {
	var g frameGeometry
	g.polygon(scene.ColorHex(0x0b485b, 1), framePoint{-.19, .557}, framePoint{.19, .557}, framePoint{.166, .519}, framePoint{-.166, .519})
	g.stroke(.002, scene.ColorHex(0x16d3ff, 1), framePoint{-.176, .552}, framePoint{.176, .552})
	g.stroke(.0012, scene.ColorHex(0x29778e, 1), framePoint{-.164, .523}, framePoint{.164, .523})
	for _, x := range []float32{-.025, -.003, .019} {
		g.rect(x, .534, x+.010, .538, scene.ColorHex(0xa6f4ff, .90))
	}
	return g.mesh()
}
