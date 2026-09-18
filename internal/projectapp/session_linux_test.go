//go:build linux

package projectapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionRoundTripRestoresNavigatedFolderAndSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "assets", "a.txt"), []byte("text preview"))
	putFile(t, filepath.Join(root, "assets", "photo.png"), nil)
	putFile(t, filepath.Join(root, "assets", "video.mp4"), nil)
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.openSelected()
	pollUntil(t, p, func() bool {
		return p.directory == "assets" && p.selected == 0 && !p.loadingFile && !p.loadingDirectory
	})
	p.selectEntry(2)
	p.Focus(1)
	state, ok := p.SessionState()
	if !ok || state != (SessionState{Root: root, Directory: "assets", Selected: "video.mp4"}) || !p.focused {
		t.Fatal("snapshot did not capture navigation independently of focus", state)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	var decoded SessionState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	restored, err := NewSession(decoded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	restored.SetOpenHandler(func(*os.File, string) error { t.Fatal("restore launched media"); return nil })
	restored.SetTerminalHandler(func(*os.File, string) error { t.Fatal("restore launched terminal"); return nil })
	pollUntil(t, restored, func() bool { return !restored.loadingDirectory })
	if restored.selected != 2 || restored.directory != "assets" || restored.focused || restored.loadingFile || !strings.Contains(restored.message, "Video file") {
		t.Fatal("restored browser did not retain passive video selection")
	}
	if got, ok := restored.SessionState(); !ok || got != state {
		t.Fatal("restored browser cannot round-trip its session", got)
	}
}

func TestSessionRejectsMissingRootAndRecoversMissingOrSymlinkChild(t *testing.T) {
	root := t.TempDir()
	if p, err := NewSession(SessionState{Root: filepath.Join(root, "missing")}); p != nil || err == nil {
		if p != nil {
			p.Close()
		}
		t.Fatal("missing root was not reported synchronously")
	}
	putFile(t, filepath.Join(root, "photo.png"), nil)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"missing/nested", "linked"} {
		t.Run(directory, func(t *testing.T) {
			p, err := NewSession(SessionState{Root: root, Directory: directory, Selected: "saved.png"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { p.Close() })
			pollUntil(t, p, func() bool { return !p.loadingDirectory && p.directory == "" })
			if !strings.Contains(p.notice, "project root") || p.focused || p.selected < 0 {
				t.Fatal("missing/linked saved folder did not recover visibly", p.notice)
			}
		})
	}
}

func TestSessionSnapshotsRenamedRootButOmitsUnlinkedAnchor(t *testing.T) {
	base := t.TempDir()
	original, moved := filepath.Join(base, "original"), filepath.Join(base, "moved")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := New(original)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	state, ok := p.SessionState()
	if !ok || state.Root != moved {
		t.Fatal("snapshot saved the replacement project instead of the open anchor", state)
	}
	restored, err := NewSession(state)
	if err != nil {
		t.Fatal(err)
	}
	restored.Close()
	if err := os.Remove(moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(moved, 0700); err != nil {
		t.Fatal(err)
	}
	if state, ok := p.SessionState(); ok {
		t.Fatal("unlinked root was saved as a replacement directory", state)
	}
}

func TestSessionRejectsSavedRootReplacedBySymlinkButExplicitRootStillAllowsIt(t *testing.T) {
	base := t.TempDir()
	root, replacement := filepath.Join(base, "project"), filepath.Join(base, "replacement")
	for _, directory := range []string{root, replacement} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	putFile(t, filepath.Join(root, "original.png"), nil)
	putFile(t, filepath.Join(replacement, "replacement.png"), nil)
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	state, ok := p.SessionState()
	p.Close()
	if !ok || state.Root != root {
		t.Fatal("fixture did not save the canonical root")
	}
	if err := os.Rename(root, root+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, root); err != nil {
		t.Fatal(err)
	}
	if restored, err := NewSession(state); err == nil || restored != nil {
		if restored != nil {
			restored.Close()
		}
		t.Fatal("session restore followed a replacement root symlink")
	}
	explicit, err := New(root)
	if err != nil {
		t.Fatal("explicitly selected symlink root was rejected", err)
	}
	t.Cleanup(func() { explicit.Close() })
	pollUntil(t, explicit, func() bool { return !explicit.loadingDirectory })
	if explicit.root != replacement || len(explicit.entries) != 1 || explicit.entries[0].name != "replacement.png" {
		t.Fatal("explicit symlink root did not preserve normal New behavior")
	}
}
