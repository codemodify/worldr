// Package app runs worldr's native scene workspace. Application protocols and
// window management do not define the scene or the rendering loop.
package app

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Outputs                                          []uint32
	ListOutputs                                      bool
	Metrics                                          string
	GPUMemoryMiB                                     uint64
	Models                                           []string
	Research                                         []string
	NativeApplications                               []string
	Backend, Card, Snapshot, State                   string
	AccessibilitySocket                              string
	Experience, Project                              string
	Application                                      string
	Applications                                     []string
	X11Applications                                  []string
	Xwayland                                         bool
	ApplicationArgs                                  []string
	ApplicationProfile                               string
	Launches                                         []ApplicationLaunch
	Terminal, Axial                                  bool
	Width, Height, FPS, Frames                       int
	Duration                                         time.Duration
	Demo, Fullscreen, TakeOver, Version, ListDevices bool
	Fresh                                            bool
	Autosave                                         time.Duration
}

func Parse(args []string, output io.Writer) (Options, error) {
	var o Options
	var orderedLaunches []ApplicationLaunch
	f := flag.NewFlagSet("worldr-shell", flag.ContinueOnError)
	f.SetOutput(output)
	f.StringVar(&o.Backend, "backend", "auto", "auto|nested|headless|vk-display|drm (nested needs a Wayland host)")
	f.StringVar(&o.Experience, "experience", "workspace", "workspace|axial (general workspace or engineering study)")
	f.StringVar(&o.Project, "project", "", "open a native project browser at this directory")
	f.Func("model", "open an OBJ, STL or .worldr-model.json in a native 3D inspector (repeatable)", func(path string) error {
		if strings.TrimSpace(path) == "" || len(o.Models) >= 8 {
			return fmt.Errorf("--model requires a path; at most 8 models")
		}
		o.Models = append(o.Models, path)
		return nil
	})
	f.Func("research", "open a CSV, TSV or .worldr-data.json in the native research workbench (repeatable)", func(path string) error {
		if strings.TrimSpace(path) == "" || len(o.Research) >= 8 {
			return fmt.Errorf("--research requires a path; at most 8 dashboards")
		}
		o.Research = append(o.Research, path)
		return nil
	})
	f.Func("native-app", "launch a Worldr native-app SDK v1 executable (repeatable)", func(command string) error {
		if strings.TrimSpace(command) == "" || strings.IndexByte(command, 0) >= 0 || len(o.NativeApplications) >= 4 {
			return fmt.Errorf("--native-app requires an executable; at most 4 native apps")
		}
		o.NativeApplications = append(o.NativeApplications, command)
		return nil
	})
	f.BoolVar(&o.ListOutputs, "list-outputs", false, "list DRM connectors read-only without acquiring the display")
	f.Func("output", "select a DRM connector ID (repeatable, ordered left to right; default all connected)", func(value string) error {
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == 0 {
			return fmt.Errorf("--output requires a nonzero connector ID from --list-outputs")
		}
		if len(o.Outputs) >= 8 {
			return fmt.Errorf("at most 8 outputs")
		}
		for _, old := range o.Outputs {
			if old == uint32(id) {
				return fmt.Errorf("duplicate connector %d", id)
			}
		}
		o.Outputs = append(o.Outputs, uint32(id))
		return nil
	})
	f.StringVar(&o.Card, "card", "", "KMS primary node for direct display, e.g. /dev/dri/card1")
	f.IntVar(&o.Width, "width", 1440, "initial scene width in pixels")
	f.IntVar(&o.Height, "height", 900, "initial scene height in pixels")
	f.IntVar(&o.FPS, "fps", 60, "maximum frame rate (1–240)")
	f.IntVar(&o.Frames, "frames", 0, "stop after N rendered frames (0 = unlimited)")
	f.DurationVar(&o.Duration, "duration", 0, "stop after this duration, e.g. 30s")
	f.StringVar(&o.Metrics, "metrics", "", "write bounded timing and memory measurements as JSON")
	f.Uint64Var(&o.GPUMemoryMiB, "gpu-memory-mib", 1024, "Vulkan allocation budget in MiB (128–8192)")
	f.StringVar(&o.Snapshot, "snapshot", "", "export the final scene as PNG")
	f.StringVar(&o.State, "state", "", "load a document; save with Ctrl+S and on normal exit")
	f.StringVar(&o.AccessibilitySocket, "accessibility-socket", "", "stream versioned native semantics as JSON on this private Unix socket")
	f.BoolVar(&o.Fresh, "fresh", false, "load the layout but start CLI apps without reopening saved native content or recovery")
	f.DurationVar(&o.Autosave, "autosave", 5*time.Second, "background recovery checkpoint interval when --state is set (0 disables)")
	f.StringVar(&o.ApplicationProfile, "apps", "", "JSON application profile with stable IDs and separate arguments")
	f.BoolVar(&o.Terminal, "terminal", false, "open a worldr-native terminal with your shell")
	f.BoolVar(&o.Axial, "axial", false, "open the hosted AXIAL / 07 native 3D application")
	f.BoolVar(&o.Xwayland, "xwayland", false, "enable private Xwayland for X11 programs launched from native terminals")
	f.Func("app", "launch a Wayland application (repeatable); common arguments follow --", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--app needs an executable")
		}
		if len(o.Applications)+len(o.X11Applications) >= 32 {
			return fmt.Errorf("at most 32 applications can be launched")
		}
		o.Applications = append(o.Applications, value)
		orderedLaunches = append(orderedLaunches, ApplicationLaunch{ID: fmt.Sprintf("launch-%d", len(orderedLaunches)+1), Command: value})
		if o.Application == "" {
			o.Application = value
		}
		return nil
	})
	f.Func("x11-app", "launch an X11 application on private Xwayland (repeatable); common arguments follow --", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--x11-app needs an executable")
		}
		if len(o.Applications)+len(o.X11Applications) >= 32 {
			return fmt.Errorf("at most 32 applications can be launched")
		}
		o.X11Applications = append(o.X11Applications, value)
		orderedLaunches = append(orderedLaunches, ApplicationLaunch{ID: fmt.Sprintf("launch-%d", len(orderedLaunches)+1), Command: value, X11: true})
		return nil
	})
	f.BoolVar(&o.Demo, "demo", false, "play a deterministic inspection sequence")
	f.BoolVar(&o.Fullscreen, "fullscreen", false, "request a fullscreen nested window")
	f.BoolVar(&o.TakeOver, "take-over-display", false, "explicitly allow direct display with graphical session variables set")
	f.BoolVar(&o.Version, "version", false, "print version and exit")
	f.BoolVar(&o.ListDevices, "list-devices", false, "list Vulkan devices and exit")
	if err := f.Parse(args); err != nil {
		return o, err
	}
	explicitExperience := false
	f.Visit(func(flag *flag.Flag) { explicitExperience = explicitExperience || flag.Name == "experience" })
	if o.Demo && !explicitExperience {
		o.Experience = "axial"
	}
	if o.Experience != "workspace" && o.Experience != "axial" {
		return o, fmt.Errorf("unknown experience %q; choose workspace or axial", o.Experience)
	}
	if o.Demo && o.Experience != "axial" {
		return o, fmt.Errorf("--demo requires --experience=axial")
	}
	if f.NArg() != 0 && len(orderedLaunches) == 0 {
		return o, fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	o.ApplicationArgs = append([]string(nil), f.Args()...)
	if o.ApplicationProfile != "" {
		if len(orderedLaunches) > 0 || len(o.ApplicationArgs) > 0 {
			return o, fmt.Errorf("use either --apps or --app/--x11-app with arguments")
		}
		launches, err := loadApplicationProfile(o.ApplicationProfile)
		if err != nil {
			return o, err
		}
		o.Launches = launches
	} else if len(orderedLaunches) > 0 {
		for i := range orderedLaunches {
			orderedLaunches[i].Args = append([]string(nil), o.ApplicationArgs...)
		}
		o.Launches = orderedLaunches
	}
	for _, launch := range o.Launches {
		if launch.X11 {
			o.Xwayland = true
		}
	}
	if o.Backend == "wayland-client" {
		o.Backend = "nested"
	}
	switch o.Backend {
	case "auto", "nested", "headless", "vk-display", "drm":
	default:
		return o, fmt.Errorf("unknown backend %q", o.Backend)
	}
	if len(o.Outputs) > 0 && o.Backend != "auto" && o.Backend != "vk-display" && o.Backend != "drm" {
		return o, fmt.Errorf("--output applies to direct display backends")
	}
	if o.Backend == "drm" && len(o.Outputs) > 1 {
		return o, fmt.Errorf("diagnostic DRM supports one output; use vk-display")
	}
	if o.Width < 640 || o.Height < 480 || o.Width > 7680 || o.Height > 4320 {
		return o, fmt.Errorf("scene dimensions must be between 640x480 and 7680x4320")
	}
	if o.FPS < 1 || o.FPS > 240 {
		return o, fmt.Errorf("--fps must be between 1 and 240")
	}
	if o.Frames < 0 || o.Duration < 0 {
		return o, fmt.Errorf("frame and duration limits cannot be negative")
	}
	if o.Autosave < 0 {
		return o, fmt.Errorf("autosave interval cannot be negative")
	}

	if o.GPUMemoryMiB < 128 || o.GPUMemoryMiB > 8192 {
		return o, fmt.Errorf("GPU memory budget must be 128..8192 MiB")
	}
	if o.Metrics != "" {
		destination, err := filepath.Abs(o.Metrics)
		if err != nil {
			return o, err
		}
		for _, protected := range []string{o.State, recoveryPath(o.State), o.Snapshot} {
			if protected != "" {
				path, err := filepath.Abs(protected)
				if err != nil {
					return o, err
				}
				if path == destination {
					return o, fmt.Errorf("metrics must use a separate path from workspace state or snapshot")
				}
			}
		}
	}
	if o.AccessibilitySocket != "" {
		path, err := filepath.Abs(o.AccessibilitySocket)
		if err != nil {
			return o, fmt.Errorf("accessibility socket: %w", err)
		}
		if len(path) > 96 {
			return o, fmt.Errorf("accessibility socket path exceeds 96 bytes")
		}
		o.AccessibilitySocket = path
	}
	return o, nil
}

// Auto never silently takes over an existing desktop or silently falls back to
// an invisible headless session when an explicitly available host cannot open.
func chooseBackend(o Options) string {
	if o.Backend != "auto" {
		return o.Backend
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" && !o.TakeOver {
		return "nested"
	}
	if graphicalEnvironment() && !o.TakeOver {
		return "headless"
	}
	cards, _ := filepath.Glob("/dev/dri/card[0-9]*")
	if len(cards) > 0 {
		return "vk-display"
	}
	return "headless"
}

func graphicalEnvironment() bool {
	kind := os.Getenv("XDG_SESSION_TYPE")
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != "" || kind == "wayland" || kind == "x11"
}

func validateDirect(o Options, backend string) error {
	if backend != "vk-display" && backend != "drm" {
		return nil
	}
	if graphicalEnvironment() && !o.TakeOver {
		return fmt.Errorf("refusing %s in an existing graphical session; use --backend=nested, or a spare TTY with scripts/try-tty.sh", backend)
	}
	if o.Card != "" {
		base := filepath.Base(o.Card)
		if filepath.Dir(o.Card) != "/dev/dri" || !strings.HasPrefix(base, "card") || len(base) == 4 {
			return fmt.Errorf("--card needs a KMS primary node /dev/dri/cardN; render nodes cannot drive a display")
		}
		for _, c := range base[4:] {
			if c < '0' || c > '9' {
				return fmt.Errorf("invalid DRM primary node %q", o.Card)
			}
		}
		st, err := os.Stat(o.Card)
		if err != nil {
			return fmt.Errorf("DRM device: %w", err)
		}
		if st.Mode()&os.ModeCharDevice == 0 {
			return fmt.Errorf("%s is not a DRM character device", o.Card)
		}
	}
	return nil
}
