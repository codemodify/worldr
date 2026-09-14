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

// Listen starts a Wayland socket.
func Listen(displayName string, scene *engine.Scene, screenW, screenH int) (*Server, error) {
	return wlsrv.Listen(displayName, scene, screenW, screenH)
}
