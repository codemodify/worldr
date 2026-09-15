// Package scanout decides when a client dmabuf can be shown via KMS
// primary-plane commit instead of the CPU desktop upload / Vulkan blit.
//
// Primary-fullscreen path: one opaque ARGB/XRGB buffer that covers the
// output, then drmModeAtomicCommit (or SetCrtc fallback). Overlay / cursor
// assignment lives in planes.go. Intel Arrow Lake is first; NVIDIA/AMD are
// best-effort (same helpers, AddFB2 may fail).
package scanout

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

// DRM fourcc values that KMS primary planes commonly accept.
const (
	FourccXRGB8888 = 0x34325258
	FourccARGB8888 = 0x34325241
	ModLinear      = 0
	ModInvalid     = 0x00ffffffffffffff
)

// PCI vendor ids. Scanout is written against Intel; others still try.
const (
	VendorIntel  = 0x8086
	VendorNVIDIA = 0x10de
	VendorAMD    = 0x1002
)

// Frame is one present-loop snapshot for eligibility.
type Frame struct {
	Backend  string
	ScreenW  int
	ScreenH  int
	Actors   []*engine.Actor
	WS       engine.WorkspaceDraw
	Overview bool
	Launcher bool
	Theater  engine.Tier
	Now      time.Time
	VendorID uint32
}

// Result is the eligibility decision. Actor is set only when OK.
type Result struct {
	OK         bool
	Reason     string
	Actor      *engine.Actor
	IntelFirst bool
}

// KMSBackend is true for the real-display paths that can own a CRTC.
// Nested / headless / shm-only present never scan out.
func KMSBackend(name string) bool {
	return name == "vk-display" || name == "drm"
}

// ScanoutFourcc is the 8888 pair KMS primary + our GPU blit already share.
func ScanoutFourcc(fourcc uint32) bool {
	return fourcc == FourccARGB8888 || fourcc == FourccXRGB8888
}

// LinearMod is LINEAR / implicit / invalid (driver picks).
func LinearMod(mod uint64) bool {
	return mod == ModLinear || mod == 0 || mod == ModInvalid
}

// CoversOutput is a 1:1 fullscreen (or primary) client: origin + size match
// the CRTC and the buffer is not viewport-scaled.
func CoversOutput(a *engine.Actor, screenW, screenH int) bool {
	if a == nil || screenW <= 0 || screenH <= 0 {
		return false
	}
	if a.ScaledBuffer() {
		return false
	}
	bw, bh := a.PixelSize()
	if bw != screenW || bh != screenH {
		return false
	}
	return a.X == 0 && a.Y == 0 && a.Width == screenW && a.Height == screenH
}

// Sliding is a workspace animation (not settled on Active).
func Sliding(ws engine.WorkspaceDraw) bool {
	return ws.T < 1 && ws.From != ws.To && ws.Dir != 0
}

// HasScanBuf reports a live dmabuf fd the KMS path can import.
func HasScanBuf(a *engine.Actor) bool {
	return a != nil && a.ScanFD > 0 && ScanoutFourcc(a.ScanFourcc)
}

// Evaluate returns OK when a single visible client can take the primary
// plane. Callers must still try the atomic commit and fall back to blit
// when the kernel rejects the FB (modifier, master, NVIDIA/AMD).
func Evaluate(f Frame) Result {
	intel := f.VendorID == 0 || f.VendorID == VendorIntel
	fail := func(reason string) Result {
		return Result{Reason: reason, IntelFirst: intel}
	}
	if !KMSBackend(f.Backend) {
		if f.Backend == "wayland-client" || f.Backend == "nested" {
			return fail("nested present")
		}
		if f.Backend == "headless" {
			return fail("headless present")
		}
		return fail("not kms backend")
	}
	if f.Overview {
		return fail("overview")
	}
	if f.Launcher {
		return fail("launcher")
	}
	if Sliding(f.WS) {
		return fail("workspace slide")
	}
	if f.ScreenW <= 0 || f.ScreenH <= 0 {
		return fail("no output")
	}

	var (
		n         int
		full      *engine.Actor
		animating bool
	)
	for _, a := range f.Actors {
		if a == nil {
			continue
		}
		ox, show := f.WS.OffsetFor(a.Workspace, f.ScreenW)
		if !show {
			continue
		}
		v := a.VisualAt(f.Now, f.Theater)
		if v.Gone {
			continue
		}
		if !v.Identity() || ox != 0 {
			animating = true
		}
		n++
		if CoversOutput(a, f.ScreenW, f.ScreenH) && HasScanBuf(a) {
			full = a
		}
	}
	if animating {
		return fail("theater")
	}
	if n != 1 {
		return fail("not single visible")
	}
	if full == nil {
		return fail("no fullscreen dmabuf")
	}
	if !ScanoutFourcc(full.ScanFourcc) {
		return fail("bad fourcc")
	}
	if full.ScanFD <= 0 {
		return fail("no scan fd")
	}
	if full.ScaledBuffer() {
		return fail("scaled")
	}
	return Result{OK: true, Reason: "ok", Actor: full, IntelFirst: intel}
}
