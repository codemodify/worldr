// Package apps hosts ordinary Wayland applications on a private local socket.
// It supplies SHM snapshots or retained GPU layers; workspace policy lives elsewhere.
// A Server and all its methods belong to a single goroutine.
package apps

import (
	"errors"
	"github.com/codemodify/worldr/internal/render"
)

var ErrClosed = errors.New("application host is closed")

// ErrX11SurfacePending means the WM association arrived before its Wayland
// surface. The managed XWM may retry after the next server dispatch.
var ErrX11SurfacePending = errors.New("X11 Wayland surface is not yet available")

// Surface is a mapped xdg_toplevel or explicitly associated X11 root. Pixels are tightly packed RGBA, with opaque
// alpha, and remain immutable after Poll returns. Logical dimensions describe
// the coordinate space used by Pointer and Resize; pixels can have a higher scale.
type Surface struct {
	ID                          uint64
	Title                       string
	AppID                       string
	PID                         uint32
	Width, Height               int
	LogicalWidth, LogicalHeight int
	Revision                    uint64
	Pixels                      []byte
	// Texture and Layers are present for a GPU-backed surface tree. Layers
	// include the root and are ordered back to front. Pixels remains the usual
	// flattened SHM snapshot when Layers is empty. All textures are immutable.
	Texture *render.Texture
	Layers  []Layer
}

// Layer is clipped to the root's logical rectangle. X/Y are top-left logical
// coordinates; UV is normalized [left,top,right,bottom] in the original texture.
// Root identifies the surface used for input. RGB is premultiplied by alpha.
// Opaque is true only for an XRGB buffer format, whose alpha is always one.
type Layer struct {
	Token               uint64
	Texture             *render.Texture
	Root                bool
	Opaque              bool
	X, Y, Width, Height float32
	UV                  [4]float32
}

// Cursor is the current pointer client's cursor choice. Set=false requests the
// workspace default; Set=true and Hidden=true explicitly hides the pointer.
// SurfaceID identifies the pointer-focused root toplevel, including while a
// popup or subsurface owns pointer focus. Pixels are immutable tightly packed
// premultiplied RGBA; dimensions are buffer pixels, hotspot is surface-local,
// and LogicalWidth/Height include viewport scaling; Scale is the buffer scale.
// Normal cursor images are at most 512x512 pixels; DragIcon images may reach4096.
// Revision also changes for hotspot, visibility and focus changes.
// ImageID/ImageRevision identify retained pixels independently of hotspot changes.
type Cursor struct {
	ImageID, ImageRevision                     uint64
	SurfaceID, Revision                        uint64
	Set, Hidden                                bool
	DragIcon                                   bool
	Width, Height, LogicalWidth, LogicalHeight int
	Scale, HotspotX, HotspotY                  int
	Pixels                                     []byte
}

// ClipboardOffer describes the current selection without reading its contents.
// ExternalID is nonzero for a selection supplied through OfferClipboard.
type ClipboardOffer struct {
	Revision   uint64
	ID         uint64
	ExternalID uint64
	MIMEs      []string
}

// ClipboardRequest transfers ownership of FD to the caller, which must relay or
// close it. ExternalID identifies the source passed to OfferClipboard.
type ClipboardRequest struct {
	ExternalID uint64
	MIME       string
	FD         int
}
