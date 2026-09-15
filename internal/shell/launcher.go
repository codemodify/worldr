package shell

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/codemodify/worldr/internal/input"
)

// LaunchItem is one launcher entry (from a .desktop file or the fallback list).
type LaunchItem struct {
	Label      string
	Bin        string   // Exec argv[0]
	Args       []string // Exec argv[1:]
	Icon       string   // Icon= name or path
	IconPix    []byte
	IconW      int
	IconH      int
	IconStride int
	ID         string // desktop-file id
	X11        bool
}

// Launcher is the in-shell command overlay.
type Launcher struct {
	Open   bool
	Select int
	Items  []LaunchItem
}

// launcherGate suppresses the opening press from dismissing the overlay.
// Nested evdev+wl used to feed the same physical click twice (toggle open,
// then outside-close or toggle shut). Arm ignoreOutside on open; clear on
// pointer Release. Apps-button close is explicit after debounce.
type launcherGate struct {
	ignoreOutside bool
	lastToggle    time.Time
}

const launcherToggleDebounce = 150 * time.Millisecond

func (g *launcherGate) onOpen(now time.Time) {
	if g == nil {
		return
	}
	g.ignoreOutside = true
	g.lastToggle = now
}

func (g *launcherGate) onClose(now time.Time) {
	if g == nil {
		return
	}
	g.ignoreOutside = false
	if !now.IsZero() {
		g.lastToggle = now
	}
}

func (g *launcherGate) onRelease() {
	if g == nil {
		return
	}
	g.ignoreOutside = false
}

// handlePanelAppsClick: closed → open; open + apps → close (after debounce).
func handlePanelAppsClick(ln *Launcher, g *launcherGate, now time.Time) {
	if ln == nil {
		return
	}
	if ln.Open {
		if g != nil && !g.lastToggle.IsZero() && now.Sub(g.lastToggle) < launcherToggleDebounce {
			return
		}
		ln.Close()
		g.onClose(now)
		return
	}
	ln.Open = true
	g.onOpen(now)
}

// handleLauncherDesktopClick handles a click in the usable desktop (above the panel).
// Returns consumed=true when the overlay ate the click. Opening-press
// outside-close is ignored until Release (g.ignoreOutside).
func handleLauncherDesktopClick(ln *Launcher, g *launcherGate, x, y, screenW, screenH, panelH int) (spawn *LaunchItem, consumed bool) {
	if ln == nil || !ln.Open {
		return nil, false
	}
	deskH := usableHeight(screenH, panelH)
	if y >= deskH {
		return nil, false
	}
	card, rows := LayoutLauncher(len(ln.Items), screenW, screenH, panelH)
	idx, inside := HitLauncher(card, rows, x, y)
	if idx >= 0 && idx < len(ln.Items) {
		it := ln.Items[idx]
		ln.Select = idx
		ln.Close()
		g.onClose(time.Time{})
		return &it, true
	}
	if !inside {
		if g != nil && g.ignoreOutside {
			return nil, true
		}
		ln.Close()
		g.onClose(time.Time{})
		return nil, true
	}
	return nil, true
}

// Toggle opens or closes the overlay.
func (l *Launcher) Toggle() {
	l.Open = !l.Open
	if l.Open && l.Select >= len(l.Items) {
		l.Select = 0
	}
}

func (l *Launcher) Close() { l.Open = false }

func (l *Launcher) moveSelect(delta int) {
	n := len(l.Items)
	if n <= 0 {
		l.Select = 0
		return
	}
	l.Select = (l.Select + delta) % n
	if l.Select < 0 {
		l.Select += n
	}
}

func (l *Launcher) labels() []string {
	out := make([]string, len(l.Items))
	for i, it := range l.Items {
		out[i] = it.Label
	}
	return out
}

func (l *Launcher) drawState() *LauncherDraw {
	if l == nil {
		return nil
	}
	d := &LauncherDraw{Items: l.labels(), Select: l.Select, Icons: make([]LaunchIcon, len(l.Items))}
	for i, it := range l.Items {
		d.Icons[i] = LaunchIcon{Pix: it.IconPix, W: it.IconW, H: it.IconH, Stride: it.IconStride}
	}
	return d
}

func (l *Launcher) pick() *LaunchItem {
	if l.Select < 0 || l.Select >= len(l.Items) {
		return nil
	}
	it := l.Items[l.Select]
	return &it
}

func isLauncherToggle(code uint32, metaHeld bool) bool {
	if isEvdev(code, keyF1) {
		return true
	}
	return metaHeld && isEvdev(code, keySpace)
}

func handleLauncherKeys(ln *Launcher, ptr *input.Pointer, now time.Time, metaHeld *bool, gate *launcherGate) (consumed map[uint32]bool, spawn *LaunchItem) {
	consumed = map[uint32]bool{}
	if ln == nil || ptr == nil {
		return consumed, nil
	}
	for _, k := range ptr.Keys {
		if isMeta(k.Code) {
			*metaHeld = k.Pressed
			consumed[k.Code] = true
			continue
		}
		if !k.Pressed {
			continue
		}
		if isLauncherToggle(k.Code, *metaHeld) {
			ln.Toggle()
			if ln.Open {
				gate.onOpen(now)
			} else {
				gate.onClose(now)
			}
			consumed[k.Code] = true
			ptr.Quit = false
			continue
		}
		if !ln.Open {
			continue
		}
		switch {
		case isEvdev(k.Code, keyEsc):
			ln.Close()
			consumed[k.Code] = true
			ptr.Quit = false
		case isEvdev(k.Code, keyDown) || (isEvdev(k.Code, keyTab) && !*metaHeld):
			ln.moveSelect(1)
			consumed[k.Code] = true
		case isEvdev(k.Code, keyUp):
			ln.moveSelect(-1)
			consumed[k.Code] = true
		case isEvdev(k.Code, keyEnter):
			spawn = ln.pick()
			ln.Close()
			consumed[k.Code] = true
		}
	}
	return consumed, spawn
}

// ClientEnviron sets WAYLAND_DISPLAY / DISPLAY for a spawned client.
// Wayland apps get the worldr socket and the worldr X11 display (or DISPLAY
// is stripped so they do not attach to the host session). X11 apps get
// DISPLAY only — WAYLAND_DISPLAY is stripped so xeyes stays on XWayland.
func ClientEnviron(base []string, waylandDisplay, x11Display string, x11Client bool) []string {
	out := make([]string, 0, len(base)+2)
	for _, e := range base {
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") || strings.HasPrefix(e, "DISPLAY=") ||
			strings.HasPrefix(e, "WAYLAND_SOCKET=") {
			continue
		}
		out = append(out, e)
	}
	if x11Client {
		if x11Display != "" {
			out = append(out, "DISPLAY="+x11Display)
		}
		return out
	}
	if waylandDisplay != "" {
		out = append(out, "WAYLAND_DISPLAY="+waylandDisplay)
	}
	if x11Display != "" {
		out = append(out, "DISPLAY="+x11Display)
	}
	return out
}

// SpawnClient starts bin detached with the compositor env. Errors are written
// to logw; the shell keeps running.
func SpawnClient(item LaunchItem, waylandDisplay, x11Display string, logw io.Writer) error {
	if item.Bin == "" {
		return fmt.Errorf("launcher: empty Exec")
	}
	path, err := exec.LookPath(item.Bin)
	if err != nil {
		if logw != nil {
			fmt.Fprintf(logw, "launcher: %s not on PATH (%v)\n", item.Bin, err)
		}
		return err
	}
	cmd := exec.Command(path, item.Args...)
	cmd.Env = ClientEnviron(os.Environ(), waylandDisplay, x11Display, item.X11)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		if logw != nil {
			fmt.Fprintf(logw, "launcher: spawn %s: %v\n", item.Bin, err)
		}
		return err
	}
	if logw != nil {
		fmt.Fprintf(logw, "launcher: spawned %s pid=%d WAYLAND_DISPLAY=%s DISPLAY=%s\n",
			item.Bin, cmd.Process.Pid, waylandDisplay, x11Display)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
