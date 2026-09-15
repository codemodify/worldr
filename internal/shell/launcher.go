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

// LaunchItem is one hardcoded launcher entry (no .desktop scan).
type LaunchItem struct {
	Label string
	Bin   string
	X11   bool
}

// Launcher is the in-shell command overlay.
type Launcher struct {
	Open   bool
	Select int
	Items  []LaunchItem
}

// Catalog is the v0 command list. X11 entries appear only when Xwayland is on.
func Catalog(xwayland bool) []LaunchItem {
	out := []LaunchItem{
		{Label: "foot", Bin: "foot", X11: false},
		{Label: "weston-simple-shm", Bin: "weston-simple-shm", X11: false},
	}
	if xwayland {
		out = append(out,
			LaunchItem{Label: "xeyes", Bin: "xeyes", X11: true},
			LaunchItem{Label: "xterm", Bin: "xterm", X11: true},
		)
	}
	return out
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

func handleLauncherKeys(ln *Launcher, ptr *input.Pointer, now time.Time, metaHeld *bool) (consumed map[uint32]bool, spawn *LaunchItem) {
	consumed = map[uint32]bool{}
	_ = now
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
	path, err := exec.LookPath(item.Bin)
	if err != nil {
		if logw != nil {
			fmt.Fprintf(logw, "launcher: %s not on PATH (%v)\n", item.Bin, err)
		}
		return err
	}
	cmd := exec.Command(path)
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
