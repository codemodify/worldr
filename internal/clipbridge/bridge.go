// Package clipbridge is the text clipboard state machine between a nested
// host compositor (Plasma/KWin) and worldr’s in-compositor selection.
package clipbridge

import (
	"strings"
	"syscall"
)

const (
	MimeTextPlain = "text/plain"
	MimeTextUTF8  = "text/plain;charset=utf-8"
	MaxTextBytes  = 1 << 20
)

// TextMimes are the types we offer in both directions.
func TextMimes() []string {
	return []string{MimeTextPlain, MimeTextUTF8}
}

// IsPlainText reports a text/plain family MIME (including UTF-8 aliases).
func IsPlainText(m string) bool {
	m = strings.ToLower(strings.TrimSpace(m))
	switch m {
	case MimeTextPlain, MimeTextUTF8, "text/plain; charset=utf-8", "utf8_string", "text", "string":
		return true
	}
	return strings.HasPrefix(m, "text/plain")
}

// PickPlainMime returns the offered type we should receive/send, or "".
func PickPlainMime(offered []string) string {
	for _, m := range offered {
		if m == MimeTextPlain {
			return m
		}
	}
	for _, m := range offered {
		if IsPlainText(m) {
			return m
		}
	}
	return ""
}

// WriteText writes b to fd and closes it. Caps at MaxTextBytes.
func WriteText(fd int, b []byte) {
	if fd <= 0 {
		return
	}
	defer func() { _ = syscall.Close(fd) }()
	if len(b) > MaxTextBytes {
		b = b[:MaxTextBytes]
	}
	if len(b) == 0 {
		return
	}
	_, _ = syscall.Write(fd, b)
}

// HostOwn tracks whether the nest client currently owns the host selection
// (so an echo data_device.selection must not be imported back).
type HostOwn struct {
	clip bool
	prim bool
}

// Set records ownership after set_selection / cancelled.
func (h *HostOwn) Set(primary, own bool) {
	if h == nil {
		return
	}
	if primary {
		h.prim = own
		return
	}
	h.clip = own
}

// Owns is true if we should ignore a host selection event (it is ours).
func (h *HostOwn) Owns(primary bool) bool {
	if h == nil {
		return false
	}
	if primary {
		return h.prim
	}
	return h.clip
}
