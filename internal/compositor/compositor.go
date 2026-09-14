// Package compositor hosts the Wayland server and (later) XWayland.
//
// Phase 2: minimal xdg_shell compositor. Surfaces become engine window actors.
// XWayland is not hooked up. Known gaps are listed in docs/RUN-ABOX.md.
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

// Listen starts a Wayland socket. imp may be nil (shm-only).
func Listen(displayName string, scene *engine.Scene, screenW, screenH int, imp wlsrv.DMABufImport) (*Server, error) {
	return wlsrv.Listen(displayName, scene, screenW, screenH, imp)
}
