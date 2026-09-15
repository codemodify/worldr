package scanout

import "github.com/codemodify/worldr/internal/engine"

// Hardware plane assignment beyond the primary-fullscreen path.
// Nested / headless never get overlay or cursor planes. vk-display is
// eligible in the helper (Intel first) but typically cannot commit extra
// DRM planes while VK_KHR_display holds master — callers fall back to compose.

// CursorMax is the typical Intel hardware cursor cap (width and height).
const CursorMax = 256

// PlaneAssign is one frame's overlay / cursor / primary decision.
type PlaneAssign struct {
	Primary *engine.Actor
	Overlay *engine.Actor
	Cursor  bool
	Reason  string
	// NeedDRM is true when the assign wants a libdrm atomic commit
	// (vk-display usually cannot).
	NeedDRM bool
}

// PlaneFrame is Evaluate plus cursor size and Vulkan display plane count.
type PlaneFrame struct {
	Frame
	CursorVisible bool
	CursorW       int
	CursorH       int
	DisplayPlanes int // VK_KHR_display plane count; 0 = unknown
}

// CursorPlaneOK is a hardware cursor candidate: KMS, visible, small ARGB.
func CursorPlaneOK(backend string, visible bool, cw, ch int) bool {
	if !KMSBackend(backend) || !visible {
		return false
	}
	if cw < 1 || ch < 1 || cw > CursorMax || ch > CursorMax {
		return false
	}
	return true
}

// OverlayActorOK is a windowed (not fullscreen) dmabuf that can sit on
// an overlay / underlay while the primary shows the desktop.
func OverlayActorOK(backend string, hasScan bool, covers, scaled bool, w, h int) bool {
	if !KMSBackend(backend) || !hasScan || covers || scaled {
		return false
	}
	return w > 0 && h > 0
}

// VKDisplayCanOverlay reports extra VK_KHR_display planes (n > 1).
// The present path still needs DRM master to commit them; this is
// eligibility only.
func VKDisplayCanOverlay(backend string, displayPlanes int) bool {
	return backend == "vk-display" && displayPlanes > 1
}

// EvaluatePlanes picks at most one overlay actor, optional cursor plane,
// and the existing primary-fullscreen actor. Nested stays empty.
func EvaluatePlanes(f PlaneFrame) PlaneAssign {
	out := PlaneAssign{Reason: "compose"}
	if !KMSBackend(f.Backend) {
		out.Reason = "not kms"
		return out
	}
	if f.Overview || f.Launcher || Sliding(f.WS) {
		out.Reason = "chrome"
		return out
	}
	prim := Evaluate(f.Frame)
	if prim.OK {
		out.Primary = prim.Actor
		out.NeedDRM = true
		out.Reason = "primary"
	}
	if CursorPlaneOK(f.Backend, f.CursorVisible, f.CursorW, f.CursorH) {
		out.Cursor = true
		out.NeedDRM = true
		if out.Reason == "compose" {
			out.Reason = "cursor"
		}
	}
	if prim.OK {
		// Fullscreen primary already owns the CRTC; skip overlay.
		return out
	}
	if theaterAnimating(f) {
		return out
	}
	for _, a := range f.Actors {
		if a == nil {
			continue
		}
		ox, show := f.WS.OffsetFor(a.Workspace, f.ScreenW)
		if !show || ox != 0 {
			continue
		}
		covers := CoversOutput(a, f.ScreenW, f.ScreenH)
		if OverlayActorOK(f.Backend, HasScanBuf(a), covers, a.ScaledBuffer(), a.Width, a.Height) {
			out.Overlay = a
			out.NeedDRM = true
			if out.Reason == "compose" || out.Reason == "cursor" {
				out.Reason = "overlay"
			}
			break
		}
	}
	if out.Overlay == nil && !out.Cursor && out.Primary == nil {
		if VKDisplayCanOverlay(f.Backend, f.DisplayPlanes) {
			out.Reason = "vk-display extra planes (need drm master)"
		}
	}
	return out
}

func theaterAnimating(f PlaneFrame) bool {
	if f.Theater == 0 {
		return false
	}
	for _, a := range f.Actors {
		if a == nil {
			continue
		}
		if !a.VisualAt(f.Now, f.Theater).Identity() {
			return true
		}
	}
	return false
}
