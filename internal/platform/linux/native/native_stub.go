//go:build !linux || !cgo

// Package native is the cgo ABI boundary for Vulkan and DRM/KMS.
package native

import "fmt"

// ErrUnavailable is returned when this binary was not built with linux+cgo.
var ErrUnavailable = fmt.Errorf("native vulkan/drm unavailable (need linux + CGO + libvulkan-dev + libdrm-dev)")

func Available() bool { return false }

func ListDevices() (string, error) { return "", ErrUnavailable }

type VK struct{}

func OpenVK(display bool, w, h uint32) (*VK, error) { return nil, ErrUnavailable }
func OpenVKOnDRM(d *DRM) (*VK, error)               { return nil, ErrUnavailable }
func (v *VK) Close()                                {}
func (v *VK) DeviceName() string                    { return "" }
func (v *VK) Size() (uint32, uint32)                { return 0, 0 }
func (v *VK) VendorID() uint32                      { return 0 }
func (v *VK) ClearPresent(r, g, b, a float32) error { return ErrUnavailable }
func (v *VK) UploadPresent(bgra []byte, stride uint32) error {
	return ErrUnavailable
}
func (v *VK) HasDMABuf() bool    { return false }
func (v *VK) IsDisplay() bool    { return false }
func (v *VK) HasTimeline() bool  { return false }
func (v *VK) DisplayPlanes() int { return 0 }
func (v *VK) WaitTimeline(fd int, point uint64, timeoutNS uint64) error {
	return ErrUnavailable
}

type DMABufPlane struct {
	FD     int
	Offset uint32
	Stride uint32
}

type GPULayer struct {
	Slot int
	X, Y int
	W, H int
}

func (v *VK) ImportDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) ([]byte, int, error) {
	return nil, 0, ErrUnavailable
}

func (v *VK) RetainDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) (int, error) {
	return 0, ErrUnavailable
}

func (v *VK) ReleaseDMABuf(slot int) {}

func (v *VK) UploadPresentLayers(bgra []byte, stride uint32, layers []GPULayer) error {
	return ErrUnavailable
}

func (v *VK) HeadlessClear(r, g, b, a float32) (uint32, error) { return 0, ErrUnavailable }

type DRM struct{}

func OpenDRM(card string) (*DRM, error)       { return nil, ErrUnavailable }
func OpenDRMPlanes(card string) (*DRM, error) { return nil, ErrUnavailable }
func (d *DRM) PlanesOnly() bool               { return false }
func (d *DRM) Close()                         {}
func (d *DRM) Card() string                   { return "" }
func (d *DRM) Size() (uint32, uint32, uint32) {
	return 0, 0, 0
}
func (d *DRM) PresentBGRA(bgra []byte, stride uint32) error { return ErrUnavailable }
func (d *DRM) ScanoutDMABuf(fd int, width, height, fourcc uint32, modifier uint64, offset, pitch uint32) error {
	return ErrUnavailable
}
func (d *DRM) RestoreScanout() error { return ErrUnavailable }
func (d *DRM) ScanoutActive() bool   { return false }
func (d *DRM) PlaneCaps() (bool, bool, uint32, uint32) {
	return false, false, 0, 0
}
func (d *DRM) OverlayDMABuf(fd int, width, height, fourcc uint32, modifier uint64, offset, pitch uint32, x, y int) error {
	return ErrUnavailable
}
func (d *DRM) OverlayDisable() error { return ErrUnavailable }
func (d *DRM) CursorARGB(x, y int, width, height uint32, bgra []byte, stride uint32) error {
	return ErrUnavailable
}
func (d *DRM) CursorDisable() error { return ErrUnavailable }
