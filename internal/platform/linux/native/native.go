//go:build linux && cgo

// Package native is the cgo ABI boundary for Vulkan and DRM/KMS.
//
// Binding choice: a thin owned C wrapper around libvulkan + libdrm, not
// lukem570/vulkan-go (no VK_KHR_display) and not wgpu. See ARCHITECTURE.md.
package native

/*
#cgo pkg-config: vulkan libdrm
#include "vk_session.h"
#include "drm_session.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

const errBuf = 512

func cErr(buf []C.char) error {
	s := C.GoString(&buf[0])
	if s == "" {
		return fmt.Errorf("native: unknown error")
	}
	return fmt.Errorf("%s", s)
}

// Available is true when this binary was built with linux+cgo.
func Available() bool { return true }

// ListDevices returns Vulkan physical devices (no display takeover).
func ListDevices() (string, error) {
	out := make([]C.char, 4096)
	errb := make([]C.char, errBuf)
	if C.worldr_vk_list_devices(&out[0], C.int(len(out)), &errb[0], C.int(len(errb))) != 0 {
		return "", cErr(errb)
	}
	return C.GoString(&out[0]), nil
}

// VK is a Vulkan session (headless or VK_KHR_display).
type VK struct {
	ptr *C.worldr_vk
}

// OpenVK creates a Vulkan session. mode: 0 headless, 1 vk-display.
func OpenVK(display bool, w, h uint32) (*VK, error) {
	mode := C.int(0)
	if display {
		mode = 1
	}
	errb := make([]C.char, errBuf)
	var ptr *C.worldr_vk
	if C.worldr_vk_create(mode, C.uint32_t(w), C.uint32_t(h), &ptr, &errb[0], C.int(len(errb))) != 0 {
		return nil, cErr(errb)
	}
	return &VK{ptr: ptr}, nil
}

func (v *VK) Close() {
	if v == nil || v.ptr == nil {
		return
	}
	C.worldr_vk_destroy(v.ptr)
	v.ptr = nil
}

func (v *VK) DeviceName() string {
	if v == nil || v.ptr == nil {
		return ""
	}
	return C.GoString(C.worldr_vk_device_name(v.ptr))
}

func (v *VK) Size() (w, h uint32) {
	if v == nil || v.ptr == nil {
		return 0, 0
	}
	return uint32(C.worldr_vk_width(v.ptr)), uint32(C.worldr_vk_height(v.ptr))
}

func (v *VK) VendorID() uint32 {
	if v == nil || v.ptr == nil {
		return 0
	}
	return uint32(C.worldr_vk_vendor_id(v.ptr))
}

func (v *VK) ClearPresent(r, g, b, a float32) error {
	errb := make([]C.char, errBuf)
	if C.worldr_vk_clear_present(v.ptr, C.float(r), C.float(g), C.float(b), C.float(a), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

func (v *VK) UploadPresent(bgra []byte, stride uint32) error {
	if len(bgra) == 0 {
		return fmt.Errorf("empty framebuffer")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_vk_upload_present(v.ptr, (*C.uint8_t)(unsafe.Pointer(&bgra[0])), C.uint32_t(stride), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// HasDMABuf is true when the device can import client dma-bufs via Vulkan.
func (v *VK) HasDMABuf() bool {
	return v != nil && v.ptr != nil && C.worldr_vk_has_dmabuf(v.ptr) != 0
}

// IsDisplay is true for a VK_KHR_display session (GPU overlay present).
func (v *VK) IsDisplay() bool {
	return v != nil && v.ptr != nil && C.worldr_vk_is_display(v.ptr) != 0
}

// GPULayer is one retained dmabuf blit onto the swapchain (1-based slot).
type GPULayer struct {
	Slot int
	X, Y int
	W, H int
}

// DMABufPlane is one linux-dmabuf plane (compositor owns the fd until Import returns).
type DMABufPlane struct {
	FD     int
	Offset uint32
	Stride uint32
}

// ImportDMABuf copies a client dma-buf into host BGRA (GPU import + linear readback).
func (v *VK) ImportDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) ([]byte, int, error) {
	if v == nil || v.ptr == nil {
		return nil, 0, fmt.Errorf("vulkan session closed")
	}
	if len(planes) == 0 || len(planes) > 4 {
		return nil, 0, fmt.Errorf("dmabuf plane count %d", len(planes))
	}
	fds := make([]C.int, len(planes))
	offs := make([]C.uint32_t, len(planes))
	pits := make([]C.uint32_t, len(planes))
	for i, p := range planes {
		fds[i] = C.int(p.FD)
		offs[i] = C.uint32_t(p.Offset)
		pits[i] = C.uint32_t(p.Stride)
	}
	stride := int(width * 4)
	out := make([]byte, stride*int(height))
	errb := make([]C.char, errBuf)
	if C.worldr_vk_dmabuf_import(v.ptr, C.uint32_t(width), C.uint32_t(height), C.uint32_t(fourcc), C.uint64_t(modifier),
		C.int(len(planes)), &fds[0], &offs[0], &pits[0], (*C.uint8_t)(unsafe.Pointer(&out[0])), C.uint32_t(stride),
		&errb[0], C.int(len(errb))) != 0 {
		return nil, 0, cErr(errb)
	}
	return out, stride, nil
}

// RetainDMABuf imports a client dma-buf as a GPU image and returns a 1-based slot.
func (v *VK) RetainDMABuf(width, height, fourcc uint32, modifier uint64, planes []DMABufPlane) (int, error) {
	if v == nil || v.ptr == nil {
		return 0, fmt.Errorf("vulkan session closed")
	}
	if len(planes) == 0 || len(planes) > 4 {
		return 0, fmt.Errorf("dmabuf plane count %d", len(planes))
	}
	fds := make([]C.int, len(planes))
	offs := make([]C.uint32_t, len(planes))
	pits := make([]C.uint32_t, len(planes))
	for i, p := range planes {
		fds[i] = C.int(p.FD)
		offs[i] = C.uint32_t(p.Offset)
		pits[i] = C.uint32_t(p.Stride)
	}
	errb := make([]C.char, errBuf)
	var slot C.int
	if C.worldr_vk_dmabuf_retain(v.ptr, C.uint32_t(width), C.uint32_t(height), C.uint32_t(fourcc), C.uint64_t(modifier),
		C.int(len(planes)), &fds[0], &offs[0], &pits[0], &slot, &errb[0], C.int(len(errb))) != 0 {
		return 0, cErr(errb)
	}
	return int(slot), nil
}

// ReleaseDMABuf drops a retained GPU slot (0 is a no-op).
func (v *VK) ReleaseDMABuf(slot int) {
	if v == nil || v.ptr == nil || slot <= 0 {
		return
	}
	C.worldr_vk_dmabuf_release(v.ptr, C.int(slot))
}

// UploadPresentLayers uploads the CPU desktop then blits retained dmabuf layers.
func (v *VK) UploadPresentLayers(bgra []byte, stride uint32, layers []GPULayer) error {
	if len(layers) == 0 {
		return v.UploadPresent(bgra, stride)
	}
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	if len(bgra) == 0 {
		return fmt.Errorf("empty framebuffer")
	}
	cl := make([]C.worldr_vk_layer, len(layers))
	for i, l := range layers {
		cl[i] = C.worldr_vk_layer{
			slot: C.int(l.Slot),
			x:    C.int32_t(l.X),
			y:    C.int32_t(l.Y),
			w:    C.int32_t(l.W),
			h:    C.int32_t(l.H),
		}
	}
	errb := make([]C.char, errBuf)
	if C.worldr_vk_upload_present_layers(v.ptr, (*C.uint8_t)(unsafe.Pointer(&bgra[0])), C.uint32_t(stride),
		&cl[0], C.int(len(cl)), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

func (v *VK) HeadlessClear(r, g, b, a float32) (pixel uint32, err error) {
	errb := make([]C.char, errBuf)
	var p C.uint32_t
	if C.worldr_vk_headless_clear(v.ptr, C.float(r), C.float(g), C.float(b), C.float(a), &p, &errb[0], C.int(len(errb))) != 0 {
		return 0, cErr(errb)
	}
	return uint32(p), nil
}

// DRM is a DRM/KMS dumb-buffer session.
type DRM struct {
	ptr *C.worldr_drm
}

func OpenDRM(card string) (*DRM, error) {
	errb := make([]C.char, errBuf)
	var ptr *C.worldr_drm
	var ccard *C.char
	if card != "" {
		ccard = C.CString(card)
		defer C.free(unsafe.Pointer(ccard))
	}
	if C.worldr_drm_create(ccard, &ptr, &errb[0], C.int(len(errb))) != 0 {
		return nil, cErr(errb)
	}
	return &DRM{ptr: ptr}, nil
}

func (d *DRM) Close() {
	if d == nil || d.ptr == nil {
		return
	}
	C.worldr_drm_destroy(d.ptr)
	d.ptr = nil
}

func (d *DRM) Card() string {
	if d == nil || d.ptr == nil {
		return ""
	}
	return C.GoString(C.worldr_drm_card(d.ptr))
}

func (d *DRM) Size() (w, h, stride uint32) {
	if d == nil || d.ptr == nil {
		return 0, 0, 0
	}
	return uint32(C.worldr_drm_width(d.ptr)), uint32(C.worldr_drm_height(d.ptr)), uint32(C.worldr_drm_stride(d.ptr))
}

func (d *DRM) PresentBGRA(bgra []byte, stride uint32) error {
	if len(bgra) == 0 {
		return fmt.Errorf("empty framebuffer")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_present_bgra(d.ptr, (*C.uint8_t)(unsafe.Pointer(&bgra[0])), C.uint32_t(stride), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// SummarizeDevices is a one-line helper for logs.
func SummarizeDevices(listing string) string {
	return strings.TrimSpace(listing)
}
