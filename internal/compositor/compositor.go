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

// Output is one logical wl_output.
type Output = wlsrv.Output

// MaxOutputs is the number of logical wl_output globals we advertise.
const MaxOutputs = wlsrv.MaxOutputs

// ScaleTo120ths converts a display scale to wp_fractional_scale units.
func ScaleTo120ths(scale float64) uint32 {
	return wlsrv.ScaleTo120ths(scale)
}

// ScaleFrom120ths is the floating scale encoded by n.
func ScaleFrom120ths(n uint32) float64 {
	return wlsrv.ScaleFrom120ths(n)
}

// LayoutOutputs tiles n logical outputs across the present size.
func LayoutOutputs(screenW, screenH, n int, scales []float64) []Output {
	return wlsrv.LayoutOutputs(screenW, screenH, n, scales)
}

// ParseOutputScales reads a comma list of per-output scales.
func ParseOutputScales(s string) ([]float64, error) {
	return wlsrv.ParseOutputScales(s)
}

// OutputSeams are interior X edges between tiled outputs.
func OutputSeams(outs []Output) []int {
	return wlsrv.OutputSeams(outs)
}

// OutputSeamsInto appends interior X edges into dst (reuses dst).
func OutputSeamsInto(dst []int, outs []Output) []int {
	return wlsrv.OutputSeamsInto(dst, outs)
}

// Listen starts a Wayland socket. imp may be nil (shm-only).
func Listen(displayName string, scene *engine.Scene, screenW, screenH int, imp wlsrv.DMABufImport) (*Server, error) {
	return wlsrv.Listen(displayName, scene, screenW, screenH, imp)
}

// ToXKBKeycode maps evdev → XKB (evdev+8). alreadyXKB is true for nested host keys.
func ToXKBKeycode(code uint32, alreadyXKB bool) uint32 {
	return wlsrv.ToXKBKeycode(code, alreadyXKB)
}
