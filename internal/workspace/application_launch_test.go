package workspace

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

type launchingApplications struct {
	*fakeApplications
	launched []string
	surface  experience.ApplicationSurface
	err      error
}

func (f *launchingApplications) LaunchApplication(kind string) (string, error) {
	f.launched = append(f.launched, kind)
	if f.err != nil {
		return "", f.err
	}
	f.surfaces = append(f.surfaces, f.surface)
	return f.surface.Key, nil
}

func terminalLauncher(t *testing.T, apps *fakeApplications) *launchingApplications {
	t.Helper()
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	return &launchingApplications{fakeApplications: apps, surface: experience.ApplicationSurface{
		ID: 900, Key: "native:terminal-new", AppID: "worldr-terminal", Title: "New terminal", Texture: texture,
	}}
}

func launchTerminalShortcut(t *testing.T, w *Workspace) {
	t.Helper()
	if !w.Handle(terminalChord(28)) {
		t.Fatal("Ctrl+Alt+Enter was not consumed")
	}
	if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 28}) {
		t.Fatal("terminal shortcut release was not consumed")
	}
}

func TestNewTerminalShortcutCreatesAndSelectsWithoutTakingKeyboard(t *testing.T) {
	for _, mode := range []string{"no-applications", "space", "reading"} {
		t.Run(mode, func(t *testing.T) {
			count := 1
			if mode == "no-applications" {
				count = 0
			}
			w, apps := multipleApplications(t, count)
			launcher := terminalLauncher(t, apps)
			w.SetApplications(launcher)
			if mode == "reading" {
				command(t, w, Action{Kind: ToggleApplicationReading})
			}
			w.Draw(2880, 1800)
			if count > 0 {
				x, y := visibleApplicationPoint(t, w)
				pointer(w, experience.PointerDown, x, y)
				pointer(w, experience.PointerUp, x, y)
				if !w.OwnsKeyboard() {
					t.Fatal("test did not start with an application owning input")
				}
			}
			historyPosition, historyLen := w.historyPosition, len(w.history)
			camera := w.Document().View.Camera
			launchTerminalShortcut(t, w)
			if len(launcher.launched) != 1 || launcher.launched[0] != "terminal" {
				t.Fatalf("terminal shortcut did not request exactly one terminal: %v", launcher.launched)
			}
			w.Draw(2880, 1800)
			if len(w.applicationSurfaces) != count+1 || w.application.ID != 900 || w.m.applicationState.Active != launcher.surface.Key {
				t.Fatal("new terminal did not become the selected live surface")
			}
			if w.OwnsKeyboard() || w.m.applicationReading != (mode == "reading") || w.Document().View.Camera != camera {
				t.Fatal("launch changed the current view or silently acquired keyboard focus")
			}
			if w.historyPosition != historyPosition || len(w.history) != historyLen {
				t.Fatal("process launch entered document undo history")
			}
		})
	}
}

func TestNewTerminalShortcutLaunchFailurePreservesView(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	launcher := terminalLauncher(t, apps)
	launcher.err = errors.New("missing-shell: executable file not found")
	w.SetApplications(launcher)
	command(t, w, Action{Kind: ToggleApplicationReading})
	before, historyPosition := w.Document(), w.historyPosition
	saved, err := w.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	launchTerminalShortcut(t, w)
	if w.Document() != before || w.historyPosition != historyPosition || len(w.applicationSurfaces) != 1 {
		t.Fatal("failed terminal launch changed the workspace view")
	}
	if !strings.Contains(w.applicationNotice, "missing-shell") || !strings.Contains(w.applicationNotice, "could not start") {
		t.Fatalf("missing-shell failure was not explained: %q", w.applicationNotice)
	}
	assertApplicationNoticeVisible(t, w)
	withNotice, err := w.SaveState()
	if err != nil || !bytes.Equal(saved, withNotice) {
		t.Fatal("launch notice entered saved document state")
	}
	w.Update(11 * time.Second)
	if w.applicationNotice != "" || w.applicationNoticeRemaining != 0 {
		t.Fatal("transient launch failure notice did not expire")
	}
}

type rollbackLaunchingApplications struct {
	*launchingApplications
	closed []uint64
}

func (f *rollbackLaunchingApplications) CloseApplication(id uint64) {
	f.closed = append(f.closed, id)
	for i, surface := range f.surfaces {
		if surface.ID == id {
			f.surfaces = append(f.surfaces[:i], f.surfaces[i+1:]...)
			return
		}
	}
}

func TestNewTerminalShortcutRecyclesClosedSavedLayout(t *testing.T) {
	for _, state := range []string{"all-orphaned", "one-live"} {
		t.Run(state, func(t *testing.T) {
			w, apps := multipleApplications(t, MaxApplicationLayouts)
			launcher := &rollbackLaunchingApplications{launchingApplications: terminalLauncher(t, apps)}
			switch state {
			case "all-orphaned":
				apps.surfaces = nil
			case "one-live":
				apps.surfaces = apps.surfaces[:1]
			}
			w.SetApplications(launcher)
			beforeKeys := w.SavedApplicationKeys()
			historyPosition := w.historyPosition
			launchTerminalShortcut(t, w)
			if len(launcher.launched) != 1 || len(launcher.closed) != 0 {
				t.Fatalf("recyclable terminal launch was rolled back: launches=%v closed=%v", launcher.launched, launcher.closed)
			}
			if w.application.ID != launcher.surface.ID || w.m.applicationState.index(launcher.surface.Key) < 0 || w.historyPosition != historyPosition {
				t.Fatal("new terminal did not take one closed slot outside undo history")
			}
			remaining := make(map[string]bool)
			for _, key := range w.SavedApplicationKeys() {
				remaining[key] = true
			}
			removed := 0
			for _, key := range beforeKeys {
				if !remaining[key] {
					removed++
				}
			}
			if removed != 1 {
				t.Fatalf("launch recycled %d saved placements, want exactly one", removed)
			}
		})
	}
}

func TestNewTerminalShortcutFullLiveLayoutClosesOnlyNewSurface(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	launcher := &rollbackLaunchingApplications{launchingApplications: terminalLauncher(t, apps)}
	w.SetApplications(launcher)
	before, historyPosition := w.Document(), w.historyPosition
	existing := append([]experience.ApplicationSurface(nil), apps.surfaces...)
	launchTerminalShortcut(t, w)
	if len(launcher.launched) != 1 || len(launcher.closed) != 1 || launcher.closed[0] != launcher.surface.ID {
		t.Fatalf("over-capacity terminal was not closed exactly once: launches=%v closed=%v", launcher.launched, launcher.closed)
	}
	if w.Document() != before || w.historyPosition != historyPosition || len(apps.surfaces) != len(existing) {
		t.Fatal("launch rollback changed a live layout or existing window")
	}
	for i, surface := range apps.surfaces {
		if surface != existing[i] {
			t.Fatal("launch rollback closed or changed an existing surface")
		}
	}
	if !strings.Contains(w.applicationNotice, "32 live or opening windows") {
		t.Fatalf("live capacity failure was not explained: %q", w.applicationNotice)
	}
	assertApplicationNoticeVisible(t, w)
}

func TestExcessLiveApplicationsShowCapacityNotice(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	extra := apps.surfaces[0]
	extra.ID, extra.Key = 900, "extra-window"
	apps.surfaces = append(apps.surfaces, extra)
	w.Update(0)
	if !w.applicationLayoutFull || len(w.applicationSurfaces) != MaxApplicationLayouts {
		t.Fatal("incoming live surface limit was silently exceeded")
	}
	assertApplicationNoticeVisible(t, w)
	apps.surfaces = apps.surfaces[:MaxApplicationLayouts]
	w.Update(0)
	if w.applicationLayoutFull {
		t.Fatal("capacity notice stayed after the extra surface was withdrawn")
	}
}

func assertApplicationNoticeVisible(t *testing.T, w *Workspace) {
	t.Helper()
	frame := w.Draw(1440, 900)
	color := w.color(amber, 1)
	for _, vertex := range frame.Vertices {
		if vertex.X >= 266 && vertex.X < 1388 && vertex.Y >= 22 && vertex.Y < 41 &&
			vertex.R == color.R && vertex.G == color.G && vertex.B == color.B && vertex.A == 1 {
			return
		}
	}
	t.Fatal("application notice did not render visible text above the scene")
}
