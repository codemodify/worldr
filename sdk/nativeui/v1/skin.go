package nativeui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// ThemeFromSkin owns a copy of the complete skin, including component recipes.
func ThemeFromSkin(s skin.Skin) (Theme, error) {
	if err := s.Validate(); err != nil {
		return Theme{}, err
	}
	owned := s.Clone()
	m := s.Metrics
	c := func(name string) color.RGBA { return color.RGBAModel.Convert(s.Color(name)).(color.RGBA) }
	shape := Slab
	if r, ok := s.Controls["button"]; ok && len(r.Layers) > 0 {
		switch r.Layers[0].Geometry.Kind {
		case "rounded":
			shape = Rounded
		case "chamfered":
			shape = Chamfered
		case "bracketed":
			shape = Bracketed
		case "notched":
			shape = Notched
		}
	}
	t := Theme{Family: Family(s.ID), Shape: shape, Skin: &owned, Palette: Palette{Background: c("background"), Surface: c("surface"), Raised: c("raised"), Hover: c("hover"), Pressed: c("pressed"), Accent: c("accent"), AccentAlt: c("accent-alt"), Border: c("border"), Text: c("text"), Muted: c("muted"), Selection: c("selection"), Disabled: c("disabled"), Success: c("success"), Warning: c("warning"), Danger: c("danger")}, Metrics: Metrics{Padding: int(m.Padding), Gap: int(m.Gap), ControlHeight: int(m.ControlHeight), Stroke: int(m.Stroke), Corner: int(m.Corner), Notch: int(m.Notch), Icon: int(m.Icon)}, Typography: Typography{Font: s.Typography.Family, Size: int(s.Typography.Size), LineHeight: int(s.Typography.LineHeight), Weight: s.Typography.Weight}}
	return t, t.Validate()
}

func controlKindName(kind Kind) string {
	names := []string{"label", "icon", "button", "icon-button", "field", "text-area", "switch", "checkbox", "radio", "slider", "progress", "meter", "tab", "segment", "menu-item", "list-row", "tree-row", "table-row", "scrollbar", "splitter", "panel", "card", "toolbar", "dialog", "popover", "tooltip", "badge", "separator"}
	if int(kind) >= len(names) {
		return "button"
	}
	return names[kind]
}

func (p *Painter) recipe(kind string) (skin.Recipe, bool) {
	if p == nil || p.theme.Skin == nil {
		return skin.Recipe{}, false
	}
	r, ok := p.theme.Skin.Controls[kind]
	if !ok {
		r, ok = p.theme.Skin.Controls["button"]
	}
	return r, ok
}

// drawPart intentionally uses an exact lookup. Optional subparts must not fall
// back to a button recipe when a custom package supplies only main components.
func (p *Painter) drawPart(dst draw.Image, kind string, bounds image.Rectangle, state State) (bool, error) {
	if p == nil || p.theme.Skin == nil {
		return false, nil
	}
	r, ok := p.theme.Skin.Controls[kind]
	if !ok {
		return false, nil
	}
	if err := p.DrawControlBackground(dst, kind, bounds, state); err != nil {
		return true, err
	}
	if r.Icon != "" {
		return true, p.DrawNamedIcon(dst, bounds, r.Icon, p.ControlTextColor(kind, state))
	}
	return true, nil
}
func recipeState(r skin.Recipe, state State) skin.StateStyle {
	var result skin.StateStyle
	for _, v := range []struct {
		name string
		on   bool
	}{{"hovered", state.Hovered}, {"pressed", state.Pressed}, {"selected", state.Selected}, {"focused", state.Focused}, {"invalid", state.Invalid}, {"disabled", state.Disabled}} {
		if !v.on {
			continue
		}
		s := r.States[v.name]
		if s.Fill != "" {
			result.Fill = s.Fill
		}
		if s.Stroke != "" {
			result.Stroke = s.Stroke
		}
		if s.Text != "" {
			result.Text = s.Text
		}
		if s.Opacity != 0 {
			result.Opacity = s.Opacity
		}
		if s.Glow != 0 {
			result.Glow = s.Glow
		}
		if s.Offset != (skin.Point{}) {
			result.Offset = s.Offset
		}
	}
	return result
}

// ControlTextColor and ControlContentBounds let alternate text backends share
// the skin's control layout and state colors while retaining their own shaping.
func (p *Painter) ControlTextColor(kind string, state State) color.RGBA {
	if r, ok := p.recipe(kind); ok {
		token := r.TextColor
		if token == "" {
			token = "text"
		}
		if s := recipeState(r, state); s.Text != "" {
			token = s.Text
		}
		return color.RGBAModel.Convert(p.theme.Skin.Color(token)).(color.RGBA)
	}
	_, _, ink := p.surfaceColors(state)
	return ink
}
func (p *Painter) ControlContentBounds(kind string, bounds image.Rectangle) image.Rectangle {
	if r, ok := p.recipe(kind); ok {
		i := r.ContentInsets
		return image.Rect(bounds.Min.X+int(i.Left), bounds.Min.Y+int(i.Top), max(bounds.Min.X+int(i.Left), bounds.Max.X-int(i.Right)), max(bounds.Min.Y+int(i.Top), bounds.Max.Y-int(i.Bottom))).Intersect(bounds)
	}
	return bounds.Inset(max(1, p.theme.Metrics.Padding/2))
}

// DrawControlBackground draws a component recipe without its text or behavior.
// It supports paths, alpha, gradients and layered edges. Glass refraction and
// blur remain scene-renderer capabilities and are represented by alpha here.
func (p *Painter) DrawControlBackground(dst draw.Image, kind string, bounds image.Rectangle, state State) error {
	if err := p.ready(dst); err != nil {
		return err
	}
	if bounds.Empty() {
		return nil
	}
	r, ok := p.recipe(kind)
	if !ok {
		// Partial skin packages may omit a component and a generic button. Use
		// the semantic fallback directly, without re-entering skin dispatch.
		background, border, _ := p.surfaceColors(state)
		if err := FillShape(dst, bounds, p.Shape(), background); err != nil {
			return err
		}
		return StrokeShape(dst, bounds, p.Shape(), border)
	}
	dst = clipToBounds(dst, bounds)
	style := recipeState(r, state)
	for i, layer := range r.Layers {
		box := bounds
		if b := layer.Bounds; b != (skin.Box{}) {
			box = image.Rect(bounds.Min.X+int(math.Round(b.X*float64(bounds.Dx()))), bounds.Min.Y+int(math.Round(b.Y*float64(bounds.Dy()))), bounds.Min.X+int(math.Round((b.X+b.W)*float64(bounds.Dx()))), bounds.Min.Y+int(math.Round((b.Y+b.H)*float64(bounds.Dy()))))
		}
		box = box.Add(image.Pt(int(style.Offset.X), int(style.Offset.Y)))
		fill, stroke := layer.Fill, layer.Stroke
		if i == 0 {
			if style.Fill != "" {
				fill = style.Fill
			}
			if style.Stroke != "" {
				stroke = style.Stroke
			}
		}
		opacity := layer.Opacity
		if opacity == 0 {
			opacity = 1
		}
		if style.Opacity != 0 {
			opacity *= style.Opacity
		}
		material := p.theme.Skin.Materials[layer.Material]
		if material.Opacity != 0 {
			opacity *= material.Opacity
		}
		points := recipePoints(box, layer.Geometry)
		if fill != "" {
			a := p.theme.Skin.Color(fill)
			b := a
			if material.Secondary != "" {
				b = p.theme.Skin.Color(material.Secondary)
			}
			gradient := material.Kind == "linear-gradient"
			fillRecipePolygon(dst, box, points, a, b, material.Angle, opacity, gradient)
		}
		if layer.Asset != "" {
			if texture := p.assets[layer.Asset]; texture != nil {
				fillRecipePolygon(dst, box, points, color.NRGBA{}, color.NRGBA{}, 0, opacity, false, texture)
			}
		}
		if stroke != "" {
			ink := p.theme.Skin.Color(stroke)
			ink.A = uint8(float64(ink.A) * opacity)
			width := max(1, int(math.Round(layer.StrokeWidth)))
			strokeBounds := box.Inset(width / 2)
			if strokeBounds.Dx() > 1 {
				strokeBounds.Max.X--
			}
			if strokeBounds.Dy() > 1 {
				strokeBounds.Max.Y--
			}
			points = recipePoints(strokeBounds, layer.Geometry)
			glow := material.Glow + style.Glow
			if glow > 0 {
				halo := ink
				halo.A = uint8(min(60, float64(halo.A)*glow*.12))
				strokeRecipePolygon(dst, points, width+4, halo, layer.Geometry.Kind == "bracketed")
			}
			strokeRecipePolygon(dst, points, width, ink, layer.Geometry.Kind == "bracketed")
		}
	}
	return nil
}

func recipePoints(bounds image.Rectangle, g skin.Geometry) []image.Point {
	if bounds.Empty() {
		return nil
	}
	if g.Kind == "path" {
		points := make([]image.Point, len(g.Points))
		for i, v := range g.Points {
			points[i] = image.Pt(bounds.Min.X+int(math.Round(v.X*float64(max(0, bounds.Dx()-1)))), bounds.Min.Y+int(math.Round(v.Y*float64(max(0, bounds.Dy()-1)))))
		}
		return points
	}
	span := float64(min(bounds.Dx(), bounds.Dy()))
	spec := ShapeSpec{Grammar: ShapeGrammar(g.Kind), Corner: int(math.Round(g.Corner * span)), Notch: int(math.Round(g.Notch * span)), Stroke: 1}
	if g.Kind == "rect" {
		spec.Grammar = Slab
	}
	if g.Kind == "rounded" {
		spec.Corner = int(math.Round(g.Radius * span))
	}
	return shapePoints(bounds, spec)
}
func strokeRecipePolygon(dst draw.Image, points []image.Point, width int, ink color.Color, brackets bool) {
	if len(points) < 2 {
		return
	}
	for i, a := range points {
		b := points[(i+1)%len(points)]
		if brackets && absInt(a.X-b.X)+absInt(a.Y-b.Y) > 12 {
			dx, dy := b.X-a.X, b.Y-a.Y
			strokeSoftSegment(dst, a, image.Pt(a.X+dx/4, a.Y+dy/4), float64(width), ink)
			strokeSoftSegment(dst, image.Pt(b.X-dx/4, b.Y-dy/4), b, float64(width), ink)
		} else {
			strokeSoftSegment(dst, a, b, float64(width), ink)
		}
	}
}
func strokeSoftSegment(dst draw.Image, a, b image.Point, width float64, ink color.Color) {
	radius := width / 2
	pad := int(math.Ceil(radius + .5))
	clip := image.Rect(min(a.X, b.X)-pad, min(a.Y, b.Y)-pad, max(a.X, b.X)+pad+1, max(a.Y, b.Y)+pad+1).Intersect(dst.Bounds())
	dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
	length := dx*dx + dy*dy
	source := color.NRGBAModel.Convert(ink).(color.NRGBA)
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		for x := clip.Min.X; x < clip.Max.X; x++ {
			px, py := float64(x-a.X), float64(y-a.Y)
			t := 0.0
			if length > 0 {
				t = min(1, max(0, (px*dx+py*dy)/length))
			}
			coverage := min(1, max(0, radius+.5-math.Hypot(px-t*dx, py-t*dy)))
			if coverage == 0 {
				continue
			}
			c := source
			c.A = uint8(float64(c.A) * coverage)
			blendPixel(dst, x, y, c)
		}
	}
}
func fillRecipePolygon(dst draw.Image, bounds image.Rectangle, points []image.Point, a, b color.NRGBA, angle, opacity float64, gradient bool, texture ...image.Image) {
	if len(points) < 3 {
		return
	}
	clip := bounds.Intersect(dst.Bounds())
	if clip.Empty() {
		return
	}
	intersections := make([]float64, 0, len(points))
	radians := angle * math.Pi / 180
	dx, dy := math.Cos(radians), math.Sin(radians)
	denom := math.Abs(dx) + math.Abs(dy)
	coverage := make([]float64, clip.Dx())
	for y := clip.Min.Y; y < clip.Max.Y; y++ {
		clear(coverage)
		for sample := 0; sample < 4; sample++ {
			intersections = intersections[:0]
			scan := float64(y) + (float64(sample)+.5)/4
			for i, u := range points {
				v := points[(i+1)%len(points)]
				if u.Y == v.Y || scan < float64(min(u.Y, v.Y)) || scan >= float64(max(u.Y, v.Y)) {
					continue
				}
				intersections = append(intersections, float64(u.X)+(scan-float64(u.Y))*float64(v.X-u.X)/float64(v.Y-u.Y))
			}
			sort.Float64s(intersections)
			for i := 0; i+1 < len(intersections); i += 2 {
				left := max(clip.Min.X, int(math.Floor(intersections[i])))
				right := min(clip.Max.X, int(math.Ceil(intersections[i+1])))
				for x := left; x < right; x++ {
					coverage[x-clip.Min.X] += max(0, min(float64(x+1), intersections[i+1])-max(float64(x), intersections[i])) / 4
				}
			}
		}
		for x := clip.Min.X; x < clip.Max.X; x++ {
			alpha := opacity * coverage[x-clip.Min.X]
			if alpha == 0 {
				continue
			}
			if !gradient && len(texture) == 0 {
				ink := a
				ink.A = uint8(float64(ink.A) * alpha)
				blendPixel(dst, x, y, ink)
				continue
			}
			if len(texture) > 0 {
				source := texture[0]
				r := source.Bounds()
				sx := r.Min.X + (x-bounds.Min.X)*r.Dx()/max(1, bounds.Dx())
				sy := r.Min.Y + (y-bounds.Min.Y)*r.Dy()/max(1, bounds.Dy())
				ink := color.NRGBAModel.Convert(source.At(sx, sy)).(color.NRGBA)
				ink.A = uint8(float64(ink.A) * alpha)
				blendPixel(dst, x, y, ink)
				continue
			}
			fx := float64(x-bounds.Min.X)/float64(max(1, bounds.Dx()-1)) - .5
			fy := float64(y-bounds.Min.Y)/float64(max(1, bounds.Dy()-1)) - .5
			t := min(1, max(0, (fx*dx+fy*dy)/denom+.5))
			mix := func(u, v uint8) uint8 { return uint8(float64(u)*(1-t) + float64(v)*t) }
			ink := color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), uint8(float64(mix(a.A, b.A)) * alpha)}
			blendPixel(dst, x, y, ink)
		}
	}
}
func blendPixel(dst draw.Image, x, y int, c color.NRGBA) {
	if target, ok := dst.(*image.RGBA); ok {
		if !image.Pt(x, y).In(target.Rect) || c.A == 0 {
			return
		}
		i := target.PixOffset(x, y)
		pixel := target.Pix[i : i+4 : i+4]
		if c.A == 255 {
			pixel[0], pixel[1], pixel[2], pixel[3] = c.R, c.G, c.B, 255
			return
		}
		// Match the generic RGBA64 intermediate exactly, including integer
		// rounding. Direct byte access removes three escaping color values per
		// pixel without changing antialiasing or translucent composition.
		r, g, b, a := c.RGBA()
		inv := uint32(65535) - a
		pixel[0] = uint8((r + uint32(pixel[0])*257*inv/65535) >> 8)
		pixel[1] = uint8((g + uint32(pixel[1])*257*inv/65535) >> 8)
		pixel[2] = uint8((b + uint32(pixel[2])*257*inv/65535) >> 8)
		pixel[3] = uint8((a + uint32(pixel[3])*257*inv/65535) >> 8)
		return
	}
	r, g, b, a := c.RGBA()
	dr, dg, db, da := dst.At(x, y).RGBA()
	inv := uint32(65535) - a
	dst.Set(x, y, color.RGBA64{R: uint16(r + dr*inv/65535), G: uint16(g + dg*inv/65535), B: uint16(b + db*inv/65535), A: uint16(a + da*inv/65535)})
}

// DrawNamedIcon renders paths supplied by the selected skin.
func (p *Painter) DrawNamedIcon(dst draw.Image, bounds image.Rectangle, name string, ink color.RGBA) error {
	if err := p.ready(dst); err != nil {
		return err
	}
	if p.theme.Skin == nil {
		return fmt.Errorf("native UI: no skin icon set")
	}
	icon, ok := p.theme.Skin.Icons[name]
	if !ok {
		return fmt.Errorf("native UI: unknown skin icon %q", name)
	}
	dst = clipToBounds(dst, bounds)
	if ink.A == 0 {
		ink = p.theme.Palette.Text
	}
	for _, path := range icon.Paths {
		points := make([]image.Point, len(path.Points))
		for i, v := range path.Points {
			points[i] = image.Pt(bounds.Min.X+int(math.Round(v.X*float64(bounds.Dx()))), bounds.Min.Y+int(math.Round(v.Y*float64(bounds.Dy()))))
		}
		if path.Fill {
			c := color.NRGBAModel.Convert(ink).(color.NRGBA)
			fillRecipePolygon(dst, bounds, points, c, c, 0, 1, false)
		}
		for i := 1; i < len(points); i++ {
			strokeSoftSegment(dst, points[i-1], points[i], max(1, icon.StrokeWidth), ink)
		}
		if path.Closed && len(points) > 1 {
			strokeSoftSegment(dst, points[len(points)-1], points[0], max(1, icon.StrokeWidth), ink)
		}
	}
	return nil
}
