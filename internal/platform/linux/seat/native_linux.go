//go:build linux && cgo

package seat

/*
#cgo pkg-config: libseat libinput libudev
#include "seat.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"syscall"
	"unsafe"
)

type nativeBackend struct{ ptr *C.worldr_seat }

func Open() (*Session, error) {
	var code C.int
	ptr := C.worldr_seat_open(&code)
	if ptr == nil {
		return nil, fmt.Errorf("open direct seat: %w", syscall.Errno(code))
	}
	return newSession(&nativeBackend{ptr}), nil
}
func nativeError(operation string, code C.int) error {
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, syscall.Errno(-code))
}
func (b *nativeBackend) dispatch() (Event, error) {
	code := C.worldr_seat_dispatch(b.ptr)
	if code < 0 {
		return 0, nativeError("dispatch seat", code)
	}
	return Event(code), nil
}
func (b *nativeBackend) name() string { return C.GoString(C.worldr_seat_name(b.ptr)) }
func (b *nativeBackend) openDevice(path string) (int, int, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var fd C.int
	id := C.worldr_seat_device_open(b.ptr, p, &fd)
	return int(id), int(fd), nativeError("open seat device", id)
}
func (b *nativeBackend) closeDevice(id int) error {
	return nativeError("release seat device", C.worldr_seat_device_close(b.ptr, C.int(id)))
}
func (b *nativeBackend) disable() error {
	return nativeError("acknowledge seat disable", C.worldr_seat_disable(b.ptr))
}
func (b *nativeBackend) switchSession(vt int) error {
	return nativeError("switch VT", C.worldr_seat_switch(b.ptr, C.int(vt)))
}
func (b *nativeBackend) close() error {
	err := nativeError("close seat", C.worldr_seat_close(b.ptr))
	b.ptr = nil
	return err
}

type nativeInput struct{ ptr *C.worldr_input }

func (b *nativeBackend) newInput() (inputBackend, error) {
	var code C.int
	p := C.worldr_input_open(b.ptr, &code)
	if p == nil {
		return nil, fmt.Errorf("open libinput: %w", syscall.Errno(code))
	}
	return &nativeInput{p}, nil
}
func (i *nativeInput) poll() ([]InputEvent, error) {
	var raw [256]C.worldr_input_event
	n := C.worldr_input_poll(i.ptr, &raw[0], 256)
	if n < 0 {
		return nil, nativeError("poll libinput", n)
	}
	out := make([]InputEvent, int(n))
	for index := range out {
		r := raw[index]
		out[index] = InputEvent{Kind: InputKind(r.kind), X: float64(r.x), Y: float64(r.y), Code: uint32(r.code), Time: uint32(r.time), Pressed: r.pressed != 0}
	}
	return out, nil
}
func (i *nativeInput) close() error {
	err := nativeError("close libinput", C.worldr_input_close(i.ptr))
	i.ptr = nil
	return err
}
