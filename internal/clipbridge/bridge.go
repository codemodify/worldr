// Package clipbridge is the clipboard MIME helper between a nested
// host compositor (Plasma/KWin) and worldr’s in-compositor selection
// (text/plain plus image/png, image/jpeg, image/webp, image/bmp).
package clipbridge

import (
	"strings"
	"syscall"
)

const (
	MimeTextPlain = "text/plain"
	MimeTextUTF8  = "text/plain;charset=utf-8"
	MimePNG       = "image/png"
	MimeJPEG      = "image/jpeg"
	MimeWebP      = "image/webp"
	MimeBMP       = "image/bmp"
	MaxTextBytes  = 1 << 20
	MaxImageBytes = 8 << 20
)

// TextMimes are the types we offer in both directions.
func TextMimes() []string {
	return []string{MimeTextPlain, MimeTextUTF8}
}

// ImageMimes are the raster types we bridge (png first, then jpeg/webp/bmp).
func ImageMimes() []string {
	return []string{MimePNG, MimeJPEG, MimeWebP, MimeBMP}
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

// IsImage reports a bridged raster MIME (png/jpeg/webp/bmp + aliases).
func IsImage(m string) bool {
	return CanonicalImage(m) != ""
}

// CanonicalImage maps a MIME (or alias) to png/jpeg/webp/bmp, or "".
func CanonicalImage(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	switch {
	case m == MimePNG || strings.HasPrefix(m, "image/png"):
		return MimePNG
	case m == MimeJPEG || m == "image/jpg" || strings.HasPrefix(m, "image/jpeg"):
		return MimeJPEG
	case m == MimeWebP || strings.HasPrefix(m, "image/webp"):
		return MimeWebP
	case m == MimeBMP || m == "image/x-bmp" || m == "image/x-ms-bmp":
		return MimeBMP
	}
	return ""
}

// PickImageMime prefers png, then jpeg, webp, bmp.
func PickImageMime(offered []string) string {
	pref := []string{MimePNG, MimeJPEG, MimeWebP, MimeBMP}
	for _, want := range pref {
		for _, m := range offered {
			if CanonicalImage(m) == want {
				return m
			}
		}
	}
	return ""
}

// Bridgeable is true when we can export/import at least one supported MIME.
func Bridgeable(offered []string) bool {
	return PickPlainMime(offered) != "" || PickImageMime(offered) != ""
}

// HostOfferMimes is the set we advertise on the nest host (text aliases + images).
func HostOfferMimes(src []string) []string {
	var out []string
	if PickPlainMime(src) != "" {
		out = append(out, TextMimes()...)
	}
	seen := map[string]bool{}
	for _, m := range src {
		if c := CanonicalImage(m); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// CapFor MIME size limit.
func CapFor(mime string) int {
	if IsImage(mime) {
		return MaxImageBytes
	}
	return MaxTextBytes
}

// WriteText writes b to fd and closes it. Caps at MaxTextBytes.
func WriteText(fd int, b []byte) {
	WriteBytes(fd, b, MaxTextBytes)
}

// WriteBytes writes b to fd and closes it. Caps at cap (or MaxTextBytes if cap<=0).
func WriteBytes(fd int, b []byte, capn int) {
	if fd <= 0 {
		return
	}
	defer func() { _ = syscall.Close(fd) }()
	if capn <= 0 {
		capn = MaxTextBytes
	}
	if len(b) > capn {
		b = b[:capn]
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
