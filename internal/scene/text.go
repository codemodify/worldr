package scene

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const atlasFontSize = 48

type glyph struct {
	u0, v0, u1, v1                           float32
	width, height, offsetX, offsetY, advance float32
}

func (c *Canvas) buildAtlas() error {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return fmt.Errorf("parse embedded Go font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: atlasFontSize, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return fmt.Errorf("create embedded Go font: %w", err)
	}
	c.face = face
	c.glyphs = make(map[rune]glyph)
	c.kern = make(map[uint64]float32)
	metrics := face.Metrics()
	c.ascent = float32(metrics.Ascent) / 64
	c.lineHeight = float32(metrics.Height) / 64
	const width, height = 2048, 2048
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	// Every solid primitive samples the center of this white patch.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}
	penX, penY, rowHeight := 6, 2, 0
	runes := make([]rune, 0, 850)
	for _, span := range [][2]rune{{32, 126}, {160, 383}, {880, 1023}, {8192, 8303}, {8592, 8703}, {8704, 8959}, {0xfffd, 0xfffd}} {
		for r := span[0]; r <= span[1]; r++ {
			runes = append(runes, r)
		}
	}
	for _, r := range runes {
		advance, ok := face.GlyphAdvance(r)
		if !ok {
			continue
		}
		dr, source, sourcePoint, _, ok := face.Glyph(fixed.Point26_6{}, r)
		if !ok {
			continue
		}
		gw, gh := dr.Dx(), dr.Dy()
		if penX+gw+2 > width {
			penX = 2
			penY += rowHeight + 4
			rowHeight = 0
		}
		if penY+gh+2 > height {
			face.Close()
			return fmt.Errorf("embedded font exceeds atlas capacity")
		}
		if gw > 0 && gh > 0 {
			draw.Draw(mask, image.Rect(penX, penY, penX+gw, penY+gh), source, sourcePoint, draw.Src)
		}
		c.glyphs[r] = glyph{u0: float32(penX), v0: float32(penY), u1: float32(penX + gw), v1: float32(penY + gh), width: float32(gw), height: float32(gh), offsetX: float32(dr.Min.X), offsetY: float32(dr.Min.Y), advance: float32(advance) / 64}
		penX += gw + 4
		if gh > rowHeight {
			rowHeight = gh
		}
	}
	usedHeight := 16
	for usedHeight < penY+rowHeight+2 {
		usedHeight *= 2
	}
	c.atlas = render.Atlas{Width: width, Height: usedHeight, Pixels: mask.Pix[:width*usedHeight]}
	c.whiteU, c.whiteV = 1.5/float32(width), 1.5/float32(usedHeight)
	for r, g := range c.glyphs {
		g.u0 /= width
		g.u1 /= width
		g.v0 /= float32(usedHeight)
		g.v1 /= float32(usedHeight)
		c.glyphs[r] = g
	}
	return nil
}

// Close releases the font's glyph cache. It does not own GPU atlas resources.
func (c *Canvas) Close() error {
	if c.face != nil {
		return c.face.Close()
	}
	return nil
}

func (c *Canvas) glyph(r rune) glyph {
	if g, ok := c.glyphs[r]; ok {
		return g
	}
	if g, ok := c.glyphs['\ufffd']; ok {
		return g
	}
	return c.glyphs['?']
}
func (c *Canvas) kerning(previous, current rune) float32 {
	if previous == 0 {
		return 0
	}
	key := uint64(previous)<<32 | uint64(current)
	if value, ok := c.kern[key]; ok {
		return value
	}
	value := float32(c.face.Kern(previous, current)) / 64
	c.kern[key] = value
	return value
}

// Text draws antialiased glyphs from a persistent embedded font atlas. x and y
// locate the top-left of the text line, and size is its font size in pixels.
func (c *Canvas) Text(x, y, size float32, text string, col Color) {
	if size <= 0 || col.A <= 0 || len(text) == 0 {
		return
	}
	scale := size / atlasFontSize
	penX, baseline := x, y+c.ascent*scale
	depth := float32(0)
	var previous rune
	for _, r := range text {
		if r == '\n' {
			penX = x
			baseline += c.lineHeight * scale
			previous = 0
			continue
		}
		if r == '\r' {
			continue
		}
		if r == '\t' {
			penX += c.glyph(' ').advance * scale * 4
			previous = 0
			continue
		}
		g := c.glyph(r)
		penX += c.kerning(previous, r) * scale
		if g.width > 0 && g.height > 0 {
			x0, y0 := penX+g.offsetX*scale, baseline+g.offsetY*scale
			c.quad(x0, y0, x0+g.width*scale, y0+g.height*scale, depth, g.u0, g.v0, g.u1, g.v1, col)
		}
		penX += g.advance * scale
		previous = r
	}
}

func (c *Canvas) MeasureText(size float32, text string) float32 {
	if size <= 0 {
		return 0
	}
	var width, maximum float32
	var previous rune
	for _, r := range text {
		if r == '\n' {
			if width > maximum {
				maximum = width
			}
			width = 0
			previous = 0
			continue
		}
		if r == '\r' {
			continue
		}
		if r == '\t' {
			width += c.glyph(' ').advance * 4
			previous = 0
			continue
		}
		width += c.kerning(previous, r) + c.glyph(r).advance
		previous = r
	}
	if width > maximum {
		maximum = width
	}
	return maximum * size / atlasFontSize
}
