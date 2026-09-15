// Package compositor hosts the Wayland server and optional rootless XWayland.
//
// Client surfaces (including Xwayland xdg_toplevels) become engine window actors.
package compositor

import (
	"github.com/codemodify/worldr/internal/compositor/wlsrv"
	"github.com/codemodify/worldr/internal/engine"
)

// Server is the compositor core.
type Server = wlsrv.Server

// DMABufImport is the GPU client buffer importer.
type DMABufImport = wlsrv.DMABufImport

// DMABufPlane is one linux-dmabuf plane.
type DMABufPlane = wlsrv.DMABufPlane

// X11MapHints is XWM metadata applied to an xwayland actor.
type X11MapHints = wlsrv.X11MapHints

// ApplyX11Hints updates a scene actor when X11 properties change.
func ApplyX11Hints(scene *engine.Scene, h X11MapHints) *engine.Actor {
	return wlsrv.ApplyX11Hints(scene, h)
}

// ScaleTo120ths converts a display scale to wp_fractional_scale units.
func ScaleTo120ths(scale float64) uint32 {
	return wlsrv.ScaleTo120ths(scale)
}

// Listen starts a Wayland socket. imp may be nil (shm-only).
func Listen(displayName string, scene *engine.Scene, screenW, screenH int, imp wlsrv.DMABufImport) (*Server, error) {
	return wlsrv.Listen(displayName, scene, screenW, screenH, imp)
}

// ToXKBKeycode maps evdev → XKB (evdev+8). alreadyXKB is true for nested host keys.
func ToXKBKeycode(code uint32, alreadyXKB bool) uint32 {
	return wlsrv.ToXKBKeycode(code, alreadyXKB)
}
