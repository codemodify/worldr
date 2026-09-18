//go:build !linux || !cgo

// Package native is the cgo ABI boundary for Vulkan and DRM/KMS.
package native

import (
	"fmt"
	"unsafe"
)

// ErrUnavailable is returned when this binary was not built with linux+cgo.
var ErrUnavailable = fmt.Errorf("native vulkan/drm unavailable (need linux + CGO + libvulkan-dev + libdrm-dev)")

func Available() bool { return false }

func ListDevices() (string, error) { return "", ErrUnavailable }

type VK struct{}

func OpenVK(display bool, w, h uint32) (*VK, error) { return nil, ErrUnavailable }
func OpenVKOnDRM(d *DRM) (*VK, error)               { return nil, ErrUnavailable }
func (v *VK) Close()                                {}
func (v *VK) DeviceName() string                    { return "" }
func (v *VK) RenderNode() string                    { return "" }
func (v *VK) Size() (uint32, uint32)                { return 0, 0 }
func (v *VK) IsDisplay() bool                       { return false }

type DRM struct{ planesOnly bool }

func OpenDRM(card string) (*DRM, error)       { return nil, ErrUnavailable }
func OpenDRMPlanes(card string) (*DRM, error) { return nil, ErrUnavailable }
func (d *DRM) PlanesOnly() bool               { return d != nil && d.planesOnly }
func (d *DRM) Close()                         {}
func (d *DRM) Card() string                   { return "" }
func (d *DRM) Size() (uint32, uint32, uint32) {
	return 0, 0, 0
}
func (d *DRM) PresentBGRA(bgra []byte, stride uint32) error { return ErrUnavailable }
func OpenVKWayland(display, surface unsafe.Pointer, w, h uint32) (*VK, error) {
	return nil, ErrUnavailable
}
func (v *VK) Resize(w, h uint32) error { return ErrUnavailable }

func (v *VK) SetMemoryBudget(bytes uint64) error { return ErrUnavailable }
func (v *VK) MemoryStats() MemoryStats           { return MemoryStats{} }
func (v *VK) Recover() error                     { return ErrUnavailable }
