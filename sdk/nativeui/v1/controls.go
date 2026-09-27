package nativeui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

func (p *Painter) surfaceColors(state State) (color.RGBA, color.RGBA, color.RGBA) {
	background, border, foreground := p.theme.Palette.Surface, p.theme.Palette.Border, p.theme.Palette.Text
	switch {
	case state.Disabled:
		foreground, border = p.theme.Palette.Disabled, p.theme.Palette.Disabled
	case state.Pressed:
		background = p.theme.Palette.Pressed
	case state.Hovered:
		background = p.theme.Palette.Hover
	}
	if state.Selected {
		background, border = p.theme.Palette.Selection, p.theme.Palette.Accent
	}
	if state.Focused {
		border = p.theme.Palette.Accent
	}
	if state.Invalid {
		border = p.theme.Palette.Danger
	}
	return background, border, foreground
}

func (p *Painter) drawSurface(dst draw.Image, control Control, raised bool) (color.RGBA, error) {
	if err := p.ready(dst); err != nil {
		return color.RGBA{}, err
	}
	if err := control.validate(false); err != nil {
		return color.RGBA{}, err
	}
	if p.theme.Skin != nil {
		kind := controlKindName(control.Kind)
		if err := p.DrawControlBackground(dst, kind, control.Bounds, control.State); err != nil {
			return color.RGBA{}, err
		}
		return p.ControlTextColor(kind, control.State), nil
	}
	background, border, foreground := p.surfaceColors(control.State)
	if raised && !control.State.Hovered && !control.State.Pressed && !control.State.Selected {
		background = p.theme.Palette.Raised
	}
	shape := p.Shape()
	if err := FillShape(dst, control.Bounds, shape, background); err != nil {
		return color.RGBA{}, err
	}
	if err := StrokeShape(dst, control.Bounds, shape, border); err != nil {
		return color.RGBA{}, err
	}
	return foreground, nil
}

// DrawButton paints a text or text-and-icon action control.
func (p *Painter) DrawButton(dst draw.Image, control Control) error {
	if control.Kind != KindTab && control.Kind != KindSegment {
		control.Kind = KindButton
	}
	foreground, err := p.drawSurface(dst, control, true)
	if err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	content := p.ControlContentBounds(controlKindName(control.Kind), control.Bounds)
	if control.Icon != IconNone {
		iconSize := min(p.theme.Metrics.Icon, content.Dy())
		iconBounds := image.Rect(content.Min.X, content.Min.Y+(content.Dy()-iconSize)/2, content.Min.X+iconSize, content.Min.Y+(content.Dy()+iconSize)/2)
		if err := p.DrawIcon(dst, iconBounds, control.Icon, foreground); err != nil {
			return err
		}
		content.Min.X = min(content.Max.X, iconBounds.Max.X+p.theme.Metrics.Gap)
	}
	return p.DrawLabel(dst, content, control.Label, LabelStyle{Color: foreground, Align: AlignCenter})
}

// DrawIconButton paints a square action control with a portable vector glyph.
func (p *Painter) DrawIconButton(dst draw.Image, control Control) error {
	control.Kind = KindIconButton
	foreground, err := p.drawSurface(dst, control, true)
	if err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	return p.DrawIcon(dst, control.Bounds.Inset(max(2, p.theme.Metrics.Padding/2)), control.Icon, foreground)
}

// DrawIcon paints one portable vector glyph with strict clipping.
func (p *Painter) DrawIcon(dst draw.Image, bounds image.Rectangle, icon Icon, ink color.RGBA) error {
	if err := p.ready(dst); err != nil {
		return err
	}
	if bounds.Empty() || bounds.Intersect(dst.Bounds()).Empty() || icon == IconNone {
		return nil
	}
	if p.theme.Skin != nil {
		if _, ok := p.theme.Skin.Icons[string(icon)]; ok {
			return p.DrawNamedIcon(dst, bounds, string(icon), ink)
		}
	}
	if ink.A == 0 {
		ink = p.theme.Palette.Text
	}
	clipped := clippedImage{Image: dst, clip: bounds.Intersect(dst.Bounds())}
	source := image.NewUniform(ink)
	w, h := bounds.Dx(), bounds.Dy()
	x := func(n int) int { return bounds.Min.X + n*w/10 }
	y := func(n int) int { return bounds.Min.Y + n*h/10 }
	line := func(x0, y0, x1, y1 int) {
		strokeSegment(clipped, image.Pt(x(x0), y(y0)), image.Pt(x(x1), y(y1)), max(1, p.theme.Metrics.Stroke), source)
	}
	switch icon {
	case IconAdd:
		line(2, 5, 8, 5)
		line(5, 2, 5, 8)
	case IconRemove:
		line(2, 5, 8, 5)
	case IconClose:
		line(2, 2, 8, 8)
		line(8, 2, 2, 8)
	case IconCheck:
		line(1, 5, 4, 8)
		line(4, 8, 9, 2)
	case IconPlay:
		line(3, 2, 8, 5)
		line(8, 5, 3, 8)
		line(3, 8, 3, 2)
	case IconPause:
		line(3, 2, 3, 8)
		line(7, 2, 7, 8)
	case IconStop:
		for yy := y(3); yy <= y(7); yy++ {
			strokeSegment(clipped, image.Pt(x(3), yy), image.Pt(x(7), yy), 1, source)
		}
	case IconBack:
		line(7, 2, 3, 5)
		line(3, 5, 7, 8)
		line(3, 2, 3, 8)
	case IconForward, IconChevronRight:
		line(3, 2, 7, 5)
		line(7, 5, 3, 8)
		if icon == IconForward {
			line(7, 2, 7, 8)
		}
	case IconSearch:
		strokeCircle(clipped, image.Pt(x(4), y(4)), max(2, min(w, h)*2/10), max(1, p.theme.Metrics.Stroke), ink)
		line(6, 6, 9, 9)
	case IconSettings:
		strokeCircle(clipped, image.Pt(x(5), y(5)), max(2, min(w, h)*3/10), max(1, p.theme.Metrics.Stroke), ink)
		fillCircle(clipped, image.Pt(x(5), y(5)), max(1, min(w, h)/10), ink)
	default:
		return fmt.Errorf("native UI: unknown icon %q", icon)
	}
	return nil
}

func (p *Painter) drawField(dst draw.Image, control Control, multiline bool) error {
	if multiline {
		control.Kind = KindTextArea
	} else {
		control.Kind = KindField
	}
	foreground, err := p.drawSurface(dst, control, false)
	if err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	inner := control.Bounds.Inset(max(1, p.theme.Metrics.Padding))
	if p.theme.Skin != nil {
		inner = p.ControlContentBounds(controlKindName(control.Kind), control.Bounds)
	}
	value, style := control.Text, LabelStyle{Color: foreground}
	if value == "" {
		value, style = control.Placeholder, LabelStyle{Color: p.theme.Palette.Muted}
	}
	if multiline {
		return p.drawMultiline(dst, inner, value, style)
	}
	if err := p.DrawLabel(dst, inner, value, style); err != nil {
		return err
	}
	if control.State.Focused && !control.State.Disabled {
		width := min(inner.Dx(), p.textWidth(control.Text))
		cursor := image.Rect(inner.Min.X+width+1, inner.Min.Y+2, inner.Min.X+width+2, inner.Max.Y-2).Intersect(inner)
		if !cursor.Empty() {
			draw.Draw(dst, cursor, image.NewUniform(p.theme.Palette.Accent), image.Point{}, draw.Over)
		}
	}
	return nil
}

func (p *Painter) DrawField(dst draw.Image, control Control) error {
	return p.drawField(dst, control, false)
}
func (p *Painter) DrawTextArea(dst draw.Image, control Control) error {
	return p.drawField(dst, control, true)
}

func (p *Painter) DrawSwitch(dst draw.Image, control Control) error {
	control.Kind = KindSwitch
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	if p.theme.Skin != nil {
		if err := p.DrawControlBackground(dst, "switch", control.Bounds, control.State); err != nil {
			return err
		}
	}
	trackWidth := min(control.Bounds.Dx()/3, max(36, control.Bounds.Dy()*2))
	track := image.Rect(control.Bounds.Max.X-trackWidth, control.Bounds.Min.Y+max(1, control.Bounds.Dy()/5), control.Bounds.Max.X, control.Bounds.Max.Y-max(1, control.Bounds.Dy()/5))
	trackColor := p.theme.Palette.Surface
	if control.State.Selected {
		trackColor = p.theme.Palette.Selection
	}
	if control.State.Disabled {
		trackColor = p.theme.Palette.Disabled
	}
	if painted, err := p.drawPart(dst, "switch-track", track, control.State); err != nil {
		return err
	} else if !painted {
		if err := FillShape(dst, track, p.Shape(), trackColor); err != nil {
			return err
		}
		if err := StrokeShape(dst, track, p.Shape(), p.theme.Palette.Border); err != nil {
			return err
		}
	}
	radius := max(2, track.Dy()/2-3)
	cx := track.Min.X + track.Dy()/2
	if control.State.Selected {
		cx = track.Max.X - track.Dy()/2
	}
	knob := p.theme.Palette.Muted
	if control.State.Selected {
		knob = p.theme.Palette.Accent
	}
	thumbDst := clippedImage{Image: dst, clip: track.Intersect(dst.Bounds())}
	cy := track.Min.Y + track.Dy()/2
	if painted, err := p.drawPart(thumbDst, "switch-thumb", image.Rect(cx-radius, cy-radius, cx+radius+1, cy+radius+1), control.State); err != nil {
		return err
	} else if !painted {
		fillCircle(thumbDst, image.Pt(cx, cy), radius, knob)
	}
	return p.DrawLabel(dst, image.Rect(control.Bounds.Min.X+p.theme.Metrics.Padding/2, control.Bounds.Min.Y, max(control.Bounds.Min.X, track.Min.X-p.theme.Metrics.Gap), control.Bounds.Max.Y), control.Label, LabelStyle{Color: p.ControlTextColor("switch", control.State)})
}

func (p *Painter) drawChoice(dst draw.Image, control Control, radio bool) error {
	if radio {
		control.Kind = KindRadio
	} else {
		control.Kind = KindCheckbox
	}
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	size := min(control.Bounds.Dy(), max(1, min(control.Bounds.Dy(), p.theme.Metrics.ControlHeight)-8))
	size = max(1, size)
	mark := image.Rect(control.Bounds.Min.X, control.Bounds.Min.Y+(control.Bounds.Dy()-size)/2, control.Bounds.Min.X+size, control.Bounds.Min.Y+(control.Bounds.Dy()+size)/2)
	painted, err := p.drawPart(dst, controlKindName(control.Kind), mark, control.State)
	if err != nil {
		return err
	}
	ink := p.theme.Palette.Border
	if control.State.Focused || control.State.Selected {
		ink = p.theme.Palette.Accent
	}
	if control.State.Disabled {
		ink = p.theme.Palette.Disabled
	}
	if radio {
		if !painted {
			strokeCircle(clippedImage{Image: dst, clip: mark.Intersect(dst.Bounds())}, image.Pt(mark.Min.X+mark.Dx()/2, mark.Min.Y+mark.Dy()/2), size/2-1, max(1, p.theme.Metrics.Stroke), ink)
		}
		if control.State.Selected {
			if painted, err := p.drawPart(dst, "radio-indicator", mark.Inset(max(2, size/4)), control.State); err != nil {
				return err
			} else if !painted {
				fillCircle(dst, image.Pt(mark.Min.X+mark.Dx()/2, mark.Min.Y+mark.Dy()/2), max(2, size/4), ink)
			}
		}
	} else {
		if !painted {
			if err := StrokeShape(dst, mark, ShapeSpec{Grammar: p.theme.Shape, Corner: min(2, p.theme.Metrics.Corner), Notch: 2, Stroke: max(1, p.theme.Metrics.Stroke)}, ink); err != nil {
				return err
			}
		}
		if control.State.Selected {
			if painted, err := p.drawPart(dst, "checkbox-indicator", mark.Inset(2), control.State); err != nil {
				return err
			} else if !painted {
				_ = p.DrawIcon(dst, mark.Inset(2), IconCheck, ink)
			}
		}
	}
	return p.DrawLabel(dst, image.Rect(mark.Max.X+p.theme.Metrics.Gap, control.Bounds.Min.Y, control.Bounds.Max.X, control.Bounds.Max.Y), control.Label, LabelStyle{Color: func() color.RGBA {
		if control.State.Disabled {
			return p.theme.Palette.Disabled
		}
		return p.theme.Palette.Text
	}()})
}

func (p *Painter) DrawCheckbox(dst draw.Image, control Control) error {
	return p.drawChoice(dst, control, false)
}
func (p *Painter) DrawRadio(dst draw.Image, control Control) error {
	return p.drawChoice(dst, control, true)
}

func rangeFraction(control Control) float64 {
	if control.Max <= control.Min {
		return 0
	}
	return min(1, max(0, (control.Value-control.Min)/(control.Max-control.Min)))
}

func (p *Painter) DrawSlider(dst draw.Image, control Control) error {
	control.Kind = KindSlider
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	if p.theme.Skin != nil {
		if err := p.DrawControlBackground(dst, "slider", control.Bounds, control.State); err != nil {
			return err
		}
	}
	layout := p.Layout(control)
	labelWidth := layout.Track.Min.X - control.Bounds.Min.X
	if labelWidth > 0 {
		_ = p.DrawLabel(dst, image.Rect(control.Bounds.Min.X, control.Bounds.Min.Y, control.Bounds.Min.X+labelWidth, control.Bounds.Max.Y), control.Label, LabelStyle{})
	}
	track := layout.Track
	if painted, err := p.drawPart(dst, "slider-track", track, control.State); err != nil {
		return err
	} else if !painted {
		draw.Draw(dst, track.Intersect(dst.Bounds()), image.NewUniform(p.theme.Palette.Border), image.Point{}, draw.Over)
	}
	fraction := rangeFraction(control)
	filled := track
	filled.Max.X = filled.Min.X + int(math.Round(float64(track.Dx())*fraction))
	if painted, err := p.drawPart(dst, "slider-fill", filled, control.State); err != nil {
		return err
	} else if !painted {
		draw.Draw(dst, filled.Intersect(dst.Bounds()), image.NewUniform(p.theme.Palette.Accent), image.Point{}, draw.Over)
	}
	cx := track.Min.X + int(math.Round(float64(max(0, track.Dx()-1))*fraction))
	knob := p.theme.Palette.Accent
	if control.State.Disabled {
		knob = p.theme.Palette.Disabled
	}
	radius := max(4, min(8, control.Bounds.Dy()/3))
	cy := track.Min.Y + 2
	if painted, err := p.drawPart(dst, "slider-thumb", image.Rect(cx-radius, cy-radius, cx+radius+1, cy+radius+1), control.State); err != nil {
		return err
	} else if !painted {
		fillCircle(clippedImage{Image: dst, clip: control.Bounds.Intersect(dst.Bounds())}, image.Pt(cx, cy), radius, knob)
	}
	return nil
}

func (p *Painter) drawProgress(dst draw.Image, control Control, meter bool) error {
	if meter {
		control.Kind = KindMeter
	} else {
		control.Kind = KindProgress
	}
	if control.Max == 0 && control.Min == 0 {
		control.Max = 1
	}
	if control.Value < control.Min {
		control.Value = control.Min
	}
	if control.Value > control.Max {
		control.Value = control.Max
	}
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	shape := p.Shape()
	if p.theme.Skin != nil {
		if err := p.DrawControlBackground(dst, controlKindName(control.Kind), control.Bounds, control.State); err != nil {
			return err
		}
	} else if err := FillShape(dst, control.Bounds, shape, p.theme.Palette.Surface); err != nil {
		return err
	}
	fill := control.Bounds
	fill.Max.X = fill.Min.X + int(math.Round(float64(fill.Dx())*rangeFraction(control)))
	ink := p.theme.Palette.Accent
	if meter {
		fraction := rangeFraction(control)
		if fraction >= .85 {
			ink = p.theme.Palette.Danger
		} else if fraction >= .65 {
			ink = p.theme.Palette.Warning
		} else {
			ink = p.theme.Palette.Success
		}
	}
	part := "progress-fill"
	partState := control.State
	if meter {
		part = "meter-fill"
		partState.Selected = rangeFraction(control) >= .65
		partState.Invalid = rangeFraction(control) >= .85
	}
	if !fill.Empty() {
		if painted, err := p.drawPart(dst, part, fill, partState); err != nil {
			return err
		} else if !painted {
			_ = FillShape(dst, fill, shape, ink)
		}
	}
	if err := StrokeShape(dst, control.Bounds, shape, p.theme.Palette.Border); err != nil {
		return err
	}
	if control.Label != "" {
		label := control.Bounds.Inset(max(1, p.theme.Metrics.Padding/2))
		if err := p.DrawLabel(dst, label, control.Label, LabelStyle{Align: AlignCenter}); err != nil {
			return err
		}
		if p.theme.Skin != nil {
			if _, ok := p.theme.Skin.Controls[part]; ok {
				return p.DrawLabel(clipToBounds(dst, fill), label, control.Label, LabelStyle{Align: AlignCenter, Color: p.ControlTextColor(part, partState)})
			}
		}
		return nil
	}
	return nil
}

func (p *Painter) DrawProgress(dst draw.Image, control Control) error {
	return p.drawProgress(dst, control, false)
}
func (p *Painter) DrawMeter(dst draw.Image, control Control) error {
	return p.drawProgress(dst, control, true)
}

func (p *Painter) drawChoiceStrip(dst draw.Image, controls []Control, kind Kind) error {
	for index := range controls {
		control := controls[index]
		control.Kind = kind
		if err := p.DrawButton(dst, control); err != nil {
			return err
		}
	}
	return nil
}

func (p *Painter) DrawTabs(dst draw.Image, controls []Control) error {
	return p.drawChoiceStrip(dst, controls, KindTab)
}
func (p *Painter) DrawSegments(dst draw.Image, controls []Control) error {
	return p.drawChoiceStrip(dst, controls, KindSegment)
}

func (p *Painter) drawRow(dst draw.Image, control Control, kind Kind) error {
	control.Kind = kind
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	background := p.theme.Palette.Background
	if control.State.Selected {
		background = p.theme.Palette.Selection
	} else if control.State.Pressed {
		background = p.theme.Palette.Pressed
	} else if control.State.Hovered {
		background = p.theme.Palette.Hover
	}
	if p.theme.Skin != nil {
		if err := p.DrawControlBackground(dst, controlKindName(control.Kind), control.Bounds, control.State); err != nil {
			return err
		}
	} else {
		draw.Draw(dst, control.Bounds.Intersect(dst.Bounds()), image.NewUniform(background), image.Point{}, draw.Over)
	}
	if control.State.Focused {
		draw.Draw(dst, image.Rect(control.Bounds.Min.X, control.Bounds.Min.Y, control.Bounds.Min.X+max(1, p.theme.Metrics.Stroke+1), control.Bounds.Max.Y).Intersect(dst.Bounds()), image.NewUniform(p.theme.Palette.Accent), image.Point{}, draw.Over)
	}
	left := control.Bounds.Min.X + p.theme.Metrics.Padding + control.Indent*(p.theme.Metrics.Icon+p.theme.Metrics.Gap)
	if kind == KindTreeRow {
		icon := IconChevronRight
		if control.State.Selected {
			icon = IconRemove
		}
		_ = p.DrawIcon(dst, image.Rect(left, control.Bounds.Min.Y+(control.Bounds.Dy()-p.theme.Metrics.Icon)/2, left+p.theme.Metrics.Icon, control.Bounds.Min.Y+(control.Bounds.Dy()+p.theme.Metrics.Icon)/2), icon, p.theme.Palette.Muted)
		left += p.theme.Metrics.Icon + p.theme.Metrics.Gap
	}
	if control.Icon != IconNone {
		_ = p.DrawIcon(dst, image.Rect(left, control.Bounds.Min.Y+(control.Bounds.Dy()-p.theme.Metrics.Icon)/2, left+p.theme.Metrics.Icon, control.Bounds.Min.Y+(control.Bounds.Dy()+p.theme.Metrics.Icon)/2), control.Icon, p.theme.Palette.Accent)
		left += p.theme.Metrics.Icon + p.theme.Metrics.Gap
	}
	return p.DrawLabel(dst, image.Rect(left, control.Bounds.Min.Y, control.Bounds.Max.X-p.theme.Metrics.Padding, control.Bounds.Max.Y), control.Label, LabelStyle{Color: func() color.RGBA {
		if control.State.Disabled {
			return p.theme.Palette.Disabled
		}
		return p.theme.Palette.Text
	}()})
}

func (p *Painter) DrawMenuItem(dst draw.Image, c Control) error {
	return p.drawRow(dst, c, KindMenuItem)
}
func (p *Painter) DrawListRow(dst draw.Image, c Control) error { return p.drawRow(dst, c, KindListRow) }
func (p *Painter) DrawTreeRow(dst draw.Image, c Control) error { return p.drawRow(dst, c, KindTreeRow) }
func (p *Painter) DrawTableRow(dst draw.Image, c Control) error {
	return p.drawRow(dst, c, KindTableRow)
}

func (p *Painter) DrawMenu(dst draw.Image, rows []Control) error {
	for _, row := range rows {
		if err := p.DrawMenuItem(dst, row); err != nil {
			return err
		}
	}
	return nil
}
func (p *Painter) DrawList(dst draw.Image, rows []Control) error {
	for _, row := range rows {
		if err := p.DrawListRow(dst, row); err != nil {
			return err
		}
	}
	return nil
}
func (p *Painter) DrawTree(dst draw.Image, rows []Control) error {
	for _, row := range rows {
		if err := p.DrawTreeRow(dst, row); err != nil {
			return err
		}
	}
	return nil
}
func (p *Painter) DrawTable(dst draw.Image, rows []Control) error {
	for _, row := range rows {
		if err := p.DrawTableRow(dst, row); err != nil {
			return err
		}
	}
	return nil
}

func (p *Painter) DrawScrollbar(dst draw.Image, control Control) error {
	control.Kind = KindScrollbar
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	if p.theme.Skin != nil {
		if err := p.DrawControlBackground(dst, "scrollbar", control.Bounds, control.State); err != nil {
			return err
		}
	} else {
		draw.Draw(dst, control.Bounds.Intersect(dst.Bounds()), image.NewUniform(p.theme.Palette.Surface), image.Point{}, draw.Over)
	}
	thumb := p.Layout(control).Thumb
	if painted, err := p.drawPart(dst, "scrollbar-thumb", thumb, control.State); painted || err != nil {
		return err
	}
	return FillShape(dst, thumb, p.Shape(), func() color.RGBA {
		if control.State.Disabled {
			return p.theme.Palette.Disabled
		}
		return p.theme.Palette.Accent
	}())
}

func (p *Painter) DrawSplitter(dst draw.Image, control Control) error {
	control.Kind = KindSplitter
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	ink := p.theme.Palette.Border
	if control.State.Hovered || control.State.Pressed || control.State.Focused {
		ink = p.theme.Palette.Accent
	}
	center := control.Bounds
	if control.Bounds.Dy() >= control.Bounds.Dx() {
		center.Min.X = control.Bounds.Min.X + control.Bounds.Dx()/2
		center.Max.X = center.Min.X + max(1, p.theme.Metrics.Stroke)
	} else {
		center.Min.Y = control.Bounds.Min.Y + control.Bounds.Dy()/2
		center.Max.Y = center.Min.Y + max(1, p.theme.Metrics.Stroke)
	}
	draw.Draw(dst, center.Intersect(dst.Bounds()), image.NewUniform(ink), image.Point{}, draw.Over)
	return nil
}

func (p *Painter) drawContainer(dst draw.Image, control Control, kind Kind, raised bool) error {
	control.Kind = kind
	foreground, err := p.drawSurface(dst, control, raised)
	if err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	if control.Label == "" {
		return nil
	}
	header := image.Rect(control.Bounds.Min.X+p.theme.Metrics.Padding, control.Bounds.Min.Y+p.theme.Metrics.Padding/2, control.Bounds.Max.X-p.theme.Metrics.Padding, min(control.Bounds.Max.Y, control.Bounds.Min.Y+p.theme.Metrics.ControlHeight))
	return p.DrawLabel(dst, header, control.Label, LabelStyle{Color: foreground})
}

func (p *Painter) DrawPanel(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindPanel, false)
}
func (p *Painter) DrawCard(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindCard, true)
}
func (p *Painter) DrawToolbar(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindToolbar, true)
}
func (p *Painter) DrawDialog(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindDialog, true)
}
func (p *Painter) DrawPopover(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindPopover, true)
}
func (p *Painter) DrawTooltip(dst draw.Image, c Control) error {
	return p.drawContainer(dst, c, KindTooltip, true)
}

func (p *Painter) DrawBadge(dst draw.Image, control Control) error {
	control.Kind = KindBadge
	foreground, err := p.drawSurface(dst, control, true)
	if err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	return p.DrawLabel(dst, control.Bounds.Inset(max(1, p.theme.Metrics.Padding/2)), control.Label, LabelStyle{Color: foreground, Align: AlignCenter})
}

func (p *Painter) DrawSeparator(dst draw.Image, control Control) error {
	control.Kind = KindSeparator
	if err := control.validate(false); err != nil {
		return err
	}
	if err := p.ready(dst); err != nil {
		return err
	}
	dst = clipToBounds(dst, control.Bounds)
	rect := control.Bounds
	if rect.Dy() > rect.Dx() {
		rect.Min.X += rect.Dx() / 2
		rect.Max.X = rect.Min.X + max(1, p.theme.Metrics.Stroke)
	} else {
		rect.Min.Y += rect.Dy() / 2
		rect.Max.Y = rect.Min.Y + max(1, p.theme.Metrics.Stroke)
	}
	draw.Draw(dst, rect.Intersect(dst.Bounds()), image.NewUniform(p.theme.Palette.Border), image.Point{}, draw.Over)
	return nil
}

func fillCircle(dst draw.Image, center image.Point, radius int, ink color.Color) {
	if dst == nil || radius <= 0 {
		return
	}
	source := image.NewUniform(ink)
	for y := -radius; y <= radius; y++ {
		x := int(math.Sqrt(float64(radius*radius - y*y)))
		rect := image.Rect(center.X-x, center.Y+y, center.X+x+1, center.Y+y+1).Intersect(dst.Bounds())
		if !rect.Empty() {
			draw.Draw(dst, rect, source, image.Point{}, draw.Over)
		}
	}
}

func strokeCircle(dst draw.Image, center image.Point, radius, width int, ink color.Color) {
	if dst == nil || radius <= 0 {
		return
	}
	source := image.NewUniform(ink)
	steps := max(16, radius*8)
	previous := image.Pt(center.X+radius, center.Y)
	for i := 1; i <= steps; i++ {
		angle := 2 * math.Pi * float64(i) / float64(steps)
		next := image.Pt(center.X+int(math.Round(math.Cos(angle)*float64(radius))), center.Y+int(math.Round(math.Sin(angle)*float64(radius))))
		strokeSegment(dst, previous, next, max(1, width), source)
		previous = next
	}
}
