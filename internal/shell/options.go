package shell

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codemodify/worldr/internal/engine"
)

// Backend selects a present path.
type Backend string

const (
	BackendAuto          Backend = "auto"
	BackendVKDisplay     Backend = "vk-display"
	BackendDRM           Backend = "drm"
	BackendWaylandClient Backend = "wayland-client"
	BackendNested        Backend = "nested"
	BackendHeadless      Backend = "headless"
)

// Options are worldr-shell CLI flags.
type Options struct {
	Backend          Backend
	Card             string
	TakeOverDisplay  bool
	Duration         time.Duration
	Color            [4]float32
	Compositor       bool
	WaylandDisplay   string
	ListDevices      bool
	SSD              bool
	FullscreenClient bool
	ClientWidth      int
	ClientHeight     int
	XWayland         bool
	Effects          engine.Tier
	OverviewDemo     bool
	Workspaces       int
}

// ErrTakeOverRequired is returned when a real display backend would steal
// an existing graphical session without an explicit opt-in.
var ErrTakeOverRequired = errors.New("graphical session is active; refusing to take over the display")

// ParseFlags reads os.Args. The returned FlagSet has already parsed.
func ParseFlags(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet("worldr-shell", flag.ContinueOnError)
	backend := fs.String("backend", string(BackendAuto), "auto|vk-display|drm|wayland-client|nested|headless")
	fs.StringVar(&o.Card, "card", "", "DRM card path (default: first /dev/dri/cardN)")
	fs.BoolVar(&o.TakeOverDisplay, "take-over-display", false, "allow vk-display/drm while DISPLAY or WAYLAND_DISPLAY is set")
	fs.DurationVar(&o.Duration, "duration", 0, "exit after this long (0 = until SIGINT). Use 15s for a first TTY test")
	color := fs.String("color", "#0b1020", "clear color as #RRGGBB")
	fs.BoolVar(&o.Compositor, "compositor", true, "listen as a Wayland compositor (also inside wayland-client/nested)")
	fs.StringVar(&o.WaylandDisplay, "wayland-display", "", "WAYLAND_DISPLAY name to advertise (default: first free wayland-N)")
	fs.BoolVar(&o.ListDevices, "list-devices", false, "print Vulkan devices and exit")
	ssd := fs.Bool("ssd", true, "draw server-side decoration chrome around client surfaces")
	fs.BoolVar(&o.FullscreenClient, "fullscreen-client", false, "wayland-client: request fullscreen (still nested, safe)")
	fs.IntVar(&o.ClientWidth, "width", 1280, "wayland-client window width")
	fs.IntVar(&o.ClientHeight, "height", 720, "wayland-client window height")
	fs.BoolVar(&o.XWayland, "xwayland", false, "launch rootless Xwayland against the worldr socket (xterm/xeyes)")
	effects := fs.String("effects", "high", "window theater: high|low|off (scale+fade map/unmap; off disables)")
	fs.BoolVar(&o.OverviewDemo, "overview-demo", false, "auto-enter expose after the first window maps (smoke)")
	fs.IntVar(&o.Workspaces, "workspaces", engine.WorkspaceDefault, "virtual desktops (2–4, default 3)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.Backend = Backend(strings.ToLower(strings.TrimSpace(*backend)))
	switch o.Backend {
	case BackendAuto, BackendVKDisplay, BackendDRM, BackendWaylandClient, BackendNested, BackendHeadless:
	default:
		return o, fmt.Errorf("unknown --backend=%s", *backend)
	}
	c, err := ParseColor(*color)
	if err != nil {
		return o, err
	}
	o.Color = c
	o.SSD = *ssd
	tier, err := engine.ParseTier(*effects)
	if err != nil {
		return o, err
	}
	o.Effects = tier
	o.Workspaces = engine.ClampWorkspaces(o.Workspaces)
	return o, nil
}

// GraphicalSession reports whether this process inherited a nested session.
func GraphicalSession() (wayland, x11 bool) {
	return os.Getenv("WAYLAND_DISPLAY") != "", os.Getenv("DISPLAY") != ""
}

// CheckTakeover refuses DRM/Vulkan display backends when a graphical
// session is already attached, unless the user passed --take-over-display.
// --backend=auto is allowed when a host Wayland/X11 socket is present
// (nested/headless). A leftover XDG_SESSION_TYPE=wayland|x11 without a
// socket would otherwise pick vk-display and steal the GPU.
func CheckTakeover(backend Backend, takeOver bool) error {
	if takeOver {
		return nil
	}
	seat := ProbeSeat()
	if !seat.LooksGraphical {
		return nil
	}
	if backend == BackendAuto && (seat.Wayland || seat.X11) {
		return nil
	}
	if !TakesDisplay(backend) && backend != BackendAuto {
		return nil
	}
	return fmt.Errorf("%w (WAYLAND_DISPLAY=%q DISPLAY=%q XDG_SESSION_TYPE=%q vt=%s). Spare TTY: Ctrl+Alt+F3, login, unset WAYLAND_DISPLAY/DISPLAY, then scripts/try-tty.sh. Or pass --take-over-display (steals the GPU). See docs/RUN-ABOX.md",
		ErrTakeOverRequired, os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY"), seat.SessionType, emptyDash(seat.ActiveVT))
}

// TakesDisplay is true when the backend will attempt DRM master / KHR_display.
func TakesDisplay(b Backend) bool {
	return b == BackendVKDisplay || b == BackendDRM
}
