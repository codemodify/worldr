package app

import (
	"os"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/platform/linux/xwayland"
)

type clipboardOffer struct {
	revision, id, external uint64
	mimes                  []string
	available              bool
}
type clipboardRequest struct {
	source uint64
	mime   string
	fd     int
}
type clipboardEndpoint interface {
	offer() clipboardOffer
	publish(uint64, []string) error
	receive(uint64, string, int) error
	requests() []clipboardRequest
}

func closeClipboardFD(fd int) {
	if fd >= 0 {
		if f := os.NewFile(uintptr(fd), "clipboard relay"); f != nil {
			_ = f.Close()
		}
	}
}

type hostClipboard struct{ window *host.Window }

func (c hostClipboard) offer() clipboardOffer {
	o := c.window.ClipboardOffer()
	return clipboardOffer{revision: o.Revision, id: o.ID, external: o.ExternalID, mimes: o.MIMEs, available: o.Available}
}
func (c hostClipboard) publish(id uint64, mimes []string) error {
	return c.window.OfferClipboard(id, mimes)
}
func (c hostClipboard) receive(id uint64, mime string, fd int) error {
	return c.window.ReceiveClipboard(id, mime, fd)
}
func (c hostClipboard) requests() []clipboardRequest {
	requests := c.window.PollClipboardRequests()
	result := make([]clipboardRequest, 0, len(requests))
	for _, r := range requests {
		result = append(result, clipboardRequest{source: r.ExternalID, mime: r.MIME, fd: r.FD})
	}
	return result
}

type applicationClipboardServer interface {
	ClipboardOffer() apps.ClipboardOffer
	OfferClipboard(uint64, []string) error
	ReceiveClipboard(uint64, string, int) error
	PollClipboardRequests() []apps.ClipboardRequest
}

type applicationClipboard struct{ server applicationClipboardServer }

func (c applicationClipboard) offer() clipboardOffer {
	o := c.server.ClipboardOffer()
	return clipboardOffer{revision: o.Revision, id: o.ID, external: o.ExternalID, mimes: o.MIMEs, available: true}
}
func (c applicationClipboard) publish(id uint64, mimes []string) error {
	return c.server.OfferClipboard(id, mimes)
}
func (c applicationClipboard) receive(id uint64, mime string, fd int) error {
	return c.server.ReceiveClipboard(id, mime, fd)
}
func (c applicationClipboard) requests() []clipboardRequest {
	requests := c.server.PollClipboardRequests()
	result := make([]clipboardRequest, 0, len(requests))
	for _, r := range requests {
		result = append(result, clipboardRequest{source: r.ExternalID, mime: r.MIME, fd: r.FD})
	}
	return result
}

type x11ClipboardBridge interface {
	ClipboardOffer() xwayland.ClipboardOffer
	OfferClipboard(uint64, []string) error
	ReceiveClipboard(uint64, string, int) error
	PollClipboardRequests() []xwayland.ClipboardRequest
}

type x11Clipboard struct{ bridge x11ClipboardBridge }

var _ x11ClipboardBridge = (*xwayland.Bridge)(nil)
var _ clipboardEndpoint = x11Clipboard{}

func (c x11Clipboard) offer() clipboardOffer {
	o := c.bridge.ClipboardOffer()
	return clipboardOffer{revision: o.Revision, id: o.ID, external: o.ExternalID, mimes: o.MIMEs, available: true}
}
func (c x11Clipboard) publish(id uint64, mimes []string) error {
	return c.bridge.OfferClipboard(id, mimes)
}
func (c x11Clipboard) receive(id uint64, mime string, fd int) error {
	return c.bridge.ReceiveClipboard(id, mime, fd)
}
func (c x11Clipboard) requests() []clipboardRequest {
	requests := c.bridge.PollClipboardRequests()
	result := make([]clipboardRequest, 0, len(requests))
	for _, r := range requests {
		result = append(result, clipboardRequest{source: r.ExternalID, mime: r.MIME, fd: r.FD})
	}
	return result
}
