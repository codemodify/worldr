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

// OpenVKOnDRM creates VK_KHR_display on a planes-only DRM master via
// VK_EXT_acquire_drm_display so overlay/cursor ioctls share the same fd.
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

// HasTimeline is true when the device can import a DRM syncobj as a Vulkan timeline.
func (v *VK) HasTimeline() bool {
	return v != nil && v.ptr != nil && C.worldr_vk_has_timeline(v.ptr) != 0
}

// DisplayPlanes is the VK_KHR_display plane count (0 if unknown / not display).
func (v *VK) DisplayPlanes() int {
	if v == nil || v.ptr == nil {
		return 0
	}
	return int(C.worldr_vk_display_planes(v.ptr))
}

// WaitTimeline imports fd as a timeline semaphore and vkWaitSemaphores(point).
func (v *VK) WaitTimeline(fd int, point uint64, timeoutNS uint64) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	if fd < 0 {
		return fmt.Errorf("syncobj fd")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_vk_wait_timeline_fd(v.ptr, C.int(fd), C.uint64_t(point), C.uint64_t(timeoutNS),
		&errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
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

// ScanoutDMABuf presents a client dmabuf on the primary plane (atomic, then SetCrtc).
func (d *DRM) ScanoutDMABuf(fd int, width, height, fourcc uint32, modifier uint64, offset, pitch uint32) error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	if d.planesOnly {
		return fmt.Errorf("planes-only DRM (vk-display sidecar); no primary scanout")
	}
	if fd < 0 {
		return fmt.Errorf("dmabuf fd")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_scanout_dmabuf(d.ptr, C.int(fd), C.uint32_t(width), C.uint32_t(height),
		C.uint32_t(fourcc), C.uint64_t(modifier), C.uint32_t(offset), C.uint32_t(pitch),
		&errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// RestoreScanout puts the dumb-buffer FB back on the CRTC.
func (d *DRM) RestoreScanout() error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_scanout_restore(d.ptr, &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// ScanoutActive is true while a client dmabuf is on the primary plane.
func (d *DRM) ScanoutActive() bool {
	return d != nil && d.ptr != nil && C.worldr_drm_scanout_active(d.ptr) != 0
}

// PlaneCaps reports whether the card has an overlay and/or cursor plane.
func (d *DRM) PlaneCaps() (overlay, cursor bool, cursorW, cursorH uint32) {
	if d == nil || d.ptr == nil {
		return false, false, 0, 0
	}
	var ov, cu C.int
	var cw, ch C.uint32_t
	if C.worldr_drm_plane_caps(d.ptr, &ov, &cu, &cw, &ch) != 0 {
		return false, false, 0, 0
	}
	return ov != 0, cu != 0, uint32(cw), uint32(ch)
}

// OverlayDMABuf places a windowed client dmabuf on an overlay plane.
func (d *DRM) OverlayDMABuf(fd int, width, height, fourcc uint32, modifier uint64, offset, pitch uint32, x, y int) error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	if fd < 0 {
		return fmt.Errorf("dmabuf fd")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_overlay_dmabuf(d.ptr, C.int(fd), C.uint32_t(width), C.uint32_t(height),
		C.uint32_t(fourcc), C.uint64_t(modifier), C.uint32_t(offset), C.uint32_t(pitch),
		C.int32_t(x), C.int32_t(y), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// OverlayDisable turns the overlay plane off (compose fallback).
func (d *DRM) OverlayDisable() error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_overlay_disable(d.ptr, &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// CursorARGB commits a software cursor image onto the hardware cursor plane.
func (d *DRM) CursorARGB(x, y int, width, height uint32, bgra []byte, stride uint32) error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	if len(bgra) == 0 {
		return fmt.Errorf("empty cursor")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_cursor_argb(d.ptr, C.int32_t(x), C.int32_t(y), C.uint32_t(width), C.uint32_t(height),
		(*C.uint8_t)(unsafe.Pointer(&bgra[0])), C.uint32_t(stride), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// CursorDisable turns the hardware cursor plane off.
func (d *DRM) CursorDisable() error {
	if d == nil || d.ptr == nil {
		return fmt.Errorf("drm session closed")
	}
	errb := make([]C.char, errBuf)
	if C.worldr_drm_cursor_disable(d.ptr, &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb)
	}
	return nil
}

// SummarizeDevices is a one-line helper for logs.
func SummarizeDevices(listing string) string {
	return strings.TrimSpace(listing)
}
