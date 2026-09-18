//go:build !linux || !cgo

package xwayland

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"io"
)

type Bridge struct{}

func Open(*apps.Server, io.Writer) (*Bridge, error) {
	return nil, errors.New("Xwayland requires Linux, cgo, XCB, and Xwayland")
}
func (*Bridge) Poll() error                                { return ErrClosed }
func (*Bridge) Close() error                               { return nil }
func (*Bridge) Display() string                            { return "" }
func (*Bridge) Environment(parent []string) []string       { return privateEnvironment(parent, "", "", "") }
func (*Bridge) Accelerated() bool                          { return false }
func (*Bridge) Window(uint64) (Window, bool)               { return Window{}, false }
func (*Bridge) Focus(uint64) error                         { return ErrClosed }
func (*Bridge) Resize(uint64, int, int) error              { return ErrClosed }
func (*Bridge) CloseSurface(uint64) error                  { return ErrClosed }
func (*Bridge) ClipboardOffer() ClipboardOffer             { return ClipboardOffer{} }
func (*Bridge) OfferClipboard(uint64, []string) error      { return ErrClosed }
func (*Bridge) ReceiveClipboard(uint64, string, int) error { return ErrClosed }
func (*Bridge) PollClipboardRequests() []ClipboardRequest  { return nil }
func (*Bridge) XDNDOffer() XDNDOffer                       { return XDNDOffer{} }
func (*Bridge) DragActive(uint64) bool                     { return false }
func (*Bridge) DragMotion(uint64, uint64, float32, float32, uint32) error {
	return ErrClosed
}
func (*Bridge) DropXDND(uint64, uint64, uint32) error { return ErrClosed }
func (*Bridge) CancelXDND() error                     { return ErrClosed }
