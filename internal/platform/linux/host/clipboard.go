//go:build linux && cgo

package host

/*
#include "host.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func (w *Window) ClipboardOffer() ClipboardOffer {
	if w == nil || w.ptr == nil {
		return ClipboardOffer{}
	}
	c := C.worldr_host_clipboard_state(w.ptr)
	raw := C.worldr_host_clipboard_offer(c)
	offer := ClipboardOffer{Revision: uint64(raw.revision), ID: uint64(raw.id), ExternalID: uint64(raw.external_id), Available: raw.available != 0}
	for i := 0; i < int(raw.mime_count); i++ {
		offer.MIMEs = append(offer.MIMEs, C.GoString(C.worldr_host_clipboard_mime(c, C.int(i))))
	}
	return offer
}

// ReceiveClipboard requests bytes only in response to an actual consumer. The
// fd is borrowed: libwayland duplicates it while marshalling; the caller closes
// its copy after this method returns. No reads, writes or roundtrips occur here.
func (w *Window) ReceiveClipboard(offerID uint64, mime string, fd int) error {
	if w == nil || w.ptr == nil {
		return ErrClosed
	}
	if !clipboardMIME(mime) || fd < 0 {
		return fmt.Errorf("invalid clipboard MIME or file descriptor")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		return fmt.Errorf("clipboard file descriptor: %w", err)
	}
	text := C.CString(mime)
	defer C.free(unsafe.Pointer(text))
	return clipboardResult(C.worldr_host_clipboard_receive(C.worldr_host_clipboard_state(w.ptr), C.uint64_t(offerID), text, C.int(fd)))
}

// OfferClipboard advertises MIME metadata and defers all transfer until a host
// application asks for it. externalID identifies the bridge's original source.
// Passing zero clears only a source owned by this window. New publication
// requires keyboard focus and an input serial; callers may retry after focus.
func (w *Window) OfferClipboard(externalID uint64, mimes []string) error {
	if w == nil || w.ptr == nil {
		return ErrClosed
	}
	if externalID == 0 {
		if len(mimes) != 0 {
			return fmt.Errorf("clearing clipboard must not contain MIME formats")
		}
		return clipboardResult(C.worldr_host_clipboard_publish(C.worldr_host_clipboard_state(w.ptr), 0, nil, 0))
	}
	if len(mimes) == 0 || len(mimes) > 64 {
		return fmt.Errorf("clipboard must advertise between 1 and 64 MIME formats")
	}
	for _, mime := range mimes {
		if !clipboardMIME(mime) {
			return fmt.Errorf("invalid clipboard MIME format")
		}
	}
	values := make([]*C.char, len(mimes))
	for i, mime := range mimes {
		values[i] = C.CString(mime)
		defer C.free(unsafe.Pointer(values[i]))
	}
	return clipboardResult(C.worldr_host_clipboard_publish(C.worldr_host_clipboard_state(w.ptr), C.uint64_t(externalID), &values[0], C.int(len(values))))
}

func (w *Window) PollClipboardRequests() []ClipboardRequest {
	if w == nil || w.ptr == nil {
		return nil
	}
	var raw [32]C.worldr_clipboard_request
	count := int(C.worldr_host_clipboard_requests(C.worldr_host_clipboard_state(w.ptr), &raw[0], C.int(len(raw))))
	requests := make([]ClipboardRequest, 0, count)
	for i := 0; i < count; i++ {
		requests = append(requests, ClipboardRequest{ExternalID: uint64(raw[i].external_id), MIME: C.GoString(raw[i].mime), FD: int(raw[i].fd)})
		C.free(unsafe.Pointer(raw[i].mime))
	}
	return requests
}

func clipboardMIME(mime string) bool {
	return len(mime) > 0 && len(mime) <= 1024 && strings.IndexByte(mime, 0) < 0
}

func clipboardResult(result C.int) error {
	switch result {
	case 0:
		return nil
	case -1:
		return ErrClipboardUnavailable
	case -2:
		return ErrClipboardStale
	default:
		return fmt.Errorf("cannot allocate host clipboard source")
	}
}
