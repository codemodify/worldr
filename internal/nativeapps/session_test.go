package nativeapps

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/terminal"
)

func sessionDirectory(t *testing.T) (*os.File, string) {
	t.Helper()
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	path, err := resourcepath.FromFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	return directory, path
}

func terminalSurfaceID(t *testing.T, m *Manager, key string) uint64 {
	t.Helper()
	for _, surface := range m.Surfaces() {
		if surface.Key == key {
			return surface.ID
		}
	}
	t.Fatalf("missing terminal surface %q", key)
	return 0
}

func TestRestoreTerminalsPreservesExactSlotsDefaultsAndFocus(t *testing.T) {
	options := Options{Command: "configured-shell", Args: []string{"--login"}, Env: []string{"WAYLAND_DISPLAY=private-session"}}
	f := newManagerFixture(t, options)
	m := f.manager
	directory, path := sessionDirectory(t)
	for _, key := range []string{"native:terminal-4", "native:terminal-32", "native:terminal"} {
		if got, err := m.RestoreTerminal(key, directory); err != nil || got != key {
			t.Fatalf("restore %q = %q, %v", key, got, err)
		}
	}
	firstID := terminalSurfaceID(t, m, "native:terminal")
	fourthID := terminalSurfaceID(t, m, "native:terminal-4")
	m.Focus(firstID)
	if _, err := m.RestoreTerminal("native:terminal-2", directory); err != nil {
		t.Fatal(err)
	}
	if m.focused != firstID || !f.backends[2].focused || f.backends[0].focused || f.backends[1].focused || f.backends[3].focused {
		t.Fatal("restoring a terminal moved keyboard focus")
	}
	states := m.SessionTerminals()
	want := []TerminalState{{Key: "native:terminal", Directory: path}, {Key: "native:terminal-2", Directory: path}, {Key: "native:terminal-4", Directory: path}, {Key: "native:terminal-32", Directory: path}}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("snapshot compacted/reordered stable slots: got %+v, want %+v", states, want)
	}
	for _, got := range f.options {
		if got.Directory != directory || got.Command != options.Command || !reflect.DeepEqual(got.Args, options.Args) || !reflect.DeepEqual(got.Env, options.Env) {
			t.Fatalf("restore changed the configured launch: %+v", got)
		}
	}
	ordinary := launchNative(t, m)
	if ordinary.Key != "native:terminal-3" || f.options[4].Directory != nil || m.options.Directory != nil {
		t.Fatal("restore changed the next ordinary launch's slot or default directory")
	}
	lastID := ordinary.ID
	m.CloseApplication(fourthID)
	for _, state := range m.SessionTerminals() {
		if state.Key == "native:terminal-4" {
			t.Fatal("snapshot retained a dismissed terminal")
		}
	}
	if _, err := m.RestoreTerminal("native:terminal-4", directory); err != nil {
		t.Fatal(err)
	}
	reopened := terminalSurfaceID(t, m, "native:terminal-4")
	if reopened <= lastID || reopened == fourthID || m.focused != firstID {
		t.Fatal("restored slot reused a stale runtime identity or changed focus")
	}
	if len(m.RetiredTextures()) != 1 {
		t.Fatal("restoring a dismissed slot lost its texture retirement")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if len(m.SessionTerminals()) != 0 {
		t.Fatal("closed manager still published terminals")
	}
	if _, err := directory.Stat(); err != nil {
		t.Fatalf("restoring/closing terminals took ownership of the borrowed directory: %v", err)
	}
}

func TestRestoreTerminalRejectsInvalidAndDuplicateKeysBeforeSpawning(t *testing.T) {
	f := newManagerFixture(t, Options{})
	m := f.manager
	directory, _ := sessionDirectory(t)
	for _, key := range []string{"", "terminal", "native:terminal-1", "native:terminal-0", "native:terminal-02", "native:terminal-33", "native:terminal-+2", "native:terminal-2 ", "native:terminal-2/child"} {
		if _, err := m.RestoreTerminal(key, directory); err == nil {
			t.Fatalf("accepted noncanonical key %q", key)
		}
	}
	if len(f.backends) != 0 || m.next != 0 {
		t.Fatal("invalid restore launched a process or consumed an identity")
	}
	if _, err := m.RestoreTerminal("native:terminal-7", directory); err != nil {
		t.Fatal(err)
	}
	id := terminalSurfaceID(t, m, "native:terminal-7")
	if _, err := m.RestoreTerminal("native:terminal-7", directory); err == nil || len(f.backends) != 1 || m.next != id {
		t.Fatal("duplicate restore launched or replaced the occupied slot")
	}
	if _, err := m.RestoreTerminal("native:terminal-8", nil); err == nil || len(f.backends) != 1 {
		t.Fatal("nil directory reached process startup")
	}
	f.nextErr = errors.New("spawn failed")
	if _, err := m.RestoreTerminal("native:terminal-8", directory); err == nil || m.slots[7] != nil || m.next != id {
		t.Fatal("failed restore consumed a stable slot or runtime identity")
	}
	if _, err := m.RestoreTerminal("native:terminal-8", directory); err != nil {
		t.Fatalf("retry after failed restore: %v", err)
	}
	if _, err := directory.Stat(); err != nil {
		t.Fatal("restore failure closed the caller's directory", err)
	}
	m.Close()
	count := len(f.backends)
	if _, err := m.RestoreTerminal("native:terminal-9", directory); !errors.Is(err, terminal.ErrClosed) || len(f.backends) != count {
		t.Fatal("closed manager accepted a restore", err)
	}
}

func TestTerminalStateValidationIsPureAndCanonical(t *testing.T) {
	for _, key := range []string{"native:terminal", "native:terminal-2", "native:terminal-32"} {
		if err := (TerminalState{Key: key, Directory: "/a/not-currently-mounted/directory"}).Validate(); err != nil {
			t.Fatalf("manifest validation attempted to open a valid path: %v", err)
		}
	}
	for _, state := range []TerminalState{{Key: "native:terminal-1", Directory: "/tmp"}, {Key: "native:terminal-33", Directory: "/tmp"}, {Key: "native:terminal", Directory: ""}, {Key: "native:terminal", Directory: "relative"}, {Key: "native:terminal", Directory: "/tmp/../tmp"}, {Key: "native:terminal", Directory: "/bad\x00path"}, {Key: "native:terminal", Directory: "/bad\xffpath"}} {
		if err := state.Validate(); err == nil {
			t.Fatalf("accepted invalid terminal state: %+v", state)
		}
	}
}

type cwdSessionTerminal struct {
	fakeTerminal
	directory string
	err       error
}

func (f *cwdSessionTerminal) WorkingDirectory() (string, error) { return f.directory, f.err }

func TestSessionTerminalsUsesCurrentDirectoryAndKeepsExitedOutput(t *testing.T) {
	m := NewManager(Options{})
	defer m.Close()
	backend := &cwdSessionTerminal{fakeTerminal: fakeTerminal{snapshot: testTerminalSnapshot(8, 4)}}
	m.factory = func(Options) (*Provider, error) { return newProvider(backend, 8, 4) }
	directory, start := sessionDirectory(t)
	if _, err := m.RestoreTerminal("native:terminal-5", directory); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionTerminals(); len(got) != 1 || got[0].Directory != start {
		t.Fatalf("missing cwd fallback when optional backend has no value: %+v", got)
	}
	backend.directory = "/changed/shell/directory"
	if got := m.SessionTerminals(); len(got) != 1 || got[0].Directory != backend.directory {
		t.Fatalf("snapshot did not use current shell directory: %+v", got)
	}
	backend.directory, backend.err = "", os.ErrNotExist
	backend.snapshot.Exited = true
	backend.snapshot.Revision++
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	got := m.SessionTerminals()
	if len(got) != 1 || got[0].Key != "native:terminal-5" || got[0].Directory != "/changed/shell/directory" {
		t.Fatalf("exited readable terminal lost its last known directory: %+v", got)
	}
	got[0].Key = "caller-mutated"
	if m.SessionTerminals()[0].Key != "native:terminal-5" {
		t.Fatal("snapshot returned mutable manager-owned state")
	}
}
