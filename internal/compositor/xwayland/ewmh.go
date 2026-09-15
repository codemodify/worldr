package xwayland

// Highest-ROI EWMH / ICCCM helpers for the tiny XWM.
// Not a full WM: enough for titles, chrome, focus, and popup-ish windows.

// SurfaceHints is what the compositor needs when an X11 window becomes a
// Wayland surface (or when properties change later).
type SurfaceHints struct {
	Win          uint32
	TransientFor uint32
	Title        string
	AppID        string
	NoChrome     bool
	X, Y, W, H   int
}

// EWMHSupportedNames is the _NET_SUPPORTED set we advertise.
// Intentionally small: no pager, struts, desktop, or moveresize.
func EWMHSupportedNames() []string {
	return []string{
		"_NET_SUPPORTED",
		"_NET_SUPPORTING_WM_CHECK",
		"_NET_WM_NAME",
		"_NET_ACTIVE_WINDOW",
		"_NET_CLIENT_LIST",
		"_NET_CLIENT_LIST_STACKING",
		"_NET_WM_WINDOW_TYPE",
		"_NET_WM_WINDOW_TYPE_NORMAL",
		"_NET_WM_WINDOW_TYPE_DIALOG",
		"_NET_WM_WINDOW_TYPE_UTILITY",
		"_NET_WM_WINDOW_TYPE_MENU",
		"_NET_WM_WINDOW_TYPE_DROPDOWN_MENU",
		"_NET_WM_WINDOW_TYPE_POPUP_MENU",
		"_NET_WM_WINDOW_TYPE_TOOLTIP",
		"_NET_WM_WINDOW_TYPE_NOTIFICATION",
		"_NET_WM_WINDOW_TYPE_COMBO",
		"_NET_WM_WINDOW_TYPE_DND",
		"_NET_WM_WINDOW_TYPE_SPLASH",
		"_NET_WM_STATE",
		"_NET_CLOSE_WINDOW",
	}
}

// ParseWMClass splits ICCCM WM_CLASS (instance\0class\0).
func ParseWMClass(b []byte) (instance, class string) {
	if len(b) == 0 {
		return "", ""
	}
	inst, rest, _ := split0(b)
	class, _, _ = split0(rest)
	return inst, class
}

// ParseTitle prefers _NET_WM_NAME (UTF-8) over WM_NAME (Latin-1 bytes).
func ParseTitle(netWMName, wmName []byte) string {
	if s := trim0(netWMName); s != "" {
		return s
	}
	return trim0(wmName)
}

// NoChromeFor reports whether an X window should skip SSD.
// Override-redirect, WM_TRANSIENT_FOR, and popup-ish _NET_WM_WINDOW_TYPE
// values are chrome-less. NORMAL / DIALOG without transient keep SSD.
func NoChromeFor(override, transient bool, types []uint32, popupTypes map[uint32]struct{}) bool {
	if override || transient {
		return true
	}
	for _, t := range types {
		if _, ok := popupTypes[t]; ok {
			return true
		}
	}
	return false
}

// pickPending chooses a mapped X window for a newly attached Wayland buffer.
// Prefer matching width×height; if only one window is waiting, take it.
func pickPending(pending []*xWin, w, h int) (picked *xWin, rest []*xWin) {
	if len(pending) == 0 {
		return nil, pending
	}
	for i := len(pending) - 1; i >= 0; i-- {
		p := pending[i]
		if p != nil && p.w == w && p.h == h {
			return p, append(append([]*xWin{}, pending[:i]...), pending[i+1:]...)
		}
	}
	if len(pending) == 1 {
		return pending[0], nil
	}
	return pending[0], pending[1:]
}

func split0(b []byte) (head string, rest []byte, ok bool) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), b[i+1:], true
		}
	}
	if len(b) == 0 {
		return "", nil, false
	}
	return string(b), nil, true
}

func trim0(b []byte) string {
	i := len(b)
	for i > 0 && b[i-1] == 0 {
		i--
	}
	return string(b[:i])
}

func hasAtom(list []uint32, want uint32) bool {
	if want == 0 {
		return false
	}
	for _, a := range list {
		if a == want {
			return true
		}
	}
	return false
}

func parseAtoms32(b []byte) []uint32 {
	n := len(b) / 4
	if n == 0 {
		return nil
	}
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
	}
	return out
}
