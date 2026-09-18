package nativeapps

import (
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

type fakeTerminal struct {
	snapshot terminal.Snapshot
	input    []experience.Event
	focused  bool
	scroll   []int
	pasted   []string
	closed   bool
}

func (f *fakeTerminal) Poll() (terminal.Snapshot, error) { return f.snapshot, nil }
func (f *fakeTerminal) Resize(cols, rows int) error {
	f.snapshot = testTerminalSnapshot(cols, rows)
	return nil
}
func (f *fakeTerminal) Input(event experience.Event) error {
	f.input = append(f.input, event)
	return nil
}
func (f *fakeTerminal) Focus(focused bool)      { f.focused = focused }
func (f *fakeTerminal) Scroll(lines int)        { f.scroll = append(f.scroll, lines) }
func (f *fakeTerminal) Paste(text string) error { f.pasted = append(f.pasted, text); return nil }
func (f *fakeTerminal) Close() error            { f.closed = true; return nil }

func testProvider(t *testing.T) (*Provider, *fakeTerminal) {
	t.Helper()
	fake := &fakeTerminal{snapshot: testTerminalSnapshot(30, 8)}
	p, err := newProvider(fake, 30, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, fake
}

func TestNativeTerminalSelectionClipboardAndInputOwnership(t *testing.T) {
	p, backend := testProvider(t)
	for i, ch := range []rune("hello") {
		backend.snapshot.Cells[i].Chars[0] = ch
	}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	key := experience.Event{Kind: experience.KeyInput, Keycode: 18, Key: experience.KeyE, Pressed: true}
	p.Seat(key)
	if len(backend.input) != 1 || backend.input[0].Kind != experience.KeyboardModifiers {
		t.Fatal("global seat sent a physical key to an unfocused terminal")
	}
	p.Send(1, key)
	if len(backend.input) != 1 {
		t.Fatal("unfocused provider forwarded a key")
	}
	p.Focus(1)
	p.Send(1, key)
	if backend.input[len(backend.input)-1] != key {
		t.Fatal("focused raw key changed")
	}
	p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: contentLeft + 1, Y: contentTop + 1})
	p.Send(1, experience.Event{Kind: experience.PointerMove, X: contentLeft + 4*cellWidth + 1, Y: contentTop + 1})
	p.Send(1, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: contentLeft + 4*cellWidth + 1, Y: contentTop + 1})
	if p.Selection() != "hello" {
		t.Fatalf("native selection %q", p.Selection())
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 46, Modifiers: experience.ModControl | experience.ModShift, Pressed: true})
	text, ok := p.TakeCopy()
	if !ok || text != "hello" {
		t.Fatal("copy request did not contain selected PTY text")
	}
	if _, ok := p.TakeCopy(); ok {
		t.Fatal("copy request was not drained")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 47, Modifiers: experience.ModControl | experience.ModShift, Pressed: true})
	if !p.TakePasteRequest() || p.TakePasteRequest() {
		t.Fatal("paste request was not queued once")
	}
	if err := p.Paste("bracketed input\n"); err != nil || backend.pasted[0] != "bracketed input\n" {
		t.Fatal("paste did not reach terminal backend", err)
	}
	p.Send(1, experience.Event{Kind: experience.KeyboardCancel})
	if p.focused || backend.focused {
		t.Fatal("keyboard loss left native terminal focused")
	}
}

func TestNativeTerminalMouseCoordinatesHistoryAndResize(t *testing.T) {
	p, backend := testProvider(t)
	backend.snapshot.MouseTracking = true
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	p.Send(1, experience.Event{Kind: experience.PointerDown, ButtonCode: 272, X: contentLeft + 3*cellWidth + 5, Y: contentTop + 2*cellHeight + 3})
	got := backend.input[len(backend.input)-1]
	if got.X != 3.5 || got.Y < 2 || got.Y >= 3 {
		t.Fatalf("TUI pointer was not mapped to cell coordinates: %+v", got)
	}
	count := len(backend.input)
	p.Send(1, experience.Event{Kind: experience.PointerDown, ButtonCode: 272, Modifiers: experience.ModShift, X: contentLeft + 1, Y: contentTop + 1})
	if len(backend.input) != count || !p.selecting {
		t.Fatal("Shift selection was forwarded to mouse-tracking TUI")
	}
	p.Send(1, experience.Event{Kind: experience.PointerCancel})
	p.Send(1, experience.Event{Kind: experience.PointerScroll, Modifiers: experience.ModShift, ScrollY: -15, X: contentLeft + 1, Y: contentTop + 1})
	if len(backend.scroll) != 1 || backend.scroll[0] != 3 {
		t.Fatal("scroll did not navigate native history")
	}
	id := p.Surfaces()[0].Texture.ID()
	p.Resize(1, 960, 600)
	if p.err != nil {
		t.Fatal(p.err)
	}
	cols, rows := terminalGrid(960, 600)
	if backend.snapshot.Cols != cols || backend.snapshot.Rows != rows || p.Surfaces()[0].Texture.ID() != id {
		t.Fatal("native resize lost texture identity or PTY grid")
	}
	backend.snapshot.Exited = true
	backend.snapshot.ExitError = "exit status 7"
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil || len(p.Surfaces()) != 1 {
		t.Fatal("process exit discarded readable terminal contents", err)
	}
}

func TestNativeSelectionPreservesCombiningMarksAndSkipsWideContinuation(t *testing.T) {
	p, backend := testProvider(t)
	backend.snapshot.Cells[0].Chars = [6]rune{'e', '\u0301'}
	backend.snapshot.Cells[1].Chars = [6]rune{'界'}
	backend.snapshot.Cells[1].Width = 2
	backend.snapshot.Cells[2].Width = 0
	backend.snapshot.Cells[3].Chars = [6]rune{'!'}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	p.selection = selection{Anchor: 0, End: 3, Active: true}
	if text := p.Selection(); text != "e\u0301界!" {
		t.Fatalf("selected Unicode text was corrupted: %q", text)
	}
}

func TestNativeWindowCloseWithdrawsSurfaceAndRetiresTexture(t *testing.T) {
	p, backend := testProvider(t)
	id := p.Surfaces()[0].Texture.ID()
	p.CloseApplication(999)
	if backend.closed {
		t.Fatal("unknown window closed the native shell")
	}
	p.CloseApplication(1)
	if !backend.closed || len(p.Surfaces()) != 0 {
		t.Fatal("explicit close kept the native shell or surface alive")
	}
	if err := p.Poll(); err != nil {
		t.Fatal("closed window caused workspace polling failure", err)
	}
	retired := p.Retired()
	if len(retired) != 1 || retired[0] != id || len(p.Retired()) != 0 {
		t.Fatal("native GPU image was not retired exactly once")
	}
	p.CloseApplication(1)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
