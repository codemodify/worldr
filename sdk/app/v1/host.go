package app

import (
	"errors"
	"sync/atomic"

	"github.com/codemodify/worldr/internal/platform/linux/host"
)

var ErrClipboardUnavailable = errors.New("clipboard is unavailable without a focused native window")

// Host exposes native window operations. Except Wake, methods and clipboard
// callbacks belong to the application goroutine. Native move and resize must
// be requested while handling a pointer press, using its compositor serial.
type Host struct {
	window           *host.Window
	closing          bool
	dirty            atomic.Bool
	done             chan struct{}
	clipboardResults chan clipboardResult
	clipboardSerial  uint64
	clipboardText    map[uint64]string
	clipboardPending bool
}

type clipboardResult struct {
	callback func(string, error)
	text     string
	err      error
}

func newHost(window *host.Window) *Host {
	h := &Host{window: window, done: make(chan struct{}), clipboardResults: make(chan clipboardResult, 4), clipboardText: make(map[uint64]string)}
	h.Wake()
	return h
}

// Wake requests a frame on the next paced update; safe from worker goroutines.
func (h *Host) Wake() {
	if h != nil {
		h.dirty.Store(true)
	}
}
func (h *Host) RequestClose() {
	if h != nil {
		h.closing = true
	}
}
func (h *Host) BeginMove() bool { return h != nil && h.window != nil && h.window.BeginMove() }
func (h *Host) BeginResize(edge ResizeEdge) bool {
	return h != nil && h.window != nil && h.window.BeginResize(edge)
}
func (h *Host) Minimize() {
	if h != nil && h.window != nil {
		h.window.Minimize()
	}
}
func (h *Host) SetMaximized(value bool) {
	if h != nil && h.window != nil {
		h.window.SetMaximized(value)
	}
}
func (h *Host) Maximized() bool { return h != nil && h.window != nil && h.window.Maximized() }
func (h *Host) SetTitle(title string) {
	if h != nil && h.window != nil {
		h.window.SetTitle(title)
	}
}

// WriteClipboard advertises UTF-8 text. Bytes are transferred only when another
// application requests them; the text remains available after focus changes.
func (h *Host) WriteClipboard(text string) error {
	if h == nil || h.window == nil {
		return ErrClipboardUnavailable
	}
	if len(text) > maxClipboardBytes {
		return errors.New("clipboard text exceeds 4 MiB")
	}
	h.serveClipboard()
	h.clipboardSerial++
	if err := h.window.OfferClipboard(h.clipboardSerial, []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING"}); err != nil {
		return err
	}
	h.clipboardText[h.clipboardSerial] = text
	// Older callbacks already own their strings. Keep a few recent source IDs
	// for requests already queued by the compositor while selection changes.
	for id := range h.clipboardText {
		if h.clipboardSerial-id >= 4 {
			delete(h.clipboardText, id)
		}
	}
	return nil
}

// ReadClipboard starts one bounded asynchronous read. Its callback executes on
// the application goroutine; it is discarded if the application closes first.
func (h *Host) ReadClipboard(callback func(string, error)) error {
	if h == nil || h.window == nil {
		return ErrClipboardUnavailable
	}
	if callback == nil {
		return errors.New("clipboard read requires a callback")
	}
	if h.clipboardPending {
		return errors.New("clipboard read is already pending")
	}
	return h.readClipboard(callback)
}

func (h *Host) drainClipboard() {
	h.serveClipboard()
	for {
		select {
		case result := <-h.clipboardResults:
			h.clipboardPending = false
			result.callback(result.text, result.err)
			h.Wake()
		default:
			return
		}
	}
}

const maxClipboardBytes = 4 << 20
