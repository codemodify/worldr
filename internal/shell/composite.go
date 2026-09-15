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

// Theater is the Compiz-style pose applied on the shared present path
// (nested wayland-client, vk-display, drm).
type Theater struct {
	Now    time.Time
	Tier   engine.Tier
	PanelH int // minimize-to-panel target
}

// CompositeDesktop draws the cinematic clear, window actors, optional SSD,
// and software cursor into a BGRA framebuffer. This is the present path
// used by vk-display, drm, headless (when compositing), and nested
// wayland-client.
func CompositeDesktop(dst []byte, stride, w, h int, clear uint32, actors []*engine.Actor, ssd bool, cursor CursorBlit, fx Theater, ov OverviewDraw, ch ChromeDraw, gpuOverlay bool) {
	engine.FillBGRA(dst, stride, w, h, clear)
	deskH := usableHeight(h, ch.PanelH)
	if ov.T > 0 {
		if ch.WS.Count > 1 {
			actors = compactWorkspace(actors, ch.WS.Active)
		}
		actors = compactChrome(actors)
		engine.FillRectAlpha(dst, stride, w, h, 0, 0, w, deskH, 0xff000000, 0.38*ov.T)
		if ch.WS.Count > 1 {
			lbl := "desk " + engine.WorkspaceLabel(ch.WS.Active, ch.WS.Count)
			engine.DrawText(dst, stride, w, h, 12, 10, lbl, colBrand, 2)
		}
		cells := engine.LayoutGrid(len(actors), w, deskH)
		for i, a := range actors {
			if a == nil || i >= len(cells) {
				continue
			}
			home := engine.HomeFrame(a, ssd, decorations.Border, decorations.TitleH)
			fit := engine.FitInCell(home.W, home.H, cells[i])
			dest := engine.LerpCell(home, fit, ov.T)
			sel := i == ov.Select
			drawActorIn(dst, stride, w, h, a, dest, ssd, sel, 1)
		}
	} else {
		for _, a := range actors {
			ox, show := ch.WS.OffsetFor(a.Workspace, w)
			if !show {
				continue
			}
			drawActor(dst, stride, w, h, a, ssd, fx, ox, gpuOverlay)
		}
	}
	if ch.Launcher != nil {
		drawLauncher(dst, stride, w, h, ch.PanelH, *ch.Launcher)
	}
	if ch.PanelH > 0 {
		drawPanel(dst, stride, w, h, ch)
	}
	if cursor.Visible {
		decorations.OverlayCursor(dst, stride, w, h, cursor.X, cursor.Y, cursor.HX, cursor.HY,
			cursor.Pix, cursor.W, cursor.H, cursor.Stride, cursor.Shape)
	}
}

// compactWorkspace filters in-place (snapshot only; no new alloc).
func compactWorkspace(actors []*engine.Actor, ws int) []*engine.Actor {
	n := 0
	for _, a := range actors {
		if a != nil && a.Workspace == ws {
			actors[n] = a
			n++
		}
	}
	return actors[:n]
}

// compactChrome filters in-place (snapshot only).
func compactChrome(actors []*engine.Actor) []*engine.Actor {
	n := 0
	for _, a := range actors {
		if a != nil && !a.NoChrome {
			actors[n] = a
			n++
		}
	}
	return actors[:n]
}

func drawActor(dst []byte, stride, w, h int, a *engine.Actor, ssd bool, fx Theater, ox int, gpuOverlay bool) {
	if a == nil {
		return
	}
	if ox != 0 {
		old := a.X
		a.X += ox
		defer func() { a.X = old }()
	}
	v := a.VisualAt(fx.Now, fx.Tier)
	if v.Gone {
		return
	}
	mx, my := 0, 0
	if fx.Tier == engine.TierHigh && v.Phase == engine.PhaseMapOut {
		mx, my = engine.MinimizeDelta(a, v, w, h, fx.PanelH)
	}
	slideA := engine.SlideFade(ox, w)
	ssd = ssd && !a.NoChrome
	if v.Identity() && slideA >= 0.999 && mx == 0 && my == 0 {
		if ssd {
			decorations.Draw(dst, stride, w, h, a)
		}
		if a.PlaneSkip || (gpuOverlay && a.GPUSlot > 0 && !a.ScaledBuffer()) {
			return
		}
		srcW, srcH := a.PixelSize()
		if a.ScaledBuffer() {
			engine.BlitBGRAScaledAlpha(dst, stride, w, h, a.X, a.Y, a.Width, a.Height, a.Pixels, a.Stride, srcW, srcH, 1)
		} else {
			engine.BlitBGRA(dst, stride, w, h, a.X, a.Y, a.Pixels, a.Stride, a.Width, a.Height)
		}
		return
	}
	fw := a.Width + 2*decorations.Border
	fh := a.Height + decorations.TitleH + decorations.Border
	if !ssd {
		fw, fh = a.Width, a.Height
	}
	cx := a.X + a.Width/2 + v.SlideX + mx
	cy := a.Y + a.Height/2 + v.SlideY + my
	if ssd {
		cx = a.X - decorations.Border + fw/2 + v.SlideX + mx
		cy = a.Y - decorations.TitleH + fh/2 + v.SlideY + my
	}
	sw := scaleI(fw, v.Scale)
	sh := scaleI(fh, v.Scale)
	sx := cx - sw/2
	sy := cy - sh/2 - v.Lift
	alpha := v.Alpha * slideA
	if v.Shadow > 0 {
		pad := 10
		engine.FillRectAlpha(dst, stride, w, h, sx-pad, sy-pad+6, sw+2*pad, sh+2*pad, 0xff000010, v.Shadow)
	}
	if v.Glow > 0 {
		drawFocusGlow(dst, stride, w, h, sx, sy, sw, sh, v.Glow)
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
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, sw, th, title, alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, sw, scaleI(decorations.AccentH, v.Scale), decorations.TitleStripe(), alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy, bd, sh, frame, alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx+sw-bd, sy, bd, sh, frame, alpha)
		engine.FillRectAlpha(dst, stride, w, h, sx, sy+sh-bd, sw, bd, frame, alpha)
		cw, ch := scaleI(a.Width, v.Scale), scaleI(a.Height, v.Scale)
		srcW, srcH := a.PixelSize()
		engine.BlitBGRAScaledAlpha(dst, stride, w, h, sx+bd, sy+th, cw, ch, a.Pixels, a.Stride, srcW, srcH, alpha)
		return
	}
	srcW, srcH := a.PixelSize()
	engine.BlitBGRAScaledAlpha(dst, stride, w, h, sx, sy, sw, sh, a.Pixels, a.Stride, srcW, srcH, alpha)
}

func drawFocusGlow(dst []byte, stride, w, h, x, y, fw, fh int, glow float64) {
	if glow <= 0 {
		return
	}
	pixel := decorations.FocusGlow()
	for i, a := range []float64{0.28, 0.16, 0.08} {
		pad := (i + 1) * 2
		engine.FillRectAlpha(dst, stride, w, h, x-pad, y-pad, fw+2*pad, 2, pixel, a*glow)
		engine.FillRectAlpha(dst, stride, w, h, x-pad, y+fh+pad-2, fw+2*pad, 2, pixel, a*glow)
		engine.FillRectAlpha(dst, stride, w, h, x-pad, y-pad, 2, fh+2*pad, pixel, a*glow)
		engine.FillRectAlpha(dst, stride, w, h, x+fw+pad-2, y-pad, 2, fh+2*pad, pixel, a*glow)
	}
}

// OverviewDraw is the expose pose (T=0 is the normal desktop).
type OverviewDraw struct {
	T      float64
	Select int
}

func drawActorIn(dst []byte, stride, w, h int, a *engine.Actor, dest engine.GridCell, ssd, selected bool, alpha float64) {
	if a == nil || dest.W <= 0 || dest.H <= 0 {
		return
	}
	ssd = ssd && !a.NoChrome
	if selected {
		pad := 6
		engine.FillRectAlpha(dst, stride, w, h, dest.X-pad, dest.Y-pad, dest.W+2*pad, dest.H+2*pad, 0xffff6ad5, 0.22*alpha)
	}
	if ssd {
		th := dest.H * decorations.TitleH / max1(a.Height+decorations.TitleH+decorations.Border)
		bd := dest.W * decorations.Border / max1(a.Width+2*decorations.Border)
		if th < 1 {
			th = 1
		}
		if bd < 1 {
			bd = 1
		}
		ah := dest.H * decorations.AccentH / max1(a.Height+decorations.TitleH+decorations.Border)
		if ah < 2 {
			ah = 2
		}
		frame, title := decorations.FrameColors(selected || a.Focused)
		engine.FillRectAlpha(dst, stride, w, h, dest.X, dest.Y, dest.W, th, title, alpha)
		engine.FillRectAlpha(dst, stride, w, h, dest.X, dest.Y, dest.W, ah, decorations.TitleStripe(), alpha)
		engine.FillRectAlpha(dst, stride, w, h, dest.X, dest.Y, bd, dest.H, frame, alpha)
		engine.FillRectAlpha(dst, stride, w, h, dest.X+dest.W-bd, dest.Y, bd, dest.H, frame, alpha)
		engine.FillRectAlpha(dst, stride, w, h, dest.X, dest.Y+dest.H-bd, dest.W, bd, frame, alpha)
		cw, ch := dest.W-2*bd, dest.H-th-bd
		srcW, srcH := a.PixelSize()
		engine.BlitBGRAScaledAlpha(dst, stride, w, h, dest.X+bd, dest.Y+th, cw, ch, a.Pixels, a.Stride, srcW, srcH, alpha)
		return
	}
	srcW, srcH := a.PixelSize()
	engine.BlitBGRAScaledAlpha(dst, stride, w, h, dest.X, dest.Y, dest.W, dest.H, a.Pixels, a.Stride, srcW, srcH, alpha)
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
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
