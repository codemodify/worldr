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

func (s *Server) ClipboardOffer() ClipboardOffer {
	if s == nil || s.ptr == nil {
		return ClipboardOffer{}
	}
	var raw C.worldr_app_clipboard_offer
	C.worldr_apps_clipboard_offer(s.ptr, &raw)
	offer := ClipboardOffer{Revision: uint64(raw.revision), ID: uint64(raw.id), ExternalID: uint64(raw.external_id)}
	for i := 0; i < int(raw.mime_count); i++ {
		offer.MIMEs = append(offer.MIMEs, C.GoString(C.worldr_apps_clipboard_mime(s.ptr, C.int(i))))
	}
	return offer
}

// OfferClipboard advertises metadata for an external source. Its contents are
// requested lazily through PollClipboardRequests. ID zero clears the selection.
func (s *Server) OfferClipboard(externalID uint64, mimes []string) error {
	if err := s.valid(); err != nil {
		return err
	}
	if len(mimes) > 64 || (externalID == 0 && len(mimes) != 0) {
		return fmt.Errorf("invalid clipboard MIME list")
	}
	for _, mime := range mimes {
		if len(mime) == 0 || len(mime) > 4096 || strings.IndexByte(mime, 0) >= 0 {
			return fmt.Errorf("invalid clipboard MIME type")
		}
	}
	var mem unsafe.Pointer
	var names **C.char
	if len(mimes) > 0 {
		mem = C.malloc(C.size_t(len(mimes)) * C.size_t(unsafe.Sizeof(uintptr(0))))
		if mem == nil {
			return fmt.Errorf("clipboard MIME allocation failed")
		}
		defer C.free(mem)
		list := unsafe.Slice((**C.char)(mem), len(mimes))
		for i, mime := range mimes {
			list[i] = C.CString(mime)
			defer C.free(unsafe.Pointer(list[i]))
		}
		names = (**C.char)(mem)
	}
	if C.worldr_apps_offer_clipboard(s.ptr, C.uint64_t(externalID), names, C.int(len(mimes))) != 0 {
		return fmt.Errorf("cannot advertise clipboard source")
	}
	return nil
}

// ReceiveClipboard relays an owned application selection into a borrowed file
// descriptor. The caller retains ownership and closes FD after this call.
func (s *Server) ReceiveClipboard(offerID uint64, mime string, fd int) error {
	if err := s.valid(); err != nil {
		return err
	}
	if fd < 0 || len(mime) == 0 || len(mime) > 4096 || strings.IndexByte(mime, 0) >= 0 {
		return fmt.Errorf("invalid clipboard receive request")
	}
	name := C.CString(mime)
	defer C.free(unsafe.Pointer(name))
	if C.worldr_apps_receive_clipboard(s.ptr, C.uint64_t(offerID), name, C.int(fd)) != 0 {
		return fmt.Errorf("clipboard offer is stale or MIME type unavailable")
	}
	return nil
}

// PollClipboardRequests transfers ownership of each returned FD to the caller.
// Undrained requests are closed by Server.Close.
func (s *Server) PollClipboardRequests() []ClipboardRequest {
	if s == nil || s.ptr == nil {
		return nil
	}
	var raw [64]C.worldr_app_clipboard_request
	n := int(C.worldr_apps_clipboard_requests(s.ptr, &raw[0], 64))
	out := make([]ClipboardRequest, 0, n)
	for i := 0; i < n; i++ {
		v := raw[i]
		out = append(out, ClipboardRequest{ExternalID: uint64(v.external_id), MIME: C.GoString(v.mime), FD: int(v.fd)})
		C.free(unsafe.Pointer(v.mime))
	}
	return out
}
