package nativeui

import (
	"image"
	"math"
)

// ControlLayout is the geometry shared by painting and pointer input. Track is
// the actual value domain, after text/insets. Thumb is the rendered scroll grip.
type ControlLayout struct{ Content, Track, Thumb image.Rectangle }

func (p *Painter) Layout(control Control) ControlLayout {
	l := ControlLayout{Content: p.ControlContentBounds(controlKindName(control.Kind), control.Bounds), Track: control.Bounds}
	if control.Kind == KindSlider {
		labelWidth := 0
		if control.Label != "" {
			labelWidth = min(control.Bounds.Dx()/3, max(70, p.textWidth(control.Label)+p.theme.Metrics.Gap))
		}
		left, right := control.Bounds.Min.X+labelWidth, control.Bounds.Max.X
		if p.theme.Skin != nil {
			left = max(left, l.Content.Min.X)
			right = max(left, l.Content.Max.X)
		}
		cy := control.Bounds.Min.Y + control.Bounds.Dy()/2
		l.Track = image.Rect(left, cy-2, right, cy+2)
	}
	if control.Kind == KindScrollbar {
		vertical := control.Bounds.Dy() >= control.Bounds.Dx()
		span := control.Bounds.Dx()
		if vertical {
			span = control.Bounds.Dy()
		}
		pageFraction := min(1, max(.05, control.Page/(control.Max-control.Min+control.Page)))
		thumbSpan := min(span, max(6, int(float64(span)*pageFraction)))
		start := int(math.Round(float64(span-thumbSpan) * rangeFraction(control)))
		l.Thumb = control.Bounds
		if vertical {
			l.Thumb.Min.Y += start
			l.Thumb.Max.Y = l.Thumb.Min.Y + thumbSpan
		} else {
			l.Thumb.Min.X += start
			l.Thumb.Max.X = l.Thumb.Min.X + thumbSpan
		}
	}
	return l
}
