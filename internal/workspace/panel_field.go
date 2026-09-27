package workspace

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// panelField is retained scenery behind the working windows. Every sheet is
// world geometry viewed through the workspace camera, never a screen wallpaper
// or an input target. Static drawings upload once; camera movement costs no
// texture uploads, text shaping, mesh rebuilding, or background simulation.
type panelField struct {
	scene                            *scene.Scene
	palette                          [4]skin.Color
	retiredTextures, retiredGeometry []uint64
}

var panelFieldTokens = [...]string{"field-panel", "field-slate", "field-line", "field-light"}

func panelFieldPalette(selected *skin.Skin) [4]skin.Color {
	values := [4]skin.Color{"#16234448", "#36414E5C", "#839F8748", "#C7E5FF"}
	for i, token := range panelFieldTokens {
		if value, ok := selected.Palette[token]; ok {
			values[i] = value
		}
	}
	return values
}

func panelFieldColor(selected *skin.Skin, token string) color.NRGBA {
	values := panelFieldPalette(selected)
	for i, name := range panelFieldTokens {
		if name == token {
			return selected.Color(string(values[i]))
		}
	}
	return selected.Color(token)
}

func (w *Workspace) panelFieldVisible() bool {
	return w.desktop && w.activeSkin != nil && w.activeSkin.Desktop.Backdrop == "panel-field"
}

func (w *Workspace) drawPanelField() {
	w.drawQuietSkinBackdrop()
	key := panelFieldPalette(w.activeSkin)
	if w.panelField == nil || w.panelField.palette != key {
		field, err := newPanelField(w.activeSkin, key)
		if err != nil {
			return // Keep the quiet background if a font or texture cannot load.
		}
		if old := w.panelField; old != nil {
			field.retiredTextures, field.retiredGeometry = old.retiredTextures, old.retiredGeometry
			seen := make(map[uint64]bool)
			for _, id := range old.scene.Children(0) {
				n := old.scene.Node(id)
				if n.Surface != nil && !seen[n.Surface.ID()] {
					seen[n.Surface.ID()] = true
					field.retiredTextures = append(field.retiredTextures, n.Surface.ID())
				}
				if n.Mesh != nil {
					field.retiredGeometry = append(field.retiredGeometry, n.Mesh.Geometry().ID())
				}
			}
		}
		w.panelField = field
	}
	// A broad source behind the sheets gives their alpha something to reveal.
	// Project its position and radius through the same camera, so the light
	// participates in parallax and gets smaller when the viewer walks away.
	right, up, normal := applicationBasis()
	lampCenter := right.Mul(-5.8).Add(up.Mul(4.8)).Add(normal.Mul(-17))
	x, y, depth, _ := w.camera.Project(lampCenter, w.viewport)
	if depth > 0 && depth < 1 && finite(float64(x)) && finite(float64(y)) {
		x2, y2, _, _ := w.camera.Project(lampCenter.Add(right.Mul(4)), w.viewport)
		radius := float32(math.Hypot(float64(x2-x), float64(y2-y)))
		light := panelFieldColor(w.activeSkin, "field-light")
		glow := scene.Color{R: float32(light.R) / 255, G: float32(light.G) / 255, B: float32(light.B) / 255, A: .22}
		w.canvas.RadialGradient(x, y, radius, glow, glow.WithAlpha(0))
		w.canvas.RadialGradient(x, y, radius*.32, glow.WithAlpha(.35), glow.WithAlpha(0))
	}
	w.panelField.scene.Draw(w.canvas, w.camera, w.viewport)
}

func newPanelField(selected *skin.Skin, key [4]skin.Color) (*panelField, error) {
	p := &panelField{scene: scene.NewScene(), palette: key}
	// A bounded background pass. Four overlapping sheets suffice for the
	// sparse field; working windows have their own complete transparency pass.
	p.scene.TransparencyLayers = 4
	theme := nativeui.Cinematic()
	theme.Font, theme.FontSize = "Monospace", 10
	painter, err := nativeui.NewPainter(theme)
	if err != nil {
		return nil, err
	}
	defer painter.Close()
	textures := make([]*render.Texture, 8)
	for i := range textures {
		textures[i], err = panelFieldTexture(painter, selected, i)
		if err != nil {
			return nil, err
		}
	}
	// Large peripheral sheets and staggered distant groups leave a clear
	// central work area. All scenery stays behind the initial app placements.
	placements := [][6]float32{
		{-7.7, 1.4, -5, 4.0, 11.8, -.08}, {8.4, -.5, -6, 4.8, 11.4, .1},
		{-5.4, 2.6, -9, 3.6, 6.8, .04}, {5.5, 3.2, -10, 4.2, 6.2, -.06},
		{-2.1, 4.4, -12, 3.0, 5.8, -.06}, {1.8, -3.9, -11, 4.6, 5.4, .04},
		{-7.4, -3.6, -13, 4.3, 5.2, .08}, {9.3, 4.8, -16, 3.5, 7.8, -.04},
		{-11, 3.7, -20, 3.6, 7.1, -.08}, {-5.0, 1.2, -18, 2.7, 5.4, .07},
		{.3, 1.5, -19, 3.4, 6.9, -.06}, {5.4, -1.8, -20, 3.4, 7.0, .06},
		{-4.4, -6, -22, 3.8, 5.4, -.04}, {10.8, -5.6, -23, 5, 6.2, .04},
		{-1, 7.4, -25, 4.4, 6.5, .02}, {4, 5.8, -27, 3.4, 5.9, -.03},
		{-9.2, -.6, -28, 3.2, 7.8, .04}, {1.6, -5.1, -29, 3.6, 6.7, -.04},
		{-4, 3.1, -32, 3.4, 6.6, .02}, {6.4, 1.2, -34, 4.1, 7.1, -.03},
	}
	right, up, normal := applicationBasis()
	for i, v := range placements {
		center := right.Mul(v[0]).Add(up.Mul(v[1])).Add(normal.Mul(v[2]))
		p.scene.Add(0, scene.Node{
			Transform: roomPlacement(center).Mul(scene.RotateY(v[5])).Mul(scene.Scale(v[3], v[4], 1)),
			Surface:   textures[i%len(textures)], Translucent: true, Unlit: true, Unpickable: true,
			Color: scene.Color{R: 1, G: 1, B: 1, A: 1},
		})
	}
	// The blue-white source belongs to the scene: sheets cross and attenuate
	// it during camera travel, unlike a lens flare painted over client text.
	vertices := []scene.Vec3{{}}
	indices := make([]uint32, 0, 48)
	for i := 0; i < 16; i++ {
		a := float64(i) * math.Pi / 8
		vertices = append(vertices, scene.Vec3{X: float32(math.Cos(a)), Y: float32(math.Sin(a))})
		indices = append(indices, 0, uint32(i+1), uint32((i+1)%16+1))
	}
	mesh, err := scene.NewMesh(vertices, indices, nil)
	if err != nil {
		return nil, err
	}
	lamp := panelFieldColor(selected, "field-light")
	center := right.Mul(-5.8).Add(up.Mul(4.8)).Add(normal.Mul(-17))
	p.scene.Add(0, scene.Node{Mesh: mesh, Transform: roomPlacement(center).Mul(scene.Scale(.11, .11, 1)),
		Color: scene.Color{R: float32(lamp.R) / 255, G: float32(lamp.G) / 255, B: float32(lamp.B) / 255, A: 1},
		Unlit: true, Unpickable: true, Glow: [3]float32{float32(lamp.R) / 255 * .7, float32(lamp.G) / 255 * .83, float32(lamp.B) / 255}})
	return p, nil
}

func panelFieldTexture(painter *nativeui.Painter, selected *skin.Skin, variant int) (*render.Texture, error) {
	const width, height = 256, 448
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	colorFor := func(token string) color.RGBA {
		c := panelFieldColor(selected, token)
		r, g, b, a := c.RGBA()
		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	}
	base := colorFor("field-panel")
	if variant%3 == 0 {
		base = colorFor("field-slate")
	}
	draw.Draw(pixels, pixels.Bounds(), image.NewUniform(base), image.Point{}, draw.Src)
	ink := colorFor("field-line")
	line := func(x0, y0, x1, y1 int) {
		steps := max(x1-x0, x0-x1, y1-y0, y0-y1)
		for i := 0; i <= steps; i++ {
			x, y := x0, y0
			if steps > 0 {
				x += (x1 - x0) * i / steps
				y += (y1 - y0) * i / steps
			}
			draw.Draw(pixels, image.Rect(x, y, x+1, y+1), image.NewUniform(ink), image.Point{}, draw.Over)
		}
	}
	line(0, 0, width-1, 0)
	line(0, 0, 0, height-1)
	line(width-1, 0, width-1, height-1)
	line(0, height-1, width-1, height-1)
	line(14, 39, width-14, 39)
	label := func(y int, text string) error {
		return painter.DrawLabel(pixels, image.Rect(16, y, width-12, y+16), text, ink)
	}
	if err := label(15, fmt.Sprintf("SPATIAL REFERENCE / %02d", variant+1)); err != nil {
		return nil, err
	}
	// Authored coordinate/schematic plates, not fabricated live telemetry.
	// A handful of shared variants supplies the quiet density in the reference.
	if variant%2 == 0 {
		for row := 0; row < 13; row++ {
			if err := label(55+row*16, fmt.Sprintf("%02d   %+05.1f   %+05.1f", row, float64(row-6)*.25, math.Sin(float64(row+variant)))); err != nil {
				return nil, err
			}
		}
	}
	graphTop := 65
	if variant%2 == 0 {
		graphTop = 292
	}
	for x := 24; x < width-16; x += 26 {
		line(x, graphTop, x, height-36)
	}
	for y := graphTop; y < height-32; y += 26 {
		line(24, y, width-16, y)
	}
	for curve := 0; curve < 3; curve++ {
		var lastX, lastY int
		for step := 0; step <= 120; step++ {
			a := float64(step)*math.Pi/60 + float64(variant)*.3
			x := 128 + int(math.Cos(a)*float64(83-curve*16))
			y := (graphTop+height-36)/2 + int(math.Sin(a*float64(curve+1))*float64((height-36-graphTop)/2-12))
			if step != 0 {
				line(lastX, lastY, x, y)
			}
			lastX, lastY = x, y
		}
	}
	if err := label(height-25, "X / Y / Z     WORLD SPACE"); err != nil {
		return nil, err
	}
	return render.NewTexture(width, height, pixels.Pix)
}
