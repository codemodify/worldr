//go:build !linux || !cgo

package apps

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
)

type Server struct{}

func (*Server) SetDMABufImporter([]dmabuf.Format, func(dmabuf.Descriptor) (*render.Texture, error)) error {
	return errUnavailable
}
func (*Server) SetDMABufImporterForDevice([]dmabuf.Format, string, func(dmabuf.Descriptor) (*render.Texture, error)) error {
	return errUnavailable
}
func (*Server) DMABufCapabilities() ([]dmabuf.Format, string) { return nil, "" }
func (*Server) RetiredTextures() []*render.Texture            { return nil }

func (*Server) Cursor() Cursor { return Cursor{} }

func (*Server) ClipboardOffer() ClipboardOffer             { return ClipboardOffer{} }
func (*Server) OfferClipboard(uint64, []string) error      { return errUnavailable }
func (*Server) ReceiveClipboard(uint64, string, int) error { return errUnavailable }
func (*Server) PollClipboardRequests() []ClipboardRequest  { return nil }

var errUnavailable = errors.New("Wayland application host requires Linux and cgo")

func Open(int, int) (*Server, error)                                           { return nil, errUnavailable }
func (*Server) Socket() string                                                 { return "" }
func (*Server) Poll() ([]Surface, error)                                       { return nil, errUnavailable }
func (*Server) Focus(uint64) error                                             { return errUnavailable }
func (*Server) Pointer(uint64, float32, float32) error                         { return errUnavailable }
func (*Server) Button(uint32, bool, uint32) error                              { return errUnavailable }
func (*Server) Axis(float32, float32, uint32) error                            { return errUnavailable }
func (*Server) Key(uint32, bool, uint32, uint32, uint32, uint32, uint32) error { return errUnavailable }
func (*Server) SetKeymap(string) error                                         { return errUnavailable }
func (*Server) Resize(uint64, int, int) error                                  { return errUnavailable }
func (*Server) Close() error                                                   { return nil }
func (*Server) RequestClose() error                                            { return errUnavailable }
func (*Server) CloseSurface(uint64) error                                      { return errUnavailable }
func (*Server) Modifiers(uint32, uint32, uint32, uint32) error                 { return errUnavailable }
func (*Server) SetRepeat(int32, int32) error                                   { return errUnavailable }

func (*Server) DragActive() bool  { return false }
func (*Server) CancelDrag() error { return errUnavailable }
func (*Server) AssociateX11(uint32, uint32, uint32, string, string) (uint64, error) {
	return 0, errUnavailable
}
func (*Server) UpdateX11(uint32, string, string) error { return errUnavailable }
func (*Server) WithdrawX11(uint32) error               { return errUnavailable }
func (*Server) X11Window(uint64) uint32                { return 0 }
