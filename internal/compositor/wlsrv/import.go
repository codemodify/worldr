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
