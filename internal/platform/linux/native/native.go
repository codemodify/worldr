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
