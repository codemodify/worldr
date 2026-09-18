package nativeapps

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

type managerTerminal struct {
	fakeTerminal
	closeCount int
	inputErr   error
}

func (f *managerTerminal) Input(event experience.Event) error {
	f.fakeTerminal.Input(event)
	return f.inputErr
}
func (f *managerTerminal) Close() error {
	f.closeCount++
	return f.fakeTerminal.Close()
}

type managerFixture struct {
	manager  *Manager
	backends []*managerTerminal
	options  []Options
	nextErr  error
	inputErr error
}

func newManagerFixture(t *testing.T, options Options) *managerFixture {
	t.Helper()
	f := &managerFixture{manager: NewManager(options)}
	f.manager.factory = func(options Options) (*Provider, error) {
		if f.nextErr != nil {
			err := f.nextErr
			f.nextErr = nil
			return nil, err
		}
		backend := &managerTerminal{fakeTerminal: fakeTerminal{snapshot: testTerminalSnapshot(8, 4)}, inputErr: f.inputErr}
		f.backends = append(f.backends, backend)
		f.options = append(f.options, options)
		return newProvider(backend, 8, 4)
	}
	t.Cleanup(func() { _ = f.manager.Close() })
	return f
}

func launchNative(t *testing.T, m *Manager) experience.ApplicationSurface {
	t.Helper()
	key, err := m.LaunchApplication("terminal")
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range m.Surfaces() {
		if surface.Key == key {
			return surface
		}
	}
	t.Fatal("launch returned no mapped native surface")
	return experience.ApplicationSurface{}
}

func TestManagerIndependentTerminalsReuseSlotsWithoutReusingIDs(t *testing.T) {
	f := newManagerFixture(t, Options{})
	m := f.manager
	if len(f.backends) != 0 || len(m.Surfaces()) != 0 {
		t.Fatal("constructing a manager started a terminal")
	}
	first, second := launchNative(t, m), launchNative(t, m)
	if first.Key != "native:terminal" || second.Key != "native:terminal-2" || second.ID <= first.ID || first.Texture.ID() == second.Texture.ID() {
		t.Fatalf("terminals share identity or content: %+v %+v", first, second)
	}
	m.Focus(first.ID)
	key := experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true}
	m.Send(first.ID, key)
	m.Send(second.ID, key)
	if !f.backends[0].focused || f.backends[1].focused || len(f.backends[0].input) != 1 || len(f.backends[1].input) != 0 {
		t.Fatal("native keyboard ownership leaked between terminals")
	}
	m.Resize(second.ID, 420, 330)
	cols, rows := terminalGrid(420, 330)
	if f.backends[1].snapshot.Cols != cols || f.backends[1].snapshot.Rows != rows || f.backends[0].snapshot.Cols != 8 {
		t.Fatal("resize reached the wrong terminal")
	}
	f.backends[1].snapshot.Exited = true
	f.backends[1].snapshot.Revision++
	if err := m.Poll(); err != nil || len(m.Surfaces()) != 2 {
		t.Fatal("natural shell exit discarded readable output", err)
	}
	if !strings.Contains(m.Surfaces()[1].Title, "exited") {
		t.Fatal("exited terminal lost its status")
	}
	m.CloseApplication(first.ID)
	if !f.backends[0].closed || f.backends[1].closed || len(m.Surfaces()) != 1 {
		t.Fatal("closing one terminal affected its sibling")
	}
	// No intervening Poll: a close followed immediately by launch still reuses
	// the layout slot while retaining the old texture's retirement notification.
	third := launchNative(t, m)
	if third.Key != first.Key || third.ID <= second.ID {
		t.Fatal("reopened terminal did not separate stable slot and runtime ID")
	}
	if got := m.Surfaces()[1]; got.ID != second.ID || got.Key != second.Key || got.Texture != second.Texture {
		t.Fatal("reopening another slot changed the sibling identity")
	}
	m.CloseApplication(first.ID)
	m.Send(first.ID, key)
	if f.backends[2].closed || len(f.backends[2].input) != 0 {
		t.Fatal("stale runtime ID reached a reopened terminal")
	}
	if retired := m.Retired(); !reflect.DeepEqual(retired, []uint64{first.Texture.ID()}) || len(m.Retired()) != 0 {
		t.Fatalf("closed image was not retired exactly once: %v", retired)
	}
	m.CloseApplication(third.ID)
	if err := m.Poll(); err != nil || len(m.Surfaces()) != 1 {
		t.Fatal("polling a dismissed terminal failed", err)
	}
	if retired := m.Retired(); !reflect.DeepEqual(retired, []uint64{third.Texture.ID()}) {
		t.Fatalf("poll lost retired image: %v", retired)
	}
	if err := m.Close(); err != nil || len(m.Surfaces()) != 0 {
		t.Fatal("manager shutdown failed", err)
	}
	if retired := m.Retired(); !reflect.DeepEqual(retired, []uint64{second.Texture.ID()}) {
		t.Fatalf("shutdown lost remaining image: %v", retired)
	}
	if err := m.Close(); err != nil || len(m.Retired()) != 0 {
		t.Fatal("shutdown was not idempotent", err)
	}
	for _, backend := range f.backends {
		if backend.closeCount != 1 {
			t.Fatal("native process was not closed exactly once")
		}
	}
	if _, err := m.LaunchApplication("terminal"); !errors.Is(err, terminal.ErrClosed) {
		t.Fatalf("launch after manager shutdown: %v", err)
	}
}

func TestManagerNewTerminalsInheritSeatAndCopiedOptions(t *testing.T) {
	options := Options{Command: "example-shell", Args: []string{"original"}, Env: []string{"TEST=original"}}
	f := newManagerFixture(t, options)
	m := f.manager
	options.Args[0], options.Env[0] = "changed", "TEST=changed"
	keymap := experience.Event{Kind: experience.KeymapChanged, Keymap: "fixture keymap"}
	mods := experience.Event{Kind: experience.KeyInput, Keycode: 42, Pressed: true, Depressed: 1, Locked: 2, Group: 3}
	repeat := experience.Event{Kind: experience.KeyboardRepeatInfo, RepeatRate: 0, RepeatDelay: 350}
	m.Seat(keymap)
	m.Seat(mods)
	m.Seat(repeat)
	first := launchNative(t, m)
	mods.Kind = experience.KeyboardModifiers
	if got := f.backends[0].input; !reflect.DeepEqual(got, []experience.Event{keymap, mods, repeat}) {
		t.Fatalf("new terminal did not inherit ordered seat metadata: %+v", got)
	}
	if f.options[0].Args[0] != "original" || f.options[0].Env[0] != "TEST=original" {
		t.Fatal("manager retained caller-owned argument/environment slices")
	}
	f.options[0].Args[0], f.options[0].Env[0] = "provider mutation", "TEST=provider mutation"
	m.Focus(first.ID)
	m.Seat(experience.Event{Kind: experience.KeyboardCancel})
	second := launchNative(t, m)
	if f.backends[0].focused || f.backends[1].focused || m.focused != 0 {
		t.Fatal("keyboard cancellation retained terminal focus")
	}
	if f.options[1].Args[0] != "original" || f.options[1].Env[0] != "TEST=original" {
		t.Fatal("one provider changed subsequent terminal options")
	}
	if got := f.backends[1].input[1]; got != (experience.Event{Kind: experience.KeyboardModifiers}) {
		t.Fatalf("cancelled modifiers leaked into a new terminal: %+v", got)
	}
	newMods := experience.Event{Kind: experience.KeyboardModifiers, Depressed: 4, Latched: 8, Locked: 16, Group: 1}
	m.Seat(newMods)
	for _, backend := range f.backends {
		if got := backend.input[len(backend.input)-1]; got != newMods {
			t.Fatal("global modifiers did not reach every terminal")
		}
	}
	m.Seat(experience.Event{Kind: experience.KeymapChanged, Keymap: "replacement map"})
	third := launchNative(t, m)
	if third.ID <= second.ID || f.backends[2].input[1].Depressed != 0 || f.backends[2].input[1].Locked != 0 {
		t.Fatal("a replacement keymap reused old modifier masks")
	}
}

func TestManagerClipboardKeepsOriginalPasteRequester(t *testing.T) {
	f := newManagerFixture(t, Options{})
	m := f.manager
	first, second := launchNative(t, m), launchNative(t, m)
	for i, text := range []string{"first", "second"} {
		for j, ch := range text {
			f.backends[i].snapshot.Cells[j].Chars[0] = ch
		}
		if err := m.slots[i].provider.Poll(); err != nil {
			t.Fatal(err)
		}
		m.slots[i].provider.selection = selection{Anchor: 0, End: len(text) - 1, Active: true}
	}
	copyKey := experience.Event{Kind: experience.KeyInput, Keycode: 46, Modifiers: experience.ModControl | experience.ModShift, Pressed: true}
	pasteKey := copyKey
	pasteKey.Keycode = 47
	m.Focus(second.ID)
	m.Send(second.ID, copyKey)
	m.Focus(first.ID)
	m.Send(first.ID, copyKey)
	if text, ok := m.TakeCopy(); !ok || text != "first" {
		t.Fatalf("copy order followed slots rather than user input: %q %v", text, ok)
	}
	if _, ok := m.TakeCopy(); ok {
		t.Fatal("copy was delivered twice")
	}
	request := func(id uint64) {
		t.Helper()
		m.Focus(id)
		m.Send(id, pasteKey)
		if !m.TakePasteRequest() || m.TakePasteRequest() {
			t.Fatal("paste request did not acquire exactly one lease")
		}
	}
	request(first.ID)
	m.Focus(second.ID)
	m.Send(second.ID, pasteKey)
	if m.TakePasteRequest() {
		t.Fatal("another terminal replaced an in-flight paste requester")
	}
	if err := m.Paste("wrong target"); err != nil {
		t.Fatal(err)
	}
	if len(f.backends[0].pasted) != 0 || len(f.backends[1].pasted) != 0 {
		t.Fatal("clipboard data reached a terminal after focus changed")
	}
	request(first.ID)
	m.Focus(second.ID)
	m.Focus(first.ID)
	_ = m.Paste("focus returned too late")
	if len(f.backends[0].pasted) != 0 {
		t.Fatal("returning focus revived an invalid paste lease")
	}
	request(first.ID)
	_ = m.Paste("")
	request(first.ID)
	if err := m.Paste("allowed"); err != nil || !reflect.DeepEqual(f.backends[0].pasted, []string{"allowed"}) {
		t.Fatal("cancelled lease prevented the next valid paste", err)
	}
	request(second.ID)
	m.Send(second.ID, experience.Event{Kind: experience.KeyboardCancel})
	_ = m.Paste("keyboard cancelled")
	if len(f.backends[1].pasted) != 0 {
		t.Fatal("keyboard cancellation did not invalidate a paste")
	}
	request(first.ID)
	m.CloseApplication(first.ID)
	replacement := launchNative(t, m)
	m.Focus(replacement.ID)
	_ = m.Paste("stale window")
	if len(f.backends[2].pasted) != 0 {
		t.Fatal("a reopened slot received its former window's paste")
	}
	request(replacement.ID)
	_ = m.Paste("replacement")
	if !reflect.DeepEqual(f.backends[2].pasted, []string{"replacement"}) {
		t.Fatal("reopened terminal could not request its own paste")
	}
}

func TestManagerLaunchLimitsAndFailureCleanup(t *testing.T) {
	f := newManagerFixture(t, Options{Env: []string{}})
	m := f.manager
	if _, err := m.LaunchApplication("browser"); err == nil || len(f.backends) != 0 {
		t.Fatal("unsupported application kind started a terminal")
	}
	f.nextErr = errors.New("launch failed")
	if _, err := m.LaunchApplication("terminal"); err == nil || len(m.Surfaces()) != 0 || m.next != 0 {
		t.Fatal("failed launch consumed a native identity")
	}
	m.Seat(experience.Event{Kind: experience.KeymapChanged, Keymap: "fixture"})
	f.inputErr = errors.New("keymap rejected")
	if _, err := m.LaunchApplication("terminal"); err == nil || len(m.Surfaces()) != 0 || m.next != 0 || f.backends[0].closeCount != 1 {
		t.Fatal("failed seat initialization leaked a new terminal")
	}
	f.inputErr = nil
	for i := 0; i < maxNativeTerminals; i++ {
		launchNative(t, m)
	}
	count := len(f.backends)
	if _, err := m.LaunchApplication("terminal"); err == nil || len(f.backends) != count {
		t.Fatal("terminal limit allowed an extra process")
	}
	if f.options[1].Env == nil {
		t.Fatal("explicit empty environment changed to inherited environment")
	}
	old := m.Surfaces()[17]
	m.CloseApplication(old.ID)
	reopened := launchNative(t, m)
	if reopened.Key != "native:terminal-18" || reopened.ID <= old.ID || len(m.Surfaces()) != maxNativeTerminals {
		t.Fatal("a full manager could not reopen a vacated slot")
	}
}
