package nativeapps

import (
	"image"
	"image/color"
)

// Small opaque plates and one-pixel rails stay within the existing terminal
// header/footer. Luminous scene borders remain the workspace renderer's job.
func terminalChromePlate(dst *image.RGBA, rect image.Rectangle, cut int, c color.RGBA) {
	cut = min(cut, min(rect.Dx(), rect.Dy())/2)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		inset := max(0, max(cut-(y-rect.Min.Y), cut-(rect.Max.Y-1-y)))
		fill(dst, image.Rect(rect.Min.X+inset, y, rect.Max.X-inset, y+1), c)
	}
}

func terminalChromeLine(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := x1-x0, y1-y0
	steps := max(max(dx, -dx), max(dy, -dy))
	for i := 0; i <= steps; i++ {
		x, y := x0, y0
		if steps > 0 {
			x, y = x0+dx*i/steps, y0+dy*i/steps
		}
		dst.SetRGBA(x, y, c)
	}
}
