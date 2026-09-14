package shell

import (
	"time"

	"github.com/codemodify/worldr/internal/decorations"
	"github.com/codemodify/worldr/internal/engine"
)

// CursorBlit is the software cursor overlay for CompositeDesktop.
type CursorBlit struct {
	X, Y, HX, HY int
	Pix          []byte
	W, H, Stride int
	Shape        uint32
	Visible      bool
}

// Theater is the v0 Compiz-style pose applied on the shared present path
// (nested wayland-client, vk-display, drm).
type Theater struct {
	Now  time.Time
	Tier engine.Tier
}

// CompositeDesktop draws the cinematic clear, window actors, optional SSD,
// and software cursor into a BGRA framebuffer. This is the present path
// used by vk-display, drm, headless (when compositing), and nested
// wayland-client.
func CompositeDesktop(dst []byte, stride, w, h int, clear uint32, actors []*engine.Actor, ssd bool, cursor CursorBlit, fx Theater) {
	engine.FillBGRA(dst, stride, w, h, clear)
	for _, a := range actors {
		drawActor(dst, stride, w, h, a, ssd, fx)
	}
	if cursor.Visible {
		decorations.OverlayCursor(dst, stride, w, h, cursor.X, cursor.Y, cursor.HX, cursor.HY,
			cursor.Pix, cursor.W, cursor.H, cursor.Stride, cursor.Shape)
	}
}

func drawActor(dst []byte, stride, w, h int, a *engine.Actor, ssd bool, fx Theater) {
	if a == nil {
		return
	}
	v := a.VisualAt(fx.Now, fx.Tier)
	if v.Gone {
		return
	}
	if v.Identity() {
		if ssd {
			decorations.Draw(dst, stride, w, h, a)
		}
		engine.BlitBGRA(dst, stride, w, h, a.X, a.Y, a.Pixels, a.Stride, a.Width, a.Height)
		return
	}
	fw := a.Width + 2*decorations.Border
	fh := a.Height + decorations.TitleH + decorations.Border
	if !ssd {
		fw, fh = a.Width, a.Height
	}
	cx := a.X + a.Width/2
	cy := a.Y + a.Height/2
	if ssd {
		cx = a.X - decorations.Border + fw/2
		cy = a.Y - decorations.TitleH + fh/2
	}
	sw := scaleI(fw, v.Scale)
	sh := scaleI(fh, v.Scale)
	sx := cx - sw/2
	sy := cy - sh/2 - v.Lift
	if v.Shadow > 0 {
		pad := 10
		engine.FillRectAlpha(dst, stride, w, h, sx-pad, sy-pad+6, sw+2*pad, sh+2*pad, 0xff000010, v.Shadow)
	}
	if ssd {
		th := scaleI(decorations.TitleH, v.Scale)
		bd := scaleI(decorations.Border, v.Scale)
		if th < 1 {
			th = 1
		}
		if bd < 1 {
			bd = 1
		}
		frame, title := decorations.FrameColors(a.Focused)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, sw, th, title, v.Alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, sw, scaleI(2, v.Scale), decorations.TitleStripe(), v.Alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, bd, sh, frame, v.Alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx+sw-bd, sy, bd, sh, frame, v.Alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy+sh-bd, sw, bd, frame, v.Alpha)
		cw, ch := scaleI(a.Width, v.Scale), scaleI(a.Height, v.Scale)
		engine.BlitBGRAScaledAlpha(dst, stride, w, h, sx+bd, sy+th, cw, ch, a.Pixels, a.Stride, a.Width, a.Height, v.Alpha)
		return
	}
	engine.BlitBGRAScaledAlpha(dst, stride, w, h, sx, sy, sw, sh, a.Pixels, a.Stride, a.Width, a.Height, v.Alpha)
}

func scaleI(n int, s float64) int {
	if n <= 0 {
		return 0
	}
	v := int(float64(n)*s + 0.5)
	if v < 1 {
		return 1
	}
	return v
}
