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
func (v *VK) Close()                                {}
func (v *VK) DeviceName() string                    { return "" }
func (v *VK) Size() (uint32, uint32)                { return 0, 0 }
func (v *VK) VendorID() uint32                      { return 0 }
func (v *VK) ClearPresent(r, g, b, a float32) error { return ErrUnavailable }
func (v *VK) UploadPresent(bgra []byte, stride uint32) error {
	return ErrUnavailable
}
func (v *VK) HeadlessClear(r, g, b, a float32) (uint32, error) { return 0, ErrUnavailable }

type DRM struct{}

func OpenDRM(card string) (*DRM, error) { return nil, ErrUnavailable }
func (d *DRM) Close()                   {}
func (d *DRM) Card() string             { return "" }
func (d *DRM) Size() (uint32, uint32, uint32) {
	return 0, 0, 0
}
func (d *DRM) PresentBGRA(bgra []byte, stride uint32) error { return ErrUnavailable }
