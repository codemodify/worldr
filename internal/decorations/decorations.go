// Package decorations implements server-side decorations (SSD).
//
// Foreign and native windows get compositor-owned chrome: a thin futuristic
// frame and a title hit region. This is a colored rectangle, not a toolkit.
package decorations

import "github.com/codemodify/worldr/internal/engine"

// Geometry of the chrome around the client buffer.
const (
	Border = 6
	TitleH = 26
)

const (
	colFrame       uint32 = 0xff3de0dc // BGRA: cyan-ish
	colFrameFocus  uint32 = 0xffff6ad5 // magenta
	colTitle       uint32 = 0xff1a2233
	colTitleFocus  uint32 = 0xff2a3350
	colTitleStripe uint32 = 0xff7cf0e8
)

// FrameColors is SSD chrome for focused vs idle windows (theater blit uses this).
func FrameColors(focused bool) (frame, title uint32) {
	if focused {
		return colFrameFocus, colTitleFocus
	}
	return colFrame, colTitle
}

// TitleStripe is the thin highlight on the title bar.
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
	frame, title := FrameColors(a.Focused)
	x0 := a.X - Border
	y0 := a.Y - TitleH
	x1 := a.X + a.Width + Border
	y1 := a.Y + a.Height + Border
	engine.FillRect(dst, stride, dW, dH, x0, y0, x1-x0, TitleH, title)
	engine.FillRect(dst, stride, dW, dH, x0, y0, x1-x0, 2, colTitleStripe)
	engine.FillRect(dst, stride, dW, dH, x0, y0, Border, y1-y0, frame)
	engine.FillRect(dst, stride, dW, dH, x1-Border, y0, Border, y1-y0, frame)
	engine.FillRect(dst, stride, dW, dH, x0, y1-Border, x1-x0, Border, frame)
}

// HitTitle reports whether (px,py) is on the title bar (not the client buffer).
func HitTitle(a *engine.Actor, px, py int) bool {
	if a == nil {
		return false
	}
	return px >= a.X-Border && px < a.X+a.Width+Border && py >= a.Y-TitleH && py < a.Y
}
