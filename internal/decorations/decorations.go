// Package decorations implements server-side decorations (SSD).
//
// Foreign and native windows get compositor-owned chrome: a thin futuristic
// frame and a title hit region. This is a colored rectangle, not a toolkit.
package decorations

import "github.com/codemodify/worldr/internal/engine"

// Geometry of the chrome around the client buffer.
const (
	Border  = 6
	TitleH  = 28
	AccentH = 4
)

const (
	colFrame       uint32 = 0xff3de0dc // BGRA: cyan-ish
	colFrameFocus  uint32 = 0xffff6ad5 // magenta
	colTitle       uint32 = 0xff1a2233
	colTitleFocus  uint32 = 0xff2a3350
	colTitleStripe uint32 = 0xff7cf0e8
	colTitleHi     uint32 = 0xff243044
	colTitleHiF    uint32 = 0xff3a4468
)

// FrameColors is SSD chrome for focused vs idle windows (theater blit uses this).
func FrameColors(focused bool) (frame, title uint32) {
	if focused {
		return colFrameFocus, colTitleFocus
	}
	return colFrame, colTitle
}

// TitleStripe is the thick accent on the title bar.
func TitleStripe() uint32 { return colTitleStripe }

// Insets around the client surface (left, right, top, bottom).
func Insets() (l, r, t, b int) {
	return Border, Border, TitleH, Border
}

// Draw paints chrome for actor a onto the destination framebuffer.
func Draw(dst []byte, stride, dW, dH int, a *engine.Actor) {
	if a == nil {
		return
	}
	frame, _ := FrameColors(a.Focused)
	x0 := a.X - Border
	y0 := a.Y - TitleH
	x1 := a.X + a.Width + Border
	y1 := a.Y + a.Height + Border
	fw, fh := x1-x0, y1-y0
	if a.Focused {
		drawGlow(dst, stride, dW, dH, x0, y0, fw, fh, frame)
	}
	drawTitleBar(dst, stride, dW, dH, x0, y0, fw, a.Focused)
	engine.FillRect(dst, stride, dW, dH, x0, y0, fw, AccentH, colTitleStripe)
	engine.FillRect(dst, stride, dW, dH, x0, y0, Border, fh, frame)
	engine.FillRect(dst, stride, dW, dH, x1-Border, y0, Border, fh, frame)
	engine.FillRect(dst, stride, dW, dH, x0, y1-Border, fw, Border, frame)
}

func drawTitleBar(dst []byte, stride, dW, dH, x, y, w int, focused bool) {
	top, bot := colTitle, colTitleHi
	if focused {
		top, bot = colTitleFocus, colTitleHiF
	}
	for i := 0; i < TitleH; i++ {
		t := float64(i) / float64(TitleH)
		engine.FillRect(dst, stride, dW, dH, x, y+i, w, 1, lerpBGRA(top, bot, t))
	}
}

func drawGlow(dst []byte, stride, dW, dH, x, y, w, h int, pixel uint32) {
	for i, a := range []float64{0.22, 0.12, 0.06} {
		pad := i + 1
		engine.FillRectAlpha(dst, stride, dW, dH, x-pad, y-pad, w+2*pad, 1, pixel, a)
		engine.FillRectAlpha(dst, stride, dW, dH, x-pad, y+h+pad-1, w+2*pad, 1, pixel, a)
		engine.FillRectAlpha(dst, stride, dW, dH, x-pad, y-pad, 1, h+2*pad, pixel, a)
		engine.FillRectAlpha(dst, stride, dW, dH, x+w+pad-1, y-pad, 1, h+2*pad, pixel, a)
	}
}

func lerpBGRA(a, b uint32, t float64) uint32 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	mix := func(shift uint) uint32 {
		av := float64((a >> shift) & 0xff)
		bv := float64((b >> shift) & 0xff)
		return uint32(av+(bv-av)*t+0.5) & 0xff
	}
	return mix(0) | mix(8)<<8 | mix(16)<<16 | mix(24)<<24
}

// HitTitle reports whether (px,py) is on the title bar (not the client buffer).
func HitTitle(a *engine.Actor, px, py int) bool {
	if a == nil {
		return false
	}
	return px >= a.X-Border && px < a.X+a.Width+Border && py >= a.Y-TitleH && py < a.Y
}
