//go:build linux && cgo

package apps

/*
#include "apps.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

func x11Labels(title, appID string) error {
	if len(title) > 4096 || len(appID) > 4096 || strings.IndexByte(title, 0) >= 0 || strings.IndexByte(appID, 0) >= 0 {
		return fmt.Errorf("invalid X11 window metadata")
	}
	return nil
}

// AssociateX11 promotes a raw wl_surface only after the managed XWM identifies
// its peer PID and exact Wayland object ID. No image-size guessing is involved.
func (s *Server) AssociateX11(pid, objectID, window uint32, title, appID string) (uint64, error) {
	if err := s.valid(); err != nil {
		return 0, err
	}
	if pid == 0 || objectID == 0 || window == 0 {
		return 0, fmt.Errorf("X11 association requires peer PID, object ID and window ID")
	}
	if err := x11Labels(title, appID); err != nil {
		return 0, err
	}
	name, app := C.CString(title), C.CString(appID)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(app))
	var id C.uint64_t
	switch code := C.worldr_apps_associate_x11(s.ptr, C.uint32_t(pid), C.uint32_t(objectID), C.uint32_t(window), name, app, &id); code {
	case 0:
		return uint64(id), nil
	case -1:
		return 0, ErrX11SurfacePending
	case -2:
		return 0, fmt.Errorf("X11 association conflicts with an existing surface role or window")
	case -3:
		return 0, fmt.Errorf("application window limit reached")
	default:
		return 0, fmt.Errorf("X11 association allocation failed")
	}
}
func (s *Server) UpdateX11(window uint32, title, appID string) error {
	if err := s.valid(); err != nil {
		return err
	}
	if err := x11Labels(title, appID); err != nil {
		return err
	}
	name, app := C.CString(title), C.CString(appID)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(app))
	if C.worldr_apps_update_x11(s.ptr, C.uint32_t(window), name, app) != 0 {
		return fmt.Errorf("X11 metadata allocation failed")
	}
	return nil
}
func (s *Server) WithdrawX11(window uint32) error {
	if err := s.valid(); err != nil {
		return err
	}
	C.worldr_apps_withdraw_x11(s.ptr, C.uint32_t(window))
	return nil
}
func (s *Server) X11Window(surfaceID uint64) uint32 {
	if s == nil || s.ptr == nil {
		return 0
	}
	return uint32(C.worldr_apps_x11_window(s.ptr, C.uint64_t(surfaceID)))
}
