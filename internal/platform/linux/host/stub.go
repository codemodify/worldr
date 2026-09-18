//go:build !linux || !cgo

package host

import (
	"fmt"
	"unsafe"
)

type Window struct{}

func Open(string, int, int, bool) (*Window, error) {
	return nil, fmt.Errorf("native host requires Linux, CGO, wayland-client and xkbcommon")
}
func (*Window) Close()                                    {}
func (*Window) Handles() (unsafe.Pointer, unsafe.Pointer) { return nil, nil }
func (*Window) Size() (int, int)                          { return 0, 0 }
func (*Window) LogicalSize() (int, int)                   { return 0, 0 }
func (*Window) Scale() float64                            { return 1 }
func (*Window) Poll(dst []Event) ([]Event, error) {
	return dst[:0], fmt.Errorf("native host unavailable")
}
func (*Window) ClipboardOffer() ClipboardOffer             { return ClipboardOffer{} }
func (*Window) OfferClipboard(uint64, []string) error      { return ErrClipboardUnavailable }
func (*Window) ReceiveClipboard(uint64, string, int) error { return ErrClipboardUnavailable }
func (*Window) PollClipboardRequests() []ClipboardRequest  { return nil }
func (*Window) SetTextInput(TextInputState) error          { return ErrClosed }
func (*Window) TextInputAvailable() bool                   { return false }
