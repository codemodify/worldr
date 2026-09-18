//go:build !linux || !cgo

package nativeui

import (
	"image"
	"image/color"
	"sort"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type monoLayout struct {
	text  string
	face  font.Face
	size  Metrics
	width int
}

func newTextLayout(text, _ string, size float64, width int) (textLayout, error) {
	parsed, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	metrics := face.Metrics()
	measured := font.MeasureString(face, text).Ceil()
	if width >= 0 {
		measured = min(measured, width)
	}
	return &monoLayout{text: text, face: face, width: width, size: Metrics{Width: measured, Height: metrics.Height.Ceil(), Baseline: metrics.Ascent.Ceil(), Glyphs: utf8.RuneCountInString(text), Runs: 1, Shaped: false}}, nil
}
func (p *monoLayout) metrics() Metrics { return p.size }
func (p *monoLayout) close()           { _ = p.face.Close() }
func (p *monoLayout) draw(dst *image.Alpha, origin image.Point) error {
	text := p.text
	if p.width >= 0 && font.MeasureString(p.face, text).Ceil() > p.width {
		runes := []rune(text)
		for len(runes) > 0 && font.MeasureString(p.face, string(runes)+"…").Ceil() > p.width {
			runes = runes[:len(runes)-1]
		}
		text = string(runes) + "…"
	}
	d := font.Drawer{Dst: dst, Src: image.NewUniform(color.White), Face: p.face, Dot: fixed.P(origin.X, origin.Y+p.size.Baseline)}
	d.DrawString(text)
	return nil
}
func (p *monoLayout) caret(index int) image.Point {
	return image.Pt(font.MeasureString(p.face, p.text[:index]).Ceil(), 0)
}
func (p *monoLayout) hit(point image.Point) int {
	stops := graphemeStops(p.text)
	best, distance := 0, int(^uint(0)>>1)
	for _, index := range stops {
		d := p.caret(index).X - point.X
		if d < 0 {
			d = -d
		}
		if d < distance {
			best, distance = index, d
		}
	}
	return best
}
func (p *monoLayout) ranges(first, last int) []image.Rectangle {
	return []image.Rectangle{image.Rect(p.caret(first).X, 0, p.caret(last).X, p.size.Height)}
}

// The headless/non-cgo fallback keeps common combining and ZWJ clusters intact.
// Full Unicode segmentation, shaping, bidi and font fallback use Pango above.
func graphemeStops(text string) []int {
	stops := []int{0}
	previous := rune(0)
	regional := 0
	for index, ch := range text {
		join := index == 0 || unicode.Is(unicode.Mn, ch) || unicode.Is(unicode.Mc, ch) || unicode.Is(unicode.Me, ch) || ch == 0x200d || previous == 0x200d || ch >= 0x1f3fb && ch <= 0x1f3ff
		if ch >= 0x1f1e6 && ch <= 0x1f1ff {
			join = join || regional%2 == 1
			regional++
		} else {
			regional = 0
		}
		if !join {
			stops = append(stops, index)
		}
		previous = ch
	}
	if len(text) > 0 {
		stops = append(stops, len(text))
	}
	return stops
}
func visualMove(text string, index, direction int) int {
	stops := graphemeStops(text)
	at := sort.SearchInts(stops, index)
	return stops[max(0, min(len(stops)-1, at+direction))]
}
