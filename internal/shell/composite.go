package shell

import (
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

// CompositeDesktop draws the cinematic clear, window actors, optional SSD,
// and software cursor into a BGRA framebuffer. This is the present path
// used by vk-display, drm, headless (when compositing), and nested
// wayland-client.
func CompositeDesktop(dst []byte, stride, w, h int, clear uint32, actors []*engine.Actor, ssd bool, cursor CursorBlit) {
	engine.FillBGRA(dst, stride, w, h, clear)
	for _, a := range actors {
		if ssd {
			decorations.Draw(dst, stride, w, h, a)
		}
		engine.BlitBGRA(dst, stride, w, h, a.X, a.Y, a.Pixels, a.Stride, a.Width, a.Height)
	}
	if cursor.Visible {
		decorations.OverlayCursor(dst, stride, w, h, cursor.X, cursor.Y, cursor.HX, cursor.HY,
			cursor.Pix, cursor.W, cursor.H, cursor.Stride, cursor.Shape)
	}
}
