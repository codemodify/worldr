package nativeapps

import (
	"errors"
	"os"
	"testing"

	"github.com/codemodify/worldr/internal/terminal"
)

func TestDirectoryLaunchKeepsDefaultsFocusAndBorrowedDescriptor(t *testing.T) {
	f := newManagerFixture(t, Options{Command: "shell", Args: []string{"-i"}, Env: []string{"WAYLAND_DISPLAY=private-worldr"}})
	m := f.manager
	key, err := m.NextTerminalKey()
	if err != nil || key != "native:terminal" || len(f.backends) != 0 {
		t.Fatalf("preflight started a process or chose wrong slot: %s %v", key, err)
	}
	first := launchNative(t, m)
	m.Focus(first.ID)
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	next, err := m.NextTerminalKey()
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.LaunchTerminalInDirectory(directory)
	if err != nil || got != next || got != "native:terminal-2" {
		t.Fatalf("directory launch: %s %v", got, err)
	}
	if f.options[1].Directory != directory || f.options[1].Env[0] != "WAYLAND_DISPLAY=private-worldr" || f.options[1].Args[0] != "-i" {
		t.Fatal("directory launch lost requested directory, command or private environment")
	}
	if m.focused != first.ID || !f.backends[0].focused || f.backends[1].focused {
		t.Fatal("directory launch changed existing typing focus")
	}
	launchNative(t, m)
	if f.options[2].Directory != nil {
		t.Fatal("one directory launch changed later ordinary launches")
	}
	m.CloseApplication(first.ID)
	if next, err = m.NextTerminalKey(); err != nil || next != first.Key {
		t.Fatal("preflight failed to reuse closed launch slot", next, err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = directory.Stat(); err != nil {
		t.Fatal("manager closed borrowed directory", err)
	}
}

func TestDirectoryLaunchFailuresKeepSlotsAndCallerOwnership(t *testing.T) {
	f := newManagerFixture(t, Options{})
	m := f.manager
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if _, err = m.LaunchTerminalInDirectory(nil); err == nil {
		t.Fatal("nil directory launched")
	}
	file, err := os.CreateTemp(t.TempDir(), "file")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = m.LaunchTerminalInDirectory(file); err == nil {
		t.Fatal("regular file launched as directory")
	}
	closed, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err = m.LaunchTerminalInDirectory(closed); err == nil {
		t.Fatal("closed directory launched")
	}
	if len(f.backends) != 0 || m.next != 0 {
		t.Fatal("invalid directory consumed a launch identity")
	}
	f.nextErr = errors.New("launch failed")
	if _, err = m.LaunchTerminalInDirectory(directory); err == nil {
		t.Fatal("failed launch reported success")
	}
	if _, err = directory.Stat(); err != nil {
		t.Fatal("failed launch closed borrowed descriptor", err)
	}
	if next, err := m.NextTerminalKey(); err != nil || next != "native:terminal" || m.next != 0 {
		t.Fatal("failed launch consumed its slot", next, err)
	}
	if _, err = m.LaunchTerminalInDirectory(directory); err != nil {
		t.Fatal("retry failed", err)
	}
	m.Close()
	if _, err = m.NextTerminalKey(); !errors.Is(err, terminal.ErrClosed) {
		t.Fatal("closed manager offered a launch slot", err)
	}
	if _, err = m.LaunchTerminalInDirectory(directory); !errors.Is(err, terminal.ErrClosed) {
		t.Fatal("closed manager launched", err)
	}
}
