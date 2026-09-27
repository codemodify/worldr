package host

// Options describes native window behavior. The zero value preserves the
// compositor host's existing opaque, renderer-owned cursor behavior.
type Options struct {
	Fullscreen      bool
	ClientDecorated bool
	Transparent     bool
	SystemCursor    bool
}

// ResizeEdge identifies an edge of a client-decorated window.
type ResizeEdge uint32

const (
	ResizeTop         ResizeEdge = 1
	ResizeBottom      ResizeEdge = 2
	ResizeLeft        ResizeEdge = 4
	ResizeTopLeft     ResizeEdge = 5
	ResizeBottomLeft  ResizeEdge = 6
	ResizeRight       ResizeEdge = 8
	ResizeTopRight    ResizeEdge = 9
	ResizeBottomRight ResizeEdge = 10
)
