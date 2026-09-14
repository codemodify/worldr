// Package rhi is the owned render-hardware interface.
//
// Phase 0 defines interfaces only. A thin Vulkan backend lives in a later
// phase; cgo/FFI is allowed only at the Vulkan/OS ABI boundary. wgpu is not
// the foundation (optional tools only). The frame loop must stay
// allocation-disciplined for Go's GC.
package rhi

// Device is the GPU device owned by the shell compositor.
type Device interface {
	Name() string
	Queue() Queue
	CreateTexture(desc TextureDesc) (Texture, error)
	ImportSharedImage(desc SharedImageDesc) (SharedImage, error)
	CreateSync(desc SyncDesc) (Sync, error)
	Present() Present
	Close() error
}

// Queue submits GPU work. Implementations must avoid per-frame allocations.
type Queue interface {
	Submit(work ...Work) error
	WaitIdle() error
}

// Texture is a device-local image used as a render or sample target.
type Texture interface {
	Width() uint32
	Height() uint32
	Format() Format
	Close() error
}

// SharedImage is a cross-process image (DMA-BUF import/export later)
// used so isolated experiences/apps can share pixels with the compositor.
type SharedImage interface {
	Texture() Texture
	Close() error
}

// Sync is an explicit-sync primitive (Vulkan timeline / drm syncobj later).
type Sync interface {
	Close() error
}

// Present displays a frame on a KMS plane / output.
type Present interface {
	Frame(frame Frame) error
}
