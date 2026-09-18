//go:build linux && cgo

// Package host owns a Wayland window and normalized input transport. Vulkan
// presents directly to its wl_surface; no CPU pixel transport is involved.
package host

/*
#cgo pkg-config: wayland-client xkbcommon
#include "host.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type Window struct {
	ptr            *C.worldr_host
	events         [512]C.worldr_host_event
	textInputCause bool
}

// Open allows five seconds for registry discovery and the initial configure.
func Open(title string, w, h int, fullscreen bool) (*Window, error) {
	return open(title, w, h, fullscreen, 5000)
}
func open(title string, w, h int, fullscreen bool, timeoutMillis int) (*Window, error) {
	if w <= 0 || h <= 0 || w > 32768 || h > 32768 {
		return nil, fmt.Errorf("Wayland host dimensions must be between 1 and 32768")
	}
	name := C.CString(title)
	defer C.free(unsafe.Pointer(name))
	var p *C.worldr_host
	var err [512]C.char
	full := 0
	if fullscreen {
		full = 1
	}
	if C.worldr_host_open(name, C.int(w), C.int(h), C.int(full), C.int(timeoutMillis), &p, &err[0], 512) != 0 {
		return nil, fmt.Errorf("%s", C.GoString(&err[0]))
	}
	return &Window{ptr: p}, nil
}
func (w *Window) Close() {
	if w != nil && w.ptr != nil {
		C.worldr_host_close(w.ptr)
		w.ptr = nil
	}
}
func (w *Window) Handles() (unsafe.Pointer, unsafe.Pointer) {
	if w == nil || w.ptr == nil {
		return nil, nil
	}
	return C.worldr_host_display(w.ptr), C.worldr_host_surface(w.ptr)
}
func (w *Window) Size() (int, int) {
	if w == nil || w.ptr == nil {
		return 0, 0
	}
	var x, y C.int
	C.worldr_host_size(w.ptr, &x, &y)
	return int(x), int(y)
}
func (w *Window) LogicalSize() (int, int) {
	if w == nil || w.ptr == nil {
		return 0, 0
	}
	var x, y C.int
	C.worldr_host_logical_size(w.ptr, &x, &y)
	return int(x), int(y)
}
func (w *Window) Scale() float64 {
	if w == nil || w.ptr == nil {
		return 1
	}
	return float64(C.worldr_host_scale120(w.ptr)) / 120
}
func (w *Window) Poll(dst []Event) ([]Event, error) {
	if w == nil || w.ptr == nil {
		return dst[:0], ErrClosed
	}
	var err [512]C.char
	n := C.worldr_host_poll(w.ptr, &w.events[0], 512, &err[0], 512)
	if n < 0 {
		return dst[:0], fmt.Errorf("%s", C.GoString(&err[0]))
	}
	dst = dst[:0]
	for i := 0; i < int(n); i++ {
		e := w.events[i]
		v := Event{
			Kind: Kind(e.kind), Code: uint32(e.code), Modifiers: uint8(e.mods),
			X: float32(e.x), Y: float32(e.y), Pressed: e.pressed != 0,
			Keycode: uint32(e.keycode), ButtonCode: uint32(e.button), Time: uint32(e.time),
			Depressed: uint32(e.depressed), Latched: uint32(e.latched), Locked: uint32(e.locked), Group: uint32(e.group),
			ScrollX: float32(e.scroll_x), ScrollY: float32(e.scroll_y),
			RepeatRate: int32(e.repeat_rate), RepeatDelay: int32(e.repeat_delay),
			PreeditBegin: int32(e.preedit_begin), PreeditEnd: int32(e.preedit_end), DeleteBefore: uint32(e.delete_before), DeleteAfter: uint32(e.delete_after),
		}
		if e.text != nil {
			v.Text = C.GoString(e.text)
			C.free(unsafe.Pointer(e.text))
			w.events[i].text = nil
		}
		if e.text_context != nil {
			v.TextContext = C.GoString(e.text_context)
			C.free(unsafe.Pointer(e.text_context))
			w.events[i].text_context = nil
		}
		if !validTextEvent(v) {
			continue
		}
		if v.Kind == TextCommit || v.Kind == TextPreedit {
			w.textInputCause = true
		} else if v.Kind == Key && v.Pressed || v.Kind == Down || v.Kind == KeyboardCancel {
			w.textInputCause = false
		}
		if e.keymap != nil {
			v.Keymap = C.GoString(e.keymap)
			C.free(unsafe.Pointer(e.keymap))
			w.events[i].keymap = nil
		}
		dst = append(dst, v)
	}
	return dst, nil
}
