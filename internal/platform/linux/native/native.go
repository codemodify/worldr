//go:build linux && cgo

// Package native is the cgo ABI boundary for Vulkan and DRM/KMS.
//
// A thin owned C wrapper exposes Vulkan graphics, presentation and DRM device
// ownership. See ARCHITECTURE.md.
package native

/*
#cgo LDFLAGS: -lm
#cgo pkg-config: vulkan libdrm
#include "vk_session.h"
#include "drm_session.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"path/filepath"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/sys/unix"
	"unsafe"
)

const errBuf = 512

func cErr(buf []C.char) error {
	s := C.GoString(&buf[0])
	if s == "" {
		return fmt.Errorf("native: unknown error")
	}
	return nativeError(s)
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

// VK owns a Vulkan device and its headless, Wayland, or direct display target.
type VK struct {
	ptr                *C.worldr_vk
	frame              *frameState
	atlas              render.Atlas
	dmabufFormats      []dmabuf.Format
	dmabufFormatsReady bool
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

// OpenVKOnDRM creates VK_KHR_display using an already acquired DRM master
// and the connector selected by that session.
func OpenVKOnDRM(d *DRM) (*VK, error) {
	if d == nil || d.ptr == nil {
		return nil, fmt.Errorf("drm session closed")
	}
	fd := C.worldr_drm_fd(d.ptr)
	conn := C.worldr_drm_connector_id(d.ptr)
	if fd < 0 || conn == 0 {
		return nil, fmt.Errorf("planes-only DRM missing fd/connector")
	}
	errb := make([]C.char, errBuf)
	var ptr *C.worldr_vk
	if C.worldr_vk_create_on_drm(fd, conn, 0, 0, &ptr, &errb[0], C.int(len(errb))) != 0 {
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
	v.frame = nil
	v.atlas = render.Atlas{}
	v.dmabufFormats = nil
	v.dmabufFormatsReady = false
}

func (v *VK) DeviceName() string {
	if v == nil || v.ptr == nil {
		return ""
	}
	return C.GoString(C.worldr_vk_device_name(v.ptr))
}

// RenderNode returns the DRM render node for the selected physical device.
// An empty result means the driver did not expose VK_EXT_physical_device_drm.
func (v *VK) RenderNode() string {
	if v == nil || v.ptr == nil {
		return ""
	}
	var major, minor C.uint32_t
	if C.worldr_vk_render_node(v.ptr, &major, &minor) != 0 {
		return ""
	}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	for _, path := range nodes {
		var stat unix.Stat_t
		if unix.Stat(path, &stat) != nil {
			continue
		}
		if unix.Major(uint64(stat.Rdev)) == uint32(major) &&
			unix.Minor(uint64(stat.Rdev)) == uint32(minor) {
			return path
		}
	}
	return ""
}

func (v *VK) Size() (w, h uint32) {
	if v == nil || v.ptr == nil {
		return 0, 0
	}
	return uint32(C.worldr_vk_width(v.ptr)), uint32(C.worldr_vk_height(v.ptr))
}

// IsDisplay reports whether rendering presents to a Vulkan swapchain.
func (v *VK) IsDisplay() bool {
	return v != nil && v.ptr != nil && C.worldr_vk_is_display(v.ptr) != 0
}

// DRM is a DRM/KMS dumb-buffer session.
type DRM struct {
	ptr        *C.worldr_drm
	planesOnly bool
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

// OpenDRMPlanes takes DRM master without modesetting the primary plane.
func OpenDRMPlanes(card string) (*DRM, error) {
	errb := make([]C.char, errBuf)
	var ptr *C.worldr_drm
	var ccard *C.char
	if card != "" {
		ccard = C.CString(card)
		defer C.free(unsafe.Pointer(ccard))
	}
	if C.worldr_drm_create_planes(ccard, &ptr, &errb[0], C.int(len(errb))) != 0 {
		return nil, cErr(errb)
	}
	return &DRM{ptr: ptr, planesOnly: true}, nil
}

func (d *DRM) PlanesOnly() bool {
	return d != nil && d.planesOnly
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

// OpenVKWayland presents directly into the host surface using Vulkan WSI.
// The host display and surface must outlive the returned session.
func OpenVKWayland(display, surface unsafe.Pointer, w, h uint32) (*VK, error) {
	errb := make([]C.char, errBuf)
	var ptr *C.worldr_vk
	if C.worldr_vk_create_wayland(display, surface, C.uint32_t(w), C.uint32_t(h), &ptr, &errb[0], C.int(len(errb))) != 0 {
		return nil, cErr(errb)
	}
	return &VK{ptr: ptr}, nil
}

// Resize rebuilds render targets while preserving the device, atlas and geometry.
func (v *VK) Resize(w, h uint32) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("Vulkan session closed")
	}
	errb := make([]C.char, errBuf)
	result := C.worldr_vk_resize(v.ptr, C.uint32_t(w), C.uint32_t(h), &errb[0], C.int(len(errb)))
	if result == -3 {
		return ErrNotReady
	}
	if result != 0 {
		return cErr(errb)
	}
	return nil
}

// SetMemoryBudget caps actual renderer-owned Vulkan allocations. Zero restores
// the 1GiB default. Lowering below current use fails without changing the budget.
func (v *VK) SetMemoryBudget(bytes uint64) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("Vulkan session closed")
	}
	var errb [errBuf]C.char
	if C.worldr_vk_memory_budget(v.ptr, C.uint64_t(bytes), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb[:])
	}
	return nil
}
func (v *VK) MemoryStats() MemoryStats {
	if v == nil || v.ptr == nil {
		return MemoryStats{}
	}
	var stats C.worldr_vk_memory_stats
	C.worldr_vk_memory_usage(v.ptr, &stats)
	return MemoryStats{AllocatedBytes: uint64(stats.allocated), PeakBytes: uint64(stats.peak), BudgetBytes: uint64(stats.budget), Images: uint32(stats.images), Buffers: uint32(stats.buffers)}
}

// Recover recreates the logical device and swapchain on the existing physical
// device/surface, invalidates residency caches, and restores the retained atlas.
// The next frame re-uploads retained geometry/textures. Call only on the render
// goroutine; the host must bound retries and keep saving work on failure. This
// does not reconnect a removed physical monitor or recreate a lost host surface.
// Vulkan device-idle/destroy calls have no portable timeout; a driver blocked
// inside them cannot be repaired by an in-process retry.
func (v *VK) Recover() error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("Vulkan session closed")
	}
	var errb [errBuf]C.char
	result := C.worldr_vk_recover(v.ptr, &errb[0], C.int(len(errb)))
	v.frame = nil
	v.dmabufFormats = nil
	v.dmabufFormatsReady = false
	if result == -3 {
		return ErrNotReady
	}
	if result != 0 {
		return cErr(errb[:])
	}
	if len(v.atlas.Pixels) > 0 {
		return v.SetSceneAtlas(v.atlas)
	}
	return nil
}
