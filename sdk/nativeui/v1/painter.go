package nativeui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Alignment controls label placement within its strict clip rectangle.
type Alignment uint8

const (
	AlignStart Alignment = iota
	AlignCenter
	AlignEnd
)

// LabelStyle customizes one label. A zero Color uses the theme Text color.
type LabelStyle struct {
	Color color.RGBA
	Align Alignment
	Muted bool
}

// Painter renders controls into any draw.Image. It owns no GPU or protocol
// resources and is safe to recreate when an app changes theme.
type Painter struct {
	theme  Theme
	face   font.Face
	assets map[string]image.Image
}

// NewPainter validates theme before accepting it.
func NewPainter(theme Theme) (*Painter, error) {
	if err := theme.Validate(); err != nil {
		return nil, err
	}
	face, err := themeFace(theme)
	if err != nil {
		return nil, err
	}
	if theme.Skin != nil {
		owned := theme.Skin.Clone()
		theme.Skin = &owned
	}
	assets, err := themeAssets(theme)
	if err != nil {
		_ = face.Close()
		return nil, err
	}
	return &Painter{theme: theme, face: face, assets: assets}, nil
}

func themeAssets(theme Theme) (map[string]image.Image, error) {
	images := map[string]image.Image{}
	if theme.Skin != nil {
		for name, asset := range theme.Skin.Assets {
			if asset.Kind != "image" {
				continue
			}
			value, _, err := image.Decode(bytes.NewReader(asset.Data))
			if err != nil {
				return nil, fmt.Errorf("native UI: image asset %q: %w", name, err)
			}
			images[name] = value
		}
	}
	return images, nil
}

func themeFace(theme Theme) (font.Face, error) {
	data := goregular.TTF
	mono := strings.Contains(strings.ToLower(theme.Typography.Font), "mono")
	bold := theme.Typography.Weight >= 600
	if mono && bold {
		data = gomonobold.TTF
	} else if mono {
		data = gomono.TTF
	} else if bold {
		data = gobold.TTF
	}
	if theme.Skin != nil && theme.Skin.Typography.Asset != "" {
		data = theme.Skin.Assets[theme.Skin.Typography.Asset].Data
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(theme.Typography.Size), DPI: 72, Hinting: font.HintingFull})
}

// Close releases font resources. A closed painter may be reused with SetTheme.
func (p *Painter) Close() error {
	if p == nil || p.face == nil {
		return nil
	}
	err := p.face.Close()
	p.face = nil
	p.assets = nil
	return err
}

// Theme returns a copy of the current theme.
func (p *Painter) Theme() Theme {
	if p == nil {
		return Theme{}
	}
	t := p.theme
	if t.Skin != nil {
		owned := t.Skin.Clone()
		t.Skin = &owned
	}
	return t
}

// SetTheme changes future painting after complete validation.
func (p *Painter) SetTheme(theme Theme) error {
	if p == nil {
		return fmt.Errorf("native UI: nil painter")
	}
	if err := theme.Validate(); err != nil {
		return err
	}
	face, err := themeFace(theme)
	if err != nil {
		return err
	}
	assets, err := themeAssets(theme)
	if err != nil {
		_ = face.Close()
		return err
	}
	if theme.Skin != nil {
		owned := theme.Skin.Clone()
		theme.Skin = &owned
	}
	if p.face != nil {
		_ = p.face.Close()
	}
	p.theme, p.face, p.assets = theme, face, assets
	return nil
}

type clippedImage struct {
	draw.Image
	clip image.Rectangle
}

func (c clippedImage) Bounds() image.Rectangle { return c.clip.Intersect(c.Image.Bounds()) }
func (c clippedImage) Set(x, y int, value color.Color) {
	if image.Pt(x, y).In(c.Bounds()) {
		c.Image.Set(x, y, value)
	}
}

// clipToBounds keeps compound controls inside the rectangle supplied by the
// application. Individual primitives also clip themselves, but a control can
// contain several primitives whose ideal geometry is larger than a very narrow
// or short allocation (for example a slider track or radio mark).
func clipToBounds(dst draw.Image, bounds image.Rectangle) draw.Image {
	bounds = bounds.Intersect(dst.Bounds())
	if bounds == dst.Bounds() {
		return dst
	}
	// Preserve concrete framebuffer types so image/draw can use its optimized
	// glyph and compositing paths. A generic wrapper makes every glyph pixel go
	// through color interfaces and defeats those paths even on an RGBA image.
	switch target := dst.(type) {
	case clippedImage:
		return clipToBounds(target.Image, bounds)
	case *image.RGBA:
		return target.SubImage(bounds).(*image.RGBA)
	case *image.NRGBA:
		return target.SubImage(bounds).(*image.NRGBA)
	case *image.RGBA64:
		return target.SubImage(bounds).(*image.RGBA64)
	case *image.NRGBA64:
		return target.SubImage(bounds).(*image.NRGBA64)
	case *image.Alpha:
		return target.SubImage(bounds).(*image.Alpha)
	case *image.Alpha16:
		return target.SubImage(bounds).(*image.Alpha16)
	}
	return clippedImage{Image: dst, clip: bounds}
}

func (p *Painter) ready(dst draw.Image) error {
	if p == nil || p.face == nil {
		return fmt.Errorf("native UI: nil painter")
	}
	if dst == nil {
		return fmt.Errorf("native UI: nil drawing destination")
	}
	return nil
}

func labelColor(theme Theme, style LabelStyle) color.RGBA {
	if style.Color.A != 0 {
		return style.Color
	}
	if style.Muted {
		return theme.Palette.Muted
	}
	return theme.Palette.Text
}

func (p *Painter) textWidth(value string) int {
	return font.MeasureString(p.face, value).Ceil()
}

func (p *Painter) fitText(value string, width int) string {
	if width <= 0 || value == "" {
		return ""
	}
	if p.textWidth(value) <= width {
		return value
	}
	const ellipsis = "…"
	if p.textWidth(ellipsis) > width {
		return ""
	}
	runes := []rune(value)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if p.textWidth(string(runes[:mid])+ellipsis) <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(runes[:lo]) + ellipsis
}

// DrawLabel paints one UTF-8 line and ellipsizes it inside bounds.
func (p *Painter) DrawLabel(dst draw.Image, bounds image.Rectangle, value string, style LabelStyle) error {
	if err := p.ready(dst); err != nil {
		return err
	}
	if !utf8.ValidString(value) || len(value) > 65536 {
		return fmt.Errorf("native UI: label is invalid or exceeds 65536 bytes")
	}
	clip := bounds.Intersect(dst.Bounds())
	if clip.Empty() {
		return nil
	}
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\n", " "), "\r", " ")
	value = p.fitText(value, max(0, bounds.Dx()))
	if value == "" {
		return nil
	}
	width := p.textWidth(value)
	x := bounds.Min.X
	switch style.Align {
	case AlignCenter:
		x += (bounds.Dx() - width) / 2
	case AlignEnd:
		x = bounds.Max.X - width
	}
	metrics := p.face.Metrics()
	height := (metrics.Ascent + metrics.Descent).Ceil()
	baseline := bounds.Min.Y + (bounds.Dy()-height)/2 + metrics.Ascent.Ceil()
	drawer := font.Drawer{Dst: clipToBounds(dst, clip), Src: image.NewUniform(labelColor(p.theme, style)), Face: p.face, Dot: fixed.P(x, baseline)}
	drawer.DrawString(value)
	return nil
}

func (p *Painter) drawMultiline(dst draw.Image, bounds image.Rectangle, value string, style LabelStyle) error {
	if !utf8.ValidString(value) || len(value) > 65536 {
		return fmt.Errorf("native UI: text area content is invalid or exceeds 65536 bytes")
	}
	lineHeight := max(p.theme.Typography.LineHeight, p.face.Metrics().Height.Ceil())
	maxLines := min(256, max(0, bounds.Dy()/lineHeight))
	if maxLines == 0 {
		return nil
	}
	lines := wrapLines(p, value, bounds.Dx(), maxLines)
	for index, line := range lines {
		lineBounds := image.Rect(bounds.Min.X, bounds.Min.Y+index*lineHeight, bounds.Max.X, min(bounds.Max.Y, bounds.Min.Y+(index+1)*lineHeight))
		if err := p.DrawLabel(dst, lineBounds, line, style); err != nil {
			return err
		}
	}
	return nil
}

func wrapLines(p *Painter, value string, width, limit int) []string {
	if limit <= 0 || width <= 0 {
		return nil
	}
	paragraphs := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	lines := make([]string, 0, min(limit, len(paragraphs)))
	for _, paragraph := range paragraphs {
		if paragraph == "" {
			lines = append(lines, "")
			if len(lines) == limit {
				break
			}
			continue
		}
		remaining := []rune(paragraph)
		for len(remaining) > 0 && len(lines) < limit {
			lo, hi := 1, len(remaining)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if p.textWidth(string(remaining[:mid])) <= width {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			end := lo
			if end < len(remaining) {
				for i := end; i > 0; i-- {
					if remaining[i-1] == ' ' || remaining[i-1] == '\t' {
						end = i
						break
					}
				}
			}
			line := strings.TrimSpace(string(remaining[:end]))
			if line == "" && end > 0 {
				line = string(remaining[:end])
			}
			lines = append(lines, line)
			remaining = remaining[end:]
			for len(remaining) > 0 && (remaining[0] == ' ' || remaining[0] == '\t') {
				remaining = remaining[1:]
			}
		}
		if len(lines) == limit {
			break
		}
	}
	if len(lines) == limit && len(lines) > 0 {
		lines[len(lines)-1] = p.fitText(lines[len(lines)-1]+"…", width)
	}
	return lines
}
