package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// This compiler consumes the same data-only primitives as the native painter.
// It never dispatches on skin IDs, family names, or a fixed border-style enum.
type windowSkinBuilder struct {
	skin         *skin.Skin
	state        string
	g            frameGeometry
	exclusions   []box
	result       windowSkinPart
	layerDepth   float32
	logicalWidth float32
	pickBounds   []box
}

func newWindowSkinBuilder(s *skin.Skin, aspect float32, state string) *windowSkinBuilder {
	return newWindowSkinBuilderForWidth(s, aspect, 960, state)
}

func newWindowSkinBuilderForWidth(s *skin.Skin, aspect, width float32, state string) *windowSkinBuilder {
	return &windowSkinBuilder{logicalWidth: width, skin: s, state: state, g: frameGeometry{aspect: aspect}, exclusions: []box{{-.5, -.5, 1, 1}}, layerDepth: -.05}
}

func (b *windowSkinBuilder) part() windowSkinPart {
	// Input plates use the exact application plane and draw last. Their tiny
	// coverage keeps visual layers visible while ray picking has generous,
	// stable boxes even for open or rounded button silhouettes.
	b.layerDepth = 0
	for _, bounds := range b.pickBounds {
		b.polygon([]framePoint{{bounds.x, bounds.y}, {bounds.x + bounds.w, bounds.y}, {bounds.x + bounds.w, bounds.y + bounds.h}, {bounds.x, bounds.y + bounds.h}}, func(framePoint) scene.Color {
			return scene.Color{R: 1, G: 1, B: 1, A: .00001}
		})
	}
	if len(b.g.indices) == 0 {
		// A deliberately empty recipe remains valid and pick-neutral. Keep a
		// negligible off-client face rather than substituting a built-in design.
		b.g.rect(-.501, -.502, -.5, -.501, scene.Color{A: .00001})
	}
	mesh, err := b.g.mesh()
	if err != nil {
		// A legal data-only path can be entirely collinear or clipped away.
		// Treat that as empty decoration, never a host-process panic.
		var empty frameGeometry
		empty.rect(-.501, -.502, -.5, -.501, scene.Color{A: .00001})
		mesh, _ = empty.mesh()
	}
	b.result.mesh = mesh
	return b.result
}

func (b *windowSkinBuilder) pickPlate(bounds box) {
	b.pickBounds = append(b.pickBounds, bounds)
}

func (b *windowSkinBuilder) recipe(recipe skin.Recipe, bounds box) {
	state := recipe.States[b.state]
	for _, layer := range recipe.Layers {
		// Distinct authored layers must not be coplanar: transparent depth
		// peeling treats coincident fragments as one surface. Small rearward
		// offsets retain every layer without extending into client pixels.
		b.layerDepth += .0003
		fill, stroke := layer.Fill, layer.Stroke
		if state.Fill != "" && fill != "" {
			fill = state.Fill
		}
		if state.Stroke != "" && stroke != "" {
			stroke = state.Stroke
		}
		area := windowSkinLayerBounds(bounds, layer.Bounds)
		if state.Offset.X != 0 || state.Offset.Y != 0 {
			area.x += float32(state.Offset.X) / b.logicalWidth
			area.y -= float32(state.Offset.Y) / b.logicalWidth * b.g.aspect
		}
		points := windowSkinShape(layer.Geometry, area, b.g.aspect)
		opacity := float32(1)
		if layer.Opacity != 0 {
			opacity *= clampSkinFloat(layer.Opacity)
		}
		if state.Opacity != 0 {
			opacity *= clampSkinFloat(state.Opacity)
		}
		material := b.skin.Materials[layer.Material]
		if material.Opacity != 0 {
			opacity *= clampSkinFloat(material.Opacity)
		}
		if fill != "" {
			color := skinSceneColor(b.skin, fill, opacity)
			secondary := color
			if material.Secondary != "" {
				secondary = skinSceneColor(b.skin, material.Secondary, opacity)
			}
			shade := func(p framePoint) scene.Color {
				if material.Kind != "linear-gradient" && material.Kind != "glass" {
					return color
				}
				angle := material.Angle * math.Pi / 180
				x, y := float64((p.x-area.x)/area.w-.5), float64(.5-(p.y-area.y)/area.h)
				t := clampSkinFloat(.5 + x*math.Cos(angle) + y*math.Sin(angle))
				return scene.Color{R: color.R + (secondary.R-color.R)*t, G: color.G + (secondary.G-color.G)*t, B: color.B + (secondary.B-color.B)*t, A: color.A + (secondary.A-color.A)*t}
			}
			if material.Kind == "linear-gradient" || material.Kind == "glass" {
				// Face colors are immutable. Subdivide the gradient along its main
				// axis rather than producing two visibly different triangle fills.
				b.gradient(points, area, material.Angle, shade)
			} else {
				b.polygon(points, shade)
			}
		}
		if stroke != "" && layer.StrokeWidth > 0 {
			color := skinSceneColor(b.skin, stroke, opacity)
			if layer.Geometry.Kind == "bracketed" {
				b.brackets(area, float32(layer.StrokeWidth)/b.logicalWidth, color)
			} else {
				b.stroke(points, float32(layer.StrokeWidth)/b.logicalWidth, color, true)
			}
		}
		b.material(material, fill, stroke, state.Glow)
	}
	if recipe.Icon != "" {
		b.layerDepth += .0003
		colorToken := recipe.TextColor
		if state.Text != "" {
			colorToken = state.Text
		}
		if colorToken == "" {
			colorToken = "text"
		}
		b.icon(recipe.Icon, bounds, skinSceneColor(b.skin, colorToken, 1))
	}
}

func (b *windowSkinBuilder) gradient(points []framePoint, area box, angle float64, color func(framePoint) scene.Color) {
	axis, start, length := 0, area.x, area.w
	if math.Abs(math.Sin(angle*math.Pi/180)) > math.Abs(math.Cos(angle*math.Pi/180)) {
		axis, start, length = 1, area.y, area.h
	}
	const bands = 16
	for _, triangle := range triangulateWindowSkinPolygon(points) {
		for band := 0; band < bands; band++ {
			lower := start + length*float32(band)/bands
			upper := start + length*float32(band+1)/bands
			piece := clipFramePolygon(clipFramePolygon(triangle[:], axis, lower, true), axis, upper, false)
			b.polygon(piece, color)
		}
	}
}

func (b *windowSkinBuilder) material(material skin.Material, fill, stroke string, stateGlow float64) {
	if material.Kind == "glass" {
		b.result.translucent = true
		b.result.material.Specular = .65
		b.result.material.Roughness = .18 + clampSkinFloat(material.Blur)*.48
		b.result.material.Transmission = .7
		b.result.material.Refraction = max(b.result.material.Refraction, clampSkinFloat(material.Refraction))
		b.result.material.RefractionBlur = max(b.result.material.RefractionBlur, clampSkinFloat(material.Blur))
		b.result.material.RimStrength = .22
	}
	glow := float32(math.Max(0, math.Min(4, math.Max(material.Glow, stateGlow))))
	if glow > 0 || material.Kind == "glass" {
		token := stroke
		if token == "" {
			token = fill
		}
		c := skinSceneColor(b.skin, token, 1)
		b.result.material.RimColor = [3]float32{c.R, c.G, c.B}
		// A skin expresses brightness intent up to four; the scene renderer
		// accepts normalized emitted channels. Saturate each channel only at
		// this boundary so stronger recipes remain bright without invalidating
		// the entire frame.
		b.result.glow[0] = max(b.result.glow[0], min(float32(1), c.R*glow))
		b.result.glow[1] = max(b.result.glow[1], min(float32(1), c.G*glow))
		b.result.glow[2] = max(b.result.glow[2], min(float32(1), c.B*glow))
	}
}

func windowSkinLayerBounds(parent box, relative skin.Box) box {
	if relative == (skin.Box{}) {
		return parent
	}
	return box{parent.x + float32(relative.X)*parent.w, parent.y + (1-float32(relative.Y+relative.H))*parent.h, float32(relative.W) * parent.w, float32(relative.H) * parent.h}
}

func windowSkinShape(shape skin.Geometry, bounds box, aspect float32) []framePoint {
	left, bottom, right, top := bounds.x, bounds.y, bounds.x+bounds.w, bounds.y+bounds.h
	short := min(bounds.w, bounds.h/aspect)
	cornerX := min(short*float32(shape.Corner), bounds.w/2)
	cornerY := min(cornerX*aspect, bounds.h/2)
	switch shape.Kind {
	case "path":
		points := make([]framePoint, 0, len(shape.Points))
		for _, p := range shape.Points {
			points = append(points, framePoint{left + float32(p.X)*bounds.w, top - float32(p.Y)*bounds.h})
		}
		return points
	case "rounded":
		rx := min(short*float32(shape.Radius), bounds.w/2)
		ry := min(rx*aspect, bounds.h/2)
		if rx <= 0 || ry <= 0 {
			break
		}
		points := make([]framePoint, 0, 28)
		for i, center := range []framePoint{{right - rx, top - ry}, {left + rx, top - ry}, {left + rx, bottom + ry}, {right - rx, bottom + ry}} {
			for step := 0; step <= 6; step++ {
				angle := float64(i)*math.Pi/2 + float64(step)*math.Pi/12
				points = append(points, framePoint{center.x + rx*float32(math.Cos(angle)), center.y + ry*float32(math.Sin(angle))})
			}
		}
		return points
	case "chamfered":
		return []framePoint{{left + cornerX, bottom}, {right - cornerX, bottom}, {right, bottom + cornerY}, {right, top - cornerY}, {right - cornerX, top}, {left + cornerX, top}, {left, top - cornerY}, {left, bottom + cornerY}}
	case "bracketed":
		return []framePoint{{left, bottom}, {right - cornerX, bottom}, {right, bottom + cornerY}, {right, top}, {left + cornerX, top}, {left, top - cornerY}}
	case "notched":
		nx := min(short*float32(shape.Notch), bounds.w/3)
		ny := min(nx*aspect, bounds.h/3)
		mid := (bottom + top) / 2
		return []framePoint{{left + cornerX, bottom}, {right - cornerX, bottom}, {right, bottom + cornerY}, {right, mid - ny}, {right - nx, mid}, {right, mid + ny}, {right, top - cornerY}, {right - cornerX, top}, {left + cornerX, top}, {left, top - cornerY}, {left, bottom + cornerY}}
	}
	return []framePoint{{left, bottom}, {right, bottom}, {right, top}, {left, top}}
}

func (b *windowSkinBuilder) polygon(points []framePoint, color func(framePoint) scene.Color) {
	for _, triangle := range triangulateWindowSkinPolygon(points) {
		pieces := [][]framePoint{triangle[:]}
		for _, exclusion := range b.exclusions {
			var next [][]framePoint
			for _, piece := range pieces {
				next = append(next, outsideWindowSkinRect(piece, exclusion)...)
			}
			pieces = next
		}
		for _, piece := range pieces {
			if len(piece) < 3 {
				continue
			}
			var center framePoint
			for _, p := range piece {
				center.x += p.x / float32(len(piece))
				center.y += p.y / float32(len(piece))
			}
			start := len(b.g.vertices)
			b.g.polygon(color(center), piece...)
			for i := start; i < len(b.g.vertices); i++ {
				b.g.vertices[i].Z = b.layerDepth
			}
		}
	}
}

func outsideWindowSkinRect(points []framePoint, r box) [][]framePoint {
	// Snap the clipped axis to its exact half-plane. Interpolated float32
	// intersections can otherwise lie a fraction of a pixel inside the client.
	clip := func(polygon []framePoint, axis int, limit float32, greater bool) []framePoint {
		result := clipFramePolygon(polygon, axis, limit, greater)
		for i := range result {
			value := &result[i].x
			if axis == 1 {
				value = &result[i].y
			}
			if greater {
				*value = max(*value, limit)
			} else {
				*value = min(*value, limit)
			}
		}
		return result
	}
	left := clip(points, 0, r.x, false)
	right := clip(points, 0, r.x+r.w, true)
	middle := clip(clip(points, 0, r.x, true), 0, r.x+r.w, false)
	return [][]framePoint{left, right, clip(middle, 1, r.y, false), clip(middle, 1, r.y+r.h, true)}
}

func (b *windowSkinBuilder) stroke(points []framePoint, width float32, color scene.Color, closed bool) {
	if len(points) < 2 || width <= 0 {
		return
	}
	count := len(points) - 1
	if closed {
		count++
	}
	for i := 0; i < count; i++ {
		a, c := points[i], points[(i+1)%len(points)]
		dx, dy := c.x-a.x, (c.y-a.y)/b.g.aspect
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length < 1e-7 {
			continue
		}
		nx, ny := -dy/length*width/2, dx/length*width/2*b.g.aspect
		b.polygon([]framePoint{{a.x + nx, a.y + ny}, {a.x - nx, a.y - ny}, {c.x - nx, c.y - ny}, {c.x + nx, c.y + ny}}, func(framePoint) scene.Color { return color })
	}
}

func (b *windowSkinBuilder) brackets(r box, width float32, color scene.Color) {
	for _, corner := range []struct{ x, y, dx, dy float32 }{{r.x, r.y, 1, 1}, {r.x + r.w, r.y, -1, 1}, {r.x + r.w, r.y + r.h, -1, -1}, {r.x, r.y + r.h, 1, -1}} {
		b.stroke([]framePoint{{corner.x + corner.dx*r.w*.23, corner.y}, {corner.x, corner.y}, {corner.x, corner.y + corner.dy*r.h*.3}}, width, color, false)
	}
}

func (b *windowSkinBuilder) icon(name string, bounds box, color scene.Color) {
	icon, ok := b.skin.Icons[name]
	if !ok {
		return
	}
	// An icon has a square physical viewport, independent of window aspect.
	size := min(bounds.w, bounds.h/b.g.aspect) * .48
	area := box{bounds.x + (bounds.w-size)/2, bounds.y + (bounds.h-size*b.g.aspect)/2, size, size * b.g.aspect}
	for _, path := range icon.Paths {
		var points []framePoint
		for _, p := range path.Points {
			points = append(points, framePoint{area.x + float32(p.X)*area.w, area.y + (1-float32(p.Y))*area.h})
		}
		if path.Fill {
			b.polygon(points, func(framePoint) scene.Color { return color })
		}
		b.stroke(points, max(float32(1), float32(icon.StrokeWidth))/b.logicalWidth, color, path.Closed)
	}
}

func (b *windowSkinBuilder) defaultWindowGlyph(kind applicationWindowControl, bounds box) {
	c := skinSceneColor(b.skin, "text", 1)
	cx, cy := bounds.x+bounds.w/2, bounds.y+bounds.h/2
	rx := min(bounds.w, bounds.h/b.g.aspect) * .2
	ry := rx * b.g.aspect
	switch kind {
	case windowControlMinimize:
		b.stroke([]framePoint{{cx - rx, cy - ry*.7}, {cx + rx, cy - ry*.7}}, 2.0/b.logicalWidth, c, false)
	case windowControlRead:
		b.stroke([]framePoint{{cx - rx, cy - ry}, {cx + rx, cy - ry}, {cx + rx, cy + ry}, {cx - rx, cy + ry}}, 1.5/b.logicalWidth, c, true)
	case windowControlClose:
		b.stroke([]framePoint{{cx - rx, cy - ry}, {cx + rx, cy + ry}}, 2.0/b.logicalWidth, c, false)
		b.stroke([]framePoint{{cx - rx, cy + ry}, {cx + rx, cy - ry}}, 2.0/b.logicalWidth, c, false)
	}
}

func (b *windowSkinBuilder) addRearWalls(recipe skin.Recipe, bounds box) {
	if len(recipe.Layers) == 0 {
		return
	}
	layer := recipe.Layers[0]
	points := windowSkinShape(layer.Geometry, windowSkinLayerBounds(bounds, layer.Bounds), b.g.aspect)
	token := layer.Fill
	if token == "" {
		token = layer.Stroke
	}
	color := skinSceneColor(b.skin, token, .55)
	if color.A == 0 {
		return
	}
	for i, a := range points {
		c := points[(i+1)%len(points)]
		segments := [][2]framePoint{{a, c}}
		for _, exclusion := range b.exclusions {
			var next [][2]framePoint
			for _, segment := range segments {
				next = append(next, windowSkinSegmentOutside(segment[0], segment[1], exclusion)...)
			}
			segments = next
		}
		for _, segment := range segments {
			b.g.wall(.12, color, false, segment[0], segment[1])
		}
	}
}

func windowSkinSegmentOutside(a, c framePoint, r box) [][2]framePoint {
	dx, dy := c.x-a.x, c.y-a.y
	lo, hi := float32(0), float32(1)
	for _, pair := range [][2]float32{{-dx, a.x - r.x}, {dx, r.x + r.w - a.x}, {-dy, a.y - r.y}, {dy, r.y + r.h - a.y}} {
		p, q := pair[0], pair[1]
		if p == 0 {
			if q < 0 {
				return [][2]framePoint{{a, c}}
			}
			continue
		}
		t := q / p
		if p < 0 {
			lo = max(lo, t)
		} else {
			hi = min(hi, t)
		}
	}
	if lo >= hi {
		return [][2]framePoint{{a, c}}
	}
	var result [][2]framePoint
	if lo > 0 {
		result = append(result, [2]framePoint{a, {a.x + dx*lo, a.y + dy*lo}})
	}
	if hi < 1 {
		result = append(result, [2]framePoint{{a.x + dx*hi, a.y + dy*hi}, c})
	}
	return result
}

func triangulateWindowSkinPolygon(input []framePoint) [][3]framePoint {
	points := make([]framePoint, 0, len(input))
	for _, p := range input {
		if len(points) == 0 || p != points[len(points)-1] {
			points = append(points, p)
		}
	}
	if len(points) > 1 && points[0] == points[len(points)-1] {
		points = points[:len(points)-1]
	}
	if len(points) < 3 {
		return nil
	}
	cross := func(a, c, d framePoint) float32 { return (c.x-a.x)*(d.y-a.y) - (c.y-a.y)*(d.x-a.x) }
	var area float32
	for i, p := range points {
		q := points[(i+1)%len(points)]
		area += p.x*q.y - q.x*p.y
	}
	if area < 0 {
		for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
			points[i], points[j] = points[j], points[i]
		}
	}
	var result [][3]framePoint
	for len(points) >= 3 {
		clipped := false
		for i := range points {
			previous, next := (i+len(points)-1)%len(points), (i+1)%len(points)
			a, c, d := points[previous], points[i], points[next]
			if cross(a, c, d) <= 1e-12 {
				continue
			}
			occupied := false
			for j, p := range points {
				if j != previous && j != i && j != next && cross(a, c, p) >= -1e-12 && cross(c, d, p) >= -1e-12 && cross(d, a, p) >= -1e-12 {
					occupied = true
					break
				}
			}
			if occupied {
				continue
			}
			result = append(result, [3]framePoint{a, c, d})
			points = append(points[:i], points[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			break // Validation rejects self-intersections; zero-area tails are harmless.
		}
	}
	return result
}
