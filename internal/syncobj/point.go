// Package syncobj is the linux-drm-syncobj helper (timeline points + DRM
// ioctl plumbing). Vulkan GPU wait is a later TODO; callers must keep
// implicit-sync fallback when Wait/Signal fail.
package syncobj

// Point packs wp_linux_drm_syncobj_surface_v1 point_hi/lo.
func Point(hi, lo uint32) uint64 {
	return uint64(hi)<<32 | uint64(lo)
}

// SplitPoint is the inverse of Point.
func SplitPoint(p uint64) (hi, lo uint32) {
	return uint32(p >> 32), uint32(p)
}

// Fence is one acquire or release timeline attachment.
type Fence struct {
	FD    int
	Point uint64
}

// Valid reports a usable imported syncobj fd.
func (f Fence) Valid() bool {
	return f.FD > 0
}

// CloseFD closes a borrowed fence fd (0/-1 is a no-op).
func (f *Fence) CloseFD() {
	if f == nil || f.FD <= 0 {
		return
	}
	_ = closeFD(f.FD)
	f.FD = -1
}
