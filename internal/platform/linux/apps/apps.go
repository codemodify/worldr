//go:build linux && cgo

package apps

/*
#cgo pkg-config: wayland-server xkbcommon
#cgo CFLAGS: -Dxdg_wm_base_interface=worldr_apps_xdg_wm_base_interface -Dxdg_surface_interface=worldr_apps_xdg_surface_interface -Dxdg_toplevel_interface=worldr_apps_xdg_toplevel_interface -Dxdg_popup_interface=worldr_apps_xdg_popup_interface -Dxdg_positioner_interface=worldr_apps_xdg_positioner_interface
#cgo CFLAGS: -Dwp_viewporter_interface=worldr_apps_wp_viewporter_interface -Dwp_viewport_interface=worldr_apps_wp_viewport_interface -Dwp_fractional_scale_manager_v1_interface=worldr_apps_wp_fractional_scale_manager_v1_interface -Dwp_fractional_scale_v1_interface=worldr_apps_wp_fractional_scale_v1_interface
#include "apps.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
	"math"
	"os"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"unsafe"
)

type Server struct {
	ptr              *C.worldr_apps
	dir, socket      string
	cache            map[uint64]Surface
	cursor           Cursor
	importer         func(dmabuf.Descriptor) (*render.Texture, error)
	importHandle     cgo.Handle
	dmabufFormats    []dmabuf.Format
	dmabufRenderNode string
	images           map[uint64]*render.Texture
	retired          []*render.Texture
}

func Open(width, height int) (*Server, error) {
	if !dimensions(width, height) {
		return nil, fmt.Errorf("application dimensions must be between 1 and 4096")
	}
	dir, err := os.MkdirTemp("", "worldr-apps-")
	if err != nil {
		return nil, err
	}
	s := &Server{dir: dir, socket: filepath.Join(dir, "wayland"), cache: make(map[uint64]Surface)}
	name := C.CString(s.socket)
	defer C.free(unsafe.Pointer(name))
	var msg [512]C.char
	if C.worldr_apps_open(name, C.int(width), C.int(height), &s.ptr, &msg[0], 512) != 0 {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("application host: %s", C.GoString(&msg[0]))
	}
	return s, nil
}

func dimensions(w, h int) bool { return w > 0 && h > 0 && w <= 4096 && h <= 4096 }
func (s *Server) Socket() string {
	if s == nil {
		return ""
	}
	return s.socket
}
func (s *Server) Close() error {
	if s == nil || s.ptr == nil {
		return nil
	}
	C.worldr_apps_close(s.ptr)
	if s.importHandle != 0 {
		s.importHandle.Delete()
		s.importHandle = 0
	}
	for _, texture := range s.images {
		s.retired = append(s.retired, texture)
	}
	s.images = nil
	s.dmabufFormats = nil
	s.dmabufRenderNode = ""
	s.ptr = nil
	s.cache = nil
	s.cursor = Cursor{}
	return os.RemoveAll(s.dir)
}

// RequestClose asks clients to exit. Poll must continue dispatching while the
// owner waits for them; Close remains the immediate, idempotent final cleanup.
func (s *Server) RequestClose() error {
	if err := s.valid(); err != nil {
		return err
	}
	C.worldr_apps_request_close(s.ptr)
	return nil
}

// CloseSurface asks one mapped toplevel to close. The client may decline the
// request or show a confirmation dialog; Poll must continue dispatching. A
// surface that has already disappeared is harmless.
func (s *Server) CloseSurface(id uint64) error {
	if err := s.valid(); err != nil {
		return err
	}
	C.worldr_apps_close_surface(s.ptr, C.uint64_t(id))
	return nil
}

func (s *Server) valid() error {
	if s == nil || s.ptr == nil {
		return ErrClosed
	}
	return nil
}

// Cursor observes committed cursor state without dispatching requests. Poll
// advances clients; this method also reflects focus cleared through Pointer.
func (s *Server) Cursor() Cursor {
	if s == nil || s.ptr == nil {
		return Cursor{}
	}
	var raw C.worldr_app_cursor
	C.worldr_apps_cursor(s.ptr, &raw)
	if s.cursor.Revision != uint64(raw.revision) || s.cursor.SurfaceID != uint64(raw.surface_id) {
		v := Cursor{SurfaceID: uint64(raw.surface_id), Revision: uint64(raw.revision), ImageID: uint64(raw.image_id), ImageRevision: uint64(raw.image_revision), Set: raw.set != 0, Hidden: raw.hidden != 0, Width: int(raw.width), Height: int(raw.height), Scale: int(raw.scale), HotspotX: int(raw.hotspot_x), HotspotY: int(raw.hotspot_y)}
		v.DragIcon = raw.drag_icon != 0
		if raw.pixels != nil && v.Width > 0 && v.Height > 0 && v.Scale > 0 {
			v.LogicalWidth, v.LogicalHeight = int(raw.logical_width), int(raw.logical_height)
			if v.ImageID == s.cursor.ImageID && v.ImageRevision == s.cursor.ImageRevision {
				v.Pixels = s.cursor.Pixels
			} else {
				v.Pixels = C.GoBytes(unsafe.Pointer(raw.pixels), C.int(v.Width*v.Height*4))
			}
		}
		s.cursor = v
	}
	return s.cursor
}

func (s *Server) Poll() ([]Surface, error) {
	if err := s.valid(); err != nil {
		return nil, err
	}
	var raw [32]C.worldr_app_surface
	var msg [512]C.char
	n := int(C.worldr_apps_poll(s.ptr, &raw[0], 32, &msg[0], 512))
	if n < 0 {
		return nil, fmt.Errorf("application host: %s", C.GoString(&msg[0]))
	}
	out := make([]Surface, 0, n)
	next := make(map[uint64]Surface, n)
	for i := 0; i < n; i++ {
		r := raw[i]
		id := uint64(r.id)
		revision := uint64(r.revision)
		v, ok := s.cache[id]
		if ok && v.Revision == revision {
			v.Title = C.GoString(r.title)
		} else if r.layered != 0 {
			v = Surface{ID: id, Title: C.GoString(r.title), Width: int(r.width), Height: int(r.height), LogicalWidth: int(r.logical_width), LogicalHeight: int(r.logical_height), Revision: revision}
			var err error
			v.Layers, err = s.surfaceLayers(id)
			if err != nil {
				return nil, err
			}
			for _, layer := range v.Layers {
				if layer.Root {
					v.Texture = layer.Texture
					break
				}
			}
		} else if !ok || v.Revision != revision || v.Layers != nil {
			v = Surface{ID: id, Title: C.GoString(r.title), Width: int(r.width), Height: int(r.height), LogicalWidth: int(r.logical_width), LogicalHeight: int(r.logical_height), Revision: revision, Pixels: C.GoBytes(unsafe.Pointer(r.pixels), C.int(r.width*r.height*4))}
		} else {
			v.Title = C.GoString(r.title)
		}
		v.AppID = C.GoString(r.app_id)
		v.PID = uint32(r.pid)
		next[id] = v
		out = append(out, v)
	}
	s.cache = next
	s.retireImages()
	return out, nil
}
func (s *Server) Focus(id uint64) error {
	if err := s.valid(); err != nil {
		return err
	}
	if C.worldr_apps_focus(s.ptr, C.uint64_t(id)) != 0 {
		return fmt.Errorf("unknown application surface %d", id)
	}
	return nil
}

// Pointer uses surface-local logical coordinates. ID zero clears pointer focus.
func (s *Server) Pointer(id uint64, x, y float32) error {
	if err := s.valid(); err != nil {
		return err
	}
	if math.IsNaN(float64(x)) || math.IsNaN(float64(y)) || math.IsInf(float64(x), 0) || math.IsInf(float64(y), 0) || math.Abs(float64(x)) > 1e6 || math.Abs(float64(y)) > 1e6 {
		return fmt.Errorf("invalid pointer coordinates")
	}
	if C.worldr_apps_pointer(s.ptr, C.uint64_t(id), C.float(x), C.float(y)) != 0 {
		return fmt.Errorf("unknown application surface %d", id)
	}
	return nil
}
func (s *Server) Button(code uint32, pressed bool, time uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	p := 0
	if pressed {
		p = 1
	}
	C.worldr_apps_button(s.ptr, C.uint32_t(code), C.int(p), C.uint32_t(time))
	return nil
}
func (s *Server) Axis(horizontal, vertical float32, time uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	if math.IsNaN(float64(horizontal)) || math.IsNaN(float64(vertical)) || math.IsInf(float64(horizontal), 0) || math.IsInf(float64(vertical), 0) || math.Abs(float64(horizontal)) > 1e6 || math.Abs(float64(vertical)) > 1e6 {
		return fmt.Errorf("invalid scroll axes")
	}
	C.worldr_apps_axis(s.ptr, C.float(horizontal), C.float(vertical), C.uint32_t(time))
	return nil
}
func (s *Server) Key(code uint32, pressed bool, time uint32, depressed, latched, locked, group uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	p := 0
	if pressed {
		p = 1
	}
	C.worldr_apps_key(s.ptr, C.uint32_t(code), C.int(p), C.uint32_t(time), C.uint32_t(depressed), C.uint32_t(latched), C.uint32_t(locked), C.uint32_t(group))
	return nil
}
func (s *Server) SetKeymap(keymap string) error {
	if err := s.valid(); err != nil {
		return err
	}
	if len(keymap) == 0 || len(keymap) > 1<<20 || strings.IndexByte(keymap, 0) >= 0 {
		return fmt.Errorf("invalid XKB keymap")
	}
	v := C.CString(keymap)
	defer C.free(unsafe.Pointer(v))
	if C.worldr_apps_keymap(s.ptr, v) != 0 {
		return fmt.Errorf("cannot install XKB keymap")
	}
	return nil
}
func (s *Server) Resize(id uint64, width, height int) error {
	if err := s.valid(); err != nil {
		return err
	}
	if !dimensions(width, height) {
		return fmt.Errorf("application dimensions must be between 1 and 4096")
	}
	if C.worldr_apps_resize(s.ptr, C.uint64_t(id), C.int(width), C.int(height)) != 0 {
		return fmt.Errorf("unknown application surface %d", id)
	}
	return nil
}
func (s *Server) Modifiers(depressed, latched, locked, group uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	C.worldr_apps_modifiers(s.ptr, C.uint32_t(depressed), C.uint32_t(latched), C.uint32_t(locked), C.uint32_t(group))
	return nil
}
func (s *Server) SetRepeat(rate, delay int32) error {
	if err := s.valid(); err != nil {
		return err
	}
	if rate < 0 || rate > 1000 || delay < 0 {
		return fmt.Errorf("invalid keyboard repeat settings")
	}
	C.worldr_apps_repeat(s.ptr, C.int32_t(rate), C.int32_t(delay))
	return nil
}

// DragActive reports a client-started, input-serial-validated drag gesture.
// During it Pointer targets the drop destination, including zero for outside.
func (s *Server) DragActive() bool {
	return s != nil && s.ptr != nil && C.worldr_apps_drag_active(s.ptr) != 0
}

// CancelDrag ends a gesture without a drop, including on host focus loss.
func (s *Server) CancelDrag() error {
	if err := s.valid(); err != nil {
		return err
	}
	C.worldr_apps_cancel_drag(s.ptr)
	return nil
}
