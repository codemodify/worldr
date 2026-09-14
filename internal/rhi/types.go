package rhi

// Format is a pixel format. Concrete Vulkan mappings arrive with the backend.
type Format uint32

const (
	FormatUnknown Format = iota
	FormatBGRA8Unorm
	FormatRGBA8Unorm
)

// TextureDesc describes a texture to allocate on a Device.
type TextureDesc struct {
	Width  uint32
	Height uint32
	Format Format
}

// SharedImageDesc describes a DMA-BUF (or equivalent) import. Fields TBD.
type SharedImageDesc struct {
	Width  uint32
	Height uint32
	Format Format
}

// SyncDesc describes an explicit-sync object. Fields TBD.
type SyncDesc struct{}

// Work is a placeholder for a recorded GPU submission.
// Later: command buffers / render graphs, still allocation-disciplined.
type Work struct{}

// Frame is a placeholder present payload.
type Frame struct {
	Image Texture
}
