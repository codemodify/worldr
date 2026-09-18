//go:build linux && cgo

package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeapps"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestFilesTerminalHereUsesDirectoryAndPreservesHostState(t *testing.T) {
	parent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, reports := t.TempDir(), t.TempDir()
	selectedDirectory := filepath.Join(root, "a folder's $(false); $literal")
	selectedFile := filepath.Join(root, "readme.txt")
	if err := os.Mkdir(selectedDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectedFile, []byte("Terminal Here should use this file's parent folder.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The fixture receives every path as an argument or an environment value;
	// none is interpolated into shell source. Its OSC title proves real PTY
	// output reached the retained application surface, including after input.
	const script = `printf '%s\n' "$PWD" "$WAYLAND_DISPLAY" "${DISPLAY-unset}" "${WAYLAND_SOCKET-unset}" "$XDG_SESSION_TYPE" "$2" "$$" > "$1/$$"
printf '\033]2;%s\007' "$PWD"
while IFS= read -r value; do printf '\033]2;%s / %s\007' "$PWD" "$value"; done`
	const privateSocket = "/private/worldr-terminal-here-test"
	const literal = "$(false); '$HOME' `false`"
	base := append(os.Environ(), "DISPLAY=:outside-test", "WAYLAND_DISPLAY=/outside-test", "WAYLAND_SOCKET=999", "XDG_SESSION_TYPE=x11")
	manager := nativeapps.NewManager(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", script, "fixture", reports, literal}, Env: applicationEnvironment(base, privateSocket)})
	browser, err := projectapp.New(root)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	hub := newApplicationHub(browser, manager)
	t.Cleanup(func() {
		if err := hub.Close(); err != nil {
			t.Error(err)
		}
	})
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { work.SetApplications(nil); _ = work.Close() })
	work.SetApplications(hub)
	connectTerminalBrowser(browser, manager, hub, work)
	work.Draw(1440, 900)
	files := hub.Surfaces()[0]
	stroke := func(code uint32, modifiers experience.Modifiers) {
		t.Helper()
		for _, pressed := range []bool{true, false} {
			event := experience.Event{Kind: experience.KeyInput, Keycode: code, Modifiers: modifiers, Pressed: pressed}
			hub.seat(event)
			if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
				t.Fatalf("terminal action exited the host: quit=%t err=%v", quit, err)
			}
		}
	}
	poll := func() {
		t.Helper()
		if err := hub.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
	}
	wait := func(description string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			poll()
			if ready() {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s; surfaces=%+v", description, hub.Surfaces())
	}
	surface := func(key string) experience.ApplicationSurface {
		for _, current := range hub.Surfaces() {
			if current.Key == key {
				return current
			}
		}
		return experience.ApplicationSurface{}
	}
	activate := func(key string) {
		t.Helper()
		if err := work.ActivateApplication(key); err != nil {
			t.Fatal(err)
		}
		work.Draw(1440, 900)
	}
	selected := func(path string) bool {
		stroke(46, experience.ModControl|experience.ModShift)
		value, ready := browser.TakeCopy()
		return ready && value == path
	}
	assertView := func(before workspace.ViewState, focused uint64) {
		t.Helper()
		after := work.Document().View
		a, b := after.Application, before.Application
		if !work.OwnsKeyboard() || hub.focused != focused || after.Camera != before.Camera || a.Active != b.Active || a.Selected != b.Selected || a.Reading != b.Reading || a.Overview != b.Overview || a.Placing != b.Placing {
			t.Fatal("asynchronous terminal completion changed focus, camera, selection or view mode")
		}
		for _, previous := range b.Layouts {
			if previous.Key == "" {
				continue
			}
			found := false
			for _, current := range a.Layouts {
				if current.Key == previous.Key {
					found = true
					if current != previous {
						t.Fatal("opening a terminal moved or resized an existing application")
					}
				}
			}
			if !found {
				t.Fatal("opening a terminal removed an existing placement")
			}
		}
	}
	checkReport := func(directory string) int {
		t.Helper()
		entries, err := os.ReadDir(reports)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(reports, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) != 7 || lines[0] != directory {
				continue
			}
			if lines[1] != privateSocket || lines[2] != "unset" || lines[3] != "unset" || lines[4] != "wayland" || lines[5] != literal {
				t.Fatalf("terminal lost its private environment or literal argument: %q", data)
			}
			pid, err := strconv.Atoi(lines[6])
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.Stat(fmt.Sprintf("/proc/%d/cwd", pid))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.Stat(directory)
			if err != nil || !os.SameFile(actual, expected) {
				t.Fatalf("terminal cwd inode differs from requested directory %q: %v", directory, err)
			}
			return pid
		}
		t.Fatalf("no completed real-shell report for %q", directory)
		return 0
	}

	activate(files.Key)
	wait("Files selecting the directory", func() bool { return selected(selectedDirectory) })
	beforeDirectory := work.Document().View
	stroke(28, experience.ModControl|experience.ModShift)
	if len(manager.Surfaces()) != 0 {
		t.Fatal("Files launched a terminal before its asynchronous directory result was polled")
	}
	wait("selected-directory PTY output", func() bool {
		return surface("native:terminal").Title == "Native terminal / "+selectedDirectory
	})
	directoryTerminal := surface("native:terminal")
	if directoryTerminal.ID == 0 || directoryTerminal.ID == files.ID {
		t.Fatal("Files and its terminal did not receive distinct runtime routes")
	}
	assertView(beforeDirectory, files.ID)
	checkReport(selectedDirectory)
	if !selected(selectedDirectory) {
		t.Fatal("Terminal Here changed the browser's selected directory")
	}

	// An ordinary launch after Terminal Here must still inherit the original
	// process cwd, rather than retaining the previously borrowed descriptor.
	siblingKey, err := hub.LaunchApplication("terminal")
	if err != nil {
		t.Fatal(err)
	}
	wait("ordinary sibling terminal", func() bool { return surface(siblingKey).Title == "Native terminal / "+parent })
	sibling := surface(siblingKey)
	checkReport(parent)
	assertView(beforeDirectory, files.ID)
	if cwd, err := os.Getwd(); err != nil || cwd != parent {
		t.Fatalf("Terminal Here changed the host cwd: %q, %v", cwd, err)
	}

	stroke(108, 0) // Directory entries precede this single regular file.
	wait("Files selecting its regular file", func() bool { return selected(selectedFile) })
	stroke(28, experience.ModControl|experience.ModShift)
	if len(manager.Surfaces()) != 2 {
		t.Fatal("selected-file terminal bypassed the asynchronous Files worker")
	}
	// Change focus before consuming the result. Completion must preserve this
	// newer user decision instead of returning to Files or focusing the child.
	activate(sibling.Key)
	beforeFile := work.Document().View
	wait("regular-file parent-directory PTY", func() bool {
		return surface("native:terminal-3").Title == "Native terminal / "+root
	})
	fileTerminal := surface("native:terminal-3")
	assertView(beforeFile, sibling.ID)
	checkReport(root)
	if fileTerminal.ID == sibling.ID || fileTerminal.ID == directoryTerminal.ID || len(hub.Surfaces()) != 4 {
		t.Fatal("Terminal Here replaced a sibling or reused its runtime route")
	}

	hub.CloseApplication(directoryTerminal.ID)
	poll()
	if len(hub.Surfaces()) != 3 || surface(sibling.Key).ID != sibling.ID || surface(fileTerminal.Key).ID != fileTerminal.ID || hub.focused != sibling.ID {
		t.Fatal("closing the directory terminal affected an independently focused sibling")
	}
	stroke(30, 0) // The still-running sibling receives "a" and Enter.
	stroke(28, 0)
	wait("sibling PTY still accepting input", func() bool { return surface(sibling.Key).Title == "Native terminal / "+parent+" / a" })
	activate(fileTerminal.Key)
	hub.CloseApplication(sibling.ID)
	poll()
	stroke(48, 0) // The regular-file terminal receives "b" independently.
	stroke(28, 0)
	wait("file-parent PTY still accepting input", func() bool { return surface(fileTerminal.Key).Title == "Native terminal / "+root+" / b" })
	hub.CloseApplication(fileTerminal.ID)
	poll()
	if len(hub.Surfaces()) != 1 || hub.Surfaces()[0].ID != files.ID {
		t.Fatal("closing terminals removed the browser")
	}
	activate(files.Key)
	if !selected(selectedFile) {
		t.Fatal("Files lost its selection or stopped receiving input after terminal close")
	}
}
