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
	"github.com/codemodify/worldr/internal/workspace"
)

func TestNativeTerminalOverviewKeyboardWorkflowDoesNotLeakNavigation(t *testing.T) {
	dir := t.TempDir()
	// Every PTY writes only synthetic canaries in this temporary directory.
	// Appending every completed input line exposes even an unwanted blank Enter.
	script := `stty -echo
printf '\033]2;overview-shell:%s\007' "$$"
: > "$1/ready-$$"
while IFS= read -r line; do printf '%s\n' "$line" >> "$1/input-$$"; done`
	manager := nativeapps.NewManager(nativeapps.Options{Command: "/bin/sh", Args: []string{"-c", script, "worldr-overview-test", dir}})
	defer manager.Close()
	firstKey, err := manager.LaunchApplication("terminal")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := manager.LaunchApplication("terminal")
	if err != nil {
		t.Fatal(err)
	}
	work, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	hub := newApplicationHub(manager)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	poll := func() {
		t.Helper()
		if err := manager.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
	}
	until := func(reason string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			poll()
			if ready() {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", reason)
	}
	type shell struct {
		id     uint64
		pid    int
		canary string
	}
	shells := map[string]shell{}
	until("both shell prompts", func() bool {
		for _, surface := range hub.Surfaces() {
			const prefix = "Native terminal / overview-shell:"
			if !strings.HasPrefix(surface.Title, prefix) {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimPrefix(surface.Title, prefix))
			if err != nil || pid <= 1 {
				t.Fatalf("invalid synthetic shell title %q", surface.Title)
			}
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("ready-%d", pid))); err == nil {
				shells[surface.Key] = shell{surface.ID, pid, filepath.Join(dir, fmt.Sprintf("input-%d", pid))}
			}
		}
		return len(shells) == 2
	})
	first, second := shells[firstKey], shells[secondKey]
	if first.id == 0 || second.id == 0 || first.id == second.id || first.pid == second.pid {
		t.Fatal("independent live surface IDs did not map to independent shell PIDs")
	}
	if err := work.Dispatch(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: secondKey}); err != nil {
		t.Fatal(err)
	}
	work.Draw(1440, 900) // CPU layout only; this test does not open a GPU device.
	dispatch := func(event experience.Event) {
		t.Helper()
		hub.seat(event)
		if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
			t.Fatalf("workspace keyboard workflow requested exit: quit=%v err=%v", quit, err)
		}
	}
	stroke := func(code uint32, key experience.Key, mods experience.Modifiers, depressed uint32) {
		t.Helper()
		for _, down := range []bool{true, false} {
			dispatch(experience.Event{Kind: experience.KeyInput, Keycode: code, Key: key, Pressed: down, Modifiers: mods, Depressed: depressed})
		}
	}
	assertNoInput := func(reason string) {
		t.Helper()
		end := time.Now().Add(60 * time.Millisecond)
		for {
			poll()
			for key, shell := range shells {
				if bytes, err := os.ReadFile(shell.canary); err == nil {
					t.Fatalf("%s sent unintended input to %s (PID%d): %q", reason, key, shell.pid, bytes)
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if time.Now().After(end) {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	stroke(24, experience.KeyO, experience.ModControl|experience.ModAlt, 12)
	if !work.Document().View.Application.Overview || work.OwnsKeyboard() {
		t.Fatal("global overview chord did not enter workspace navigation")
	}
	stroke(105, experience.KeyLeft, 0, 0)
	if work.Document().View.Application.Active != firstKey || hub.focused != 0 {
		t.Fatal("overview arrow did not select the first live terminal without focusing it")
	}
	stroke(28, experience.KeyUnknown, 0, 0)
	if work.Document().View.Application.Overview || work.OwnsKeyboard() || work.Document().View.Application.Reading {
		t.Fatal("overview Enter did not return to spatial view without typing focus")
	}
	assertNoInput("overview navigation and return")
	stroke(28, experience.KeyUnknown, 0, 0)
	if !work.Document().View.Application.Reading || !work.OwnsKeyboard() || hub.focused != first.id || work.Document().View.Application.Active != firstKey {
		t.Fatal("fresh Enter did not explicitly read/focus the selected live surface")
	}
	assertNoInput("explicit activation press and release")
	for _, code := range []uint32{17, 24, 19, 38, 32, 19, 28} { // worldr + Enter
		stroke(code, experience.KeyUnknown, 0, 0)
	}
	until("selected shell receiving only the typed line", func() bool {
		data, err := os.ReadFile(first.canary)
		if os.IsNotExist(err) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		// The shell creates its output file before printf writes the line.
		if len(data) < len("worldr\n") && strings.HasPrefix("worldr\n", string(data)) {
			return false
		}
		if string(data) != "worldr\n" {
			t.Fatalf("selected shell received navigation/activation input: %q", data)
		}
		return true
	})
	if data, err := os.ReadFile(second.canary); err == nil || !os.IsNotExist(err) {
		t.Fatalf("unselected shell received input: %q err=%v", data, err)
	}
	if surfaces := hub.Surfaces(); len(surfaces) != 2 || hub.focused != first.id {
		t.Fatal("keyboard workflow lost a terminal or changed its live focus route")
	}
}
