package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Seat is a best-effort snapshot of the logind / DRM / VT environment.
// Used to refuse display takeover and to pick --backend=auto.
type Seat struct {
	Wayland        bool
	X11            bool
	SessionType    string // XDG_SESSION_TYPE: wayland|x11|tty|…
	SeatName       string // XDG_SEAT
	VTNR           string // XDG_VTNR
	ActiveVT       string // /sys/class/tty/tty0/active (e.g. tty3)
	DRMCards       []string
	GraphicalEnv   bool // WAYLAND_DISPLAY or DISPLAY
	GraphicalType  bool // XDG_SESSION_TYPE is wayland or x11
	LooksGraphical bool
}

// ProbeSeat reads process env + a few sysfs nodes. Never fails.
func ProbeSeat() Seat {
	s := Seat{
		SessionType: strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE")),
		SeatName:    strings.TrimSpace(os.Getenv("XDG_SEAT")),
		VTNR:        strings.TrimSpace(os.Getenv("XDG_VTNR")),
		ActiveVT:    readActiveVT(),
		DRMCards:    ListDRMCards(),
	}
	s.Wayland, s.X11 = GraphicalSession()
	s.GraphicalEnv = s.Wayland || s.X11
	s.GraphicalType = s.SessionType == "wayland" || s.SessionType == "x11"
	s.LooksGraphical = s.GraphicalEnv || s.GraphicalType
	return s
}

// HasDRM is true when at least one /dev/dri/card* node exists.
func HasDRM() bool { return len(ListDRMCards()) > 0 }

// ValidateDRMCard checks --card. Empty means "first /dev/dri/cardN".
// Render nodes cannot drmSetMaster; refuse them with a TTY hint.
func ValidateDRMCard(card string) error {
	if card == "" {
		return nil
	}
	base := filepath.Base(card)
	if strings.HasPrefix(base, "renderD") {
		return fmt.Errorf("%s is a render node — vk-display/drm need a KMS primary node (/dev/dri/cardN). Spare TTY: Ctrl+Alt+F3 then scripts/try-tty.sh", card)
	}
	if !strings.HasPrefix(card, "/dev/dri/card") {
		return fmt.Errorf("refusing --card=%s (expected /dev/dri/cardN). Spare TTY: Ctrl+Alt+F3 then scripts/try-tty.sh", card)
	}
	st, err := os.Stat(card)
	if err != nil {
		return fmt.Errorf("no DRM device %s (%v). Need /dev/dri/card* and video/render. Spare TTY: scripts/try-tty.sh", card, err)
	}
	if st.IsDir() {
		return fmt.Errorf("--card=%s is a directory", card)
	}
	return nil
}

// ListDRMCards returns existing /dev/dri/cardN paths (sorted).
func ListDRMCards() []string {
	matches, err := filepath.Glob("/dev/dri/card*")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out
}

func readActiveVT() string {
	b, err := os.ReadFile("/sys/class/tty/tty0/active")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// String is a one-line log for TTY bring-up.
func (s Seat) String() string {
	drm := "none"
	if len(s.DRMCards) > 0 {
		drm = strings.Join(s.DRMCards, ",")
	}
	kind := "tty"
	if s.LooksGraphical {
		kind = "graphical"
	}
	return fmt.Sprintf("kind=%s vt=%s vtnr=%s seat=%s type=%s drm=%s wayland=%v x11=%v",
		kind, emptyDash(s.ActiveVT), emptyDash(s.VTNR), emptyDash(s.SeatName),
		emptyDash(s.SessionType), drm, s.Wayland, s.X11)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// AutoPresentOrder is --backend=auto: nested if WAYLAND_DISPLAY is set
// (and we are not taking over), else vk-display/drm when a card exists.
func AutoPresentOrder(wayland, x11, takeOver, drmOK bool) []Backend {
	if (wayland || x11) && !takeOver {
		if wayland {
			return []Backend{BackendWaylandClient, BackendHeadless}
		}
		// X11 session without Wayland: do not steal the display.
		return []Backend{BackendHeadless}
	}
	if drmOK {
		return []Backend{BackendVKDisplay, BackendDRM, BackendHeadless}
	}
	return []Backend{BackendHeadless}
}

// SessionLooksGraphical is the takeover refuse signal: session env
// (WAYLAND_DISPLAY / DISPLAY) or XDG_SESSION_TYPE=wayland|x11.
func SessionLooksGraphical() bool {
	return ProbeSeat().LooksGraphical
}
