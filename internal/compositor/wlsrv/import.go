package wlsrv

// DMABufPlane is one linux-dmabuf plane. The compositor owns the fd.
type DMABufPlane struct {
	FD     int
	Offset uint32
	Stride uint32
}

// DMABufImport turns a client gpu buffer into CPU BGRA for the scene blit.
// LINEAR buffers can be mmap'd without this; tiled Intel modifiers need Vulkan.
type DMABufImport interface {
	ImportDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) (bgra []byte, stride int, err error)
}

// DMABufGPU optionally retains a Vulkan image for compositor-pass sampling.
// Nested/drm present still uses ImportDMABuf / LINEAR mmap (CPU fallback).
type DMABufGPU interface {
	RetainDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) (slot int, err error)
	ReleaseDMABuf(slot int)
	CanGPUComposite() bool
}
