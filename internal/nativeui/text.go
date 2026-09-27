package nativeui

import (
	"fmt"
	sdkui "github.com/codemodify/worldr/sdk/nativeui/v1"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode/utf8"
)

type Metrics struct {
	Width, Height, Baseline, Glyphs, Runs int
	Shaped                                bool
}
type textLayout interface {
	metrics() Metrics
	draw(*image.Alpha, image.Point) error
	caret(int) image.Point
	hit(image.Point) int
	ranges(int, int) []image.Rectangle
	close()
}
type layoutKey struct {
	text  string
	width int
	font  string
	size  float64
}
type Painter struct {
	skinPainter     *sdkui.Painter
	controlFont     string
	controlFontSize float64
	Theme           Theme
	cache           map[layoutKey]textLayout
	order           []layoutKey
	closed          bool
}

// GraphemeStops returns owned UTF-8 byte offsets at user-perceived character
// boundaries, including zero and the end of non-empty text. Native multiline
// editors use the same Pango segmentation as Field so cursor movement and
// deletion never split combining sequences or emoji ZWJ clusters.
func GraphemeStops(text string) []int {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return []int{0}
	}
	return append([]int(nil), graphemeStops(text)...)
}

// NewPainter uses Pango/HarfBuzz and system font fallback on Linux/cgo. Other
// builds retain the same control/editing APIs and use a limited Go mono face.
func NewPainter(theme Theme) (*Painter, error) {
	if theme.Font == "" {
		theme.Font = "Sans"
	}
	if theme.FontSize < 6 || theme.FontSize > 128 || math.IsNaN(theme.FontSize) || math.IsInf(theme.FontSize, 0) {
		return nil, fmt.Errorf("native UI font size must be within 6..128 pixels")
	}
	p := &Painter{Theme: theme, cache: make(map[layoutKey]textLayout)}
	if _, err := p.layout("", 1); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}
func (p *Painter) Close() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	if p.skinPainter != nil {
		_ = p.skinPainter.Close()
		p.skinPainter = nil
	}
	for _, layout := range p.cache {
		layout.close()
	}
	p.cache = nil
	p.order = nil
}
func validText(text string) string {
	text = strings.ToValidUTF8(text, "")
	if len(text) > 65536 {
		n := 65536
		for n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		text = text[:n]
	}
	return strings.ReplaceAll(text, "\x00", "")
}
func (p *Painter) layout(text string, width int) (textLayout, error) {
	if p.closed {
		return nil, fmt.Errorf("native UI painter is closed")
	}
	if width < -1 || width > 4096 {
		return nil, fmt.Errorf("native UI layout width must be -1 or within 0..4096 pixels")
	}
	if p.Theme.FontSize < 6 || p.Theme.FontSize > 128 || math.IsNaN(p.Theme.FontSize) || math.IsInf(p.Theme.FontSize, 0) || len(p.Theme.Font) > 256 {
		return nil, fmt.Errorf("invalid native UI font")
	}
	text = validText(text)
	key := layoutKey{text, width, p.Theme.Font, p.Theme.FontSize}
	if result := p.cache[key]; result != nil {
		return result, nil
	}
	result, err := newTextLayout(text, p.Theme.Font, p.Theme.FontSize, width)
	if err != nil {
		return nil, err
	}
	if len(p.order) == 128 {
		first := p.order[0]
		p.cache[first].close()
		delete(p.cache, first)
		p.order = p.order[1:]
	}
	p.order = append(p.order, key)
	p.cache[key] = result
	return result, nil
}
func (p *Painter) Measure(text string, width int) (Metrics, error) {
	layout, err := p.layout(text, width)
	if err != nil {
		return Metrics{}, err
	}
	return layout.metrics(), nil
}
func (p *Painter) paintText(dst *image.RGBA, rect image.Rectangle, layout textLayout, origin image.Point, c color.RGBA) error {
	rect = rect.Intersect(dst.Bounds())
	if rect.Empty() {
		return nil
	}
	if rect.Dx() > 4096 || rect.Dy() > 4096 {
		return fmt.Errorf("native UI text clip exceeds 4096 pixels")
	}
	mask := image.NewAlpha(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	if err := layout.draw(mask, origin.Sub(rect.Min)); err != nil {
		return err
	}
	draw.DrawMask(dst, rect, image.NewUniform(c), image.Point{}, mask, image.Point{}, draw.Over)
	return nil
}

// DrawLabel shapes one paragraph and ellipsizes at the available width. Rect
// provides both layout width and a strict paint clip; text is never rune-cut.
func (p *Painter) DrawLabel(dst *image.RGBA, rect image.Rectangle, text string, c color.RGBA) error {
	if dst == nil || rect.Empty() {
		return nil
	}
	layout, err := p.layout(text, rect.Dx())
	if err != nil {
		return err
	}
	metrics := layout.metrics()
	return p.paintText(dst, rect, layout, image.Pt(rect.Min.X, rect.Min.Y+(rect.Dy()-metrics.Height)/2), c)
}
func (p *Painter) DrawButton(dst *image.RGBA, node Node, focused, hovered bool) error {
	if dst == nil {
		return nil
	}
	rect := node.Bounds.Intersect(dst.Bounds())
	if rect.Empty() {
		return nil
	}
	restoreTypography := p.useControlTypography(rect)
	defer restoreTypography()
	background, border, foreground := p.Theme.Surface, p.Theme.Border, p.Theme.Text
	if hovered {
		background = p.Theme.Hover
	}
	if focused {
		border = p.Theme.Accent
	}
	if node.Disabled {
		foreground = p.Theme.Disabled
	}
	content := rect.Inset(max(1, p.Theme.Padding/2))
	if p.skinPainter != nil {
		kind := "button"
		if node.Role == RoleMenuItem {
			kind = "menu-item"
		}
		state := sdkui.State{Focused: focused, Hovered: hovered, Disabled: node.Disabled, Selected: node.Selected}
		if err := p.skinPainter.DrawControlBackground(dst, kind, rect, state); err != nil {
			return err
		}
		foreground = p.skinPainter.ControlTextColor(kind, state)
		content = p.controlContent(kind, rect)
	} else {
		cut := min(p.Theme.CornerCut, min(rect.Dx()/3, rect.Dy()/3))
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			inset := max(0, cut-min(y-rect.Min.Y, rect.Max.Y-y-1))
			draw.Draw(dst, image.Rect(rect.Min.X+inset, y, rect.Max.X-inset, y+1), image.NewUniform(background), image.Point{}, draw.Src)
		}
		draw.Draw(dst, image.Rect(rect.Min.X+cut, rect.Min.Y, rect.Max.X-cut, rect.Min.Y+1), image.NewUniform(border), image.Point{}, draw.Src)
		if focused {
			draw.Draw(dst, image.Rect(rect.Min.X+cut, rect.Max.Y-1, rect.Max.X-cut, rect.Max.Y), image.NewUniform(border), image.Point{}, draw.Src)
		}
	}
	return p.DrawLabel(dst, content, node.Label, foreground)
}
func (p *Painter) DrawField(dst *image.RGBA, rect image.Rectangle, field *Field, placeholder string, focused bool) error {
	if dst == nil || field == nil || rect.Empty() {
		return nil
	}
	restoreTypography := p.useControlTypography(rect)
	defer restoreTypography()
	if p.skinPainter != nil {
		if err := p.skinPainter.DrawControlBackground(dst, "field", rect, sdkui.State{Focused: focused}); err != nil {
			return err
		}
	} else {
		draw.Draw(dst, rect, image.NewUniform(p.Theme.Background), image.Point{}, draw.Src)
		border := p.Theme.Border
		if focused {
			border = p.Theme.Accent
		}
		draw.Draw(dst, image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y), image.NewUniform(border), image.Point{}, draw.Src)
	}
	inner := p.fieldContent(rect)
	if inner.Empty() {
		return nil
	}
	if field.text == "" && field.preedit == "" && !focused {
		return p.DrawLabel(dst, inner, placeholder, p.Theme.Muted)
	}
	visible := field.text
	caretIndex := field.caret
	if field.preedit != "" {
		visible = field.text[:field.caret] + field.preedit + field.text[field.caret:]
		if field.preeditBegin >= 0 {
			caretIndex += int(field.preeditBegin)
		}
	}
	layout, err := p.layout(visible, -1)
	if err != nil {
		return err
	}
	metrics := layout.metrics()
	caret := layout.caret(caretIndex)
	if caret.X-field.scroll >= inner.Dx()-1 {
		field.scroll = max(0, caret.X-inner.Dx()+2)
	}
	if caret.X < field.scroll {
		field.scroll = max(0, caret.X)
	}
	origin := image.Pt(inner.Min.X-field.scroll, inner.Min.Y+(inner.Dy()-metrics.Height)/2)
	lo, hi := field.Range()
	if field.preedit != "" && field.preeditBegin >= 0 {
		lo = field.caret + int(min(field.preeditBegin, field.preeditEnd))
		hi = field.caret + int(max(field.preeditBegin, field.preeditEnd))
	}
	if lo != hi {
		for _, selection := range layout.ranges(lo, hi) {
			selection = selection.Add(origin).Intersect(inner)
			draw.Draw(dst, selection, image.NewUniform(p.Theme.Selection), image.Point{}, draw.Src)
		}
	}
	if err := p.paintText(dst, inner, layout, origin, p.fieldTextColor(focused)); err != nil {
		return err
	}
	if field.preedit != "" {
		for _, span := range layout.ranges(field.caret, field.caret+len(field.preedit)) {
			span = span.Add(origin)
			underline := image.Rect(span.Min.X, span.Max.Y-2, span.Max.X, span.Max.Y-1).Intersect(inner)
			draw.Draw(dst, underline, image.NewUniform(p.Theme.Accent), image.Point{}, draw.Src)
		}
	}
	if focused && !(field.preeditBegin == -1 && field.preeditEnd == -1) {
		x := origin.X + caret.X
		cursor := image.Rect(x, inner.Min.Y, x+1, inner.Max.Y).Intersect(inner)
		draw.Draw(dst, cursor, image.NewUniform(p.Theme.Accent), image.Point{}, draw.Src)
	}
	return nil
}
func (p *Painter) PlaceCaret(field *Field, rect image.Rectangle, point image.Point, extend bool) error {
	if field == nil {
		return nil
	}
	restoreTypography := p.useControlTypography(rect)
	defer restoreTypography()
	layout, err := p.layout(field.text, -1)
	if err != nil {
		return err
	}
	inner := p.fieldContent(rect)
	origin := image.Pt(inner.Min.X-field.scroll, inner.Min.Y+(inner.Dy()-layout.metrics().Height)/2)
	field.SetCaret(layout.hit(point.Sub(origin)), extend)
	return nil
}

func (p *Painter) fieldContent(rect image.Rectangle) image.Rectangle {
	if p.skinPainter != nil {
		return p.controlContent("field", rect)
	}
	return rect.Inset(max(1, p.Theme.Padding/2))
}
func (p *Painter) fieldTextColor(focused bool) color.RGBA {
	if p.skinPainter != nil {
		return p.skinPainter.ControlTextColor("field", sdkui.State{Focused: focused})
	}
	return p.Theme.Text
}

// Fixed-layout native apps may allocate a shorter toolbar than a skin's sample
// controls. Retain its horizontal padding while keeping one shaped line legible.
func (p *Painter) controlContent(kind string, rect image.Rectangle) image.Rectangle {
	content := p.skinPainter.ControlContentBounds(kind, rect)
	minimum := min(rect.Dy(), int(math.Ceil(p.Theme.FontSize))+2)
	if content.Dy() < minimum {
		content.Min.Y = rect.Min.Y + (rect.Dy()-minimum)/2
		content.Max.Y = content.Min.Y + minimum
	}
	return content.Intersect(rect)
}

// Skin typography belongs to controls. Application document text can retain a
// fixed grid, while every control still uses Pango/font fallback and IME shaping.
func (p *Painter) useControlTypography(bounds image.Rectangle) func() {
	oldFont, oldSize := p.Theme.Font, p.Theme.FontSize
	if p.skinPainter != nil {
		p.Theme.Font = p.controlFont
		p.Theme.FontSize = max(6, min(p.controlFontSize, float64(max(6, bounds.Dy()-2))))
	}
	return func() { p.Theme.Font, p.Theme.FontSize = oldFont, oldSize }
}
