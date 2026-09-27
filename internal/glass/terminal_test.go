//go:build linux && cgo

package glass

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

func testPane(t *testing.T, script string) *terminalPane {
	t.Helper()
	p, err := newTerminalPane(terminal.Options{Command: "/bin/sh", Args: []string{"-c", script}, Cols: 80, Rows: 12, Scrollback: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func paneText(s terminal.Snapshot) string {
	var text strings.Builder
	for row := 0; row < s.Rows; row++ {
		for _, cell := range s.Cells[row*s.Cols : (row+1)*s.Cols] {
			if cell.Width == 0 {
				continue
			}
			if cell.Chars[0] == 0 {
				text.WriteByte(' ')
				continue
			}
			for _, r := range cell.Chars {
				if r == 0 {
					break
				}
				text.WriteRune(r)
			}
		}
		text.WriteByte('\n')
	}
	return text.String()
}

func waitPane(t *testing.T, p *terminalPane, ready func(terminal.Snapshot) bool) terminal.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		if ready(p.snapshot) {
			return p.snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("terminal timed out: exited=%v error=%q screen=%q", p.snapshot.Exited, p.snapshot.ExitError, paneText(p.snapshot))
	return terminal.Snapshot{}
}

func paneEvent(t *testing.T, p *terminalPane, e experience.Event) {
	t.Helper()
	if err := p.Handle(e); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalPaneRealPTYSelectionANSIAndResize(t *testing.T) {
	p := testPane(t, "stty -echo; printf '\033]2;Selection fixture\007\033[1;38;2;22;180;220m界é   \033[0m\r\nbeta   \r\nREADY\r\n'; IFS= read -r text; printf 'INPUT:%s\r\n' \"$text\"; stty size")
	first := waitPane(t, p, func(s terminal.Snapshot) bool { return strings.Contains(paneText(s), "READY") })
	if first.Title != "Selection fixture" || !first.Cells[0].Bold || first.Cells[0].Foreground != (terminal.Color{R: 22, G: 180, B: 220}) {
		t.Fatalf("ANSI title/style lost: %q %+v", first.Title, first.Cells[0])
	}
	// Clicking the continuation of a wide glyph must anchor the entire glyph;
	// combining accents must remain attached when the selection is copied.
	paneEvent(t, p, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 1, Y: 0})
	paneEvent(t, p, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 2, Y: 0})
	if text, ok := p.Copy(); !ok || text != "界é" {
		t.Fatalf("wide/combining copy: %q %v", text, ok)
	}
	paneEvent(t, p, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 0, Y: 0})
	paneEvent(t, p, experience.Event{Kind: experience.PointerMove, X: 6, Y: 1})
	paneEvent(t, p, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 6, Y: 1})
	if text, ok := p.Copy(); !ok || text != "界é\nbeta" {
		t.Fatalf("multiline copy included padding: %q %v", text, ok)
	}
	if err := p.Resize(96, 16); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Copy(); ok {
		t.Fatal("resize kept a selection of obsolete cell positions")
	}
	if err := p.Paste("pasted✓\n"); err != nil {
		t.Fatal(err)
	}
	last := waitPane(t, p, func(s terminal.Snapshot) bool { return s.Exited })
	if last.Cols != 96 || last.Rows != 16 || last.ExitError != "" || !strings.Contains(paneText(last), "INPUT:pasted✓") || !strings.Contains(paneText(last), "16 96") {
		t.Fatalf("PTY input/resize failed: %+v screen=%q", last, paneText(last))
	}
}

func TestTerminalPaneSelectionInvalidatesOnlyWhenSelectedTextChanges(t *testing.T) {
	p := testPane(t, "stty -echo; printf 'keep\r\nREADY\r\n'; IFS= read -r next; printf 'other output\r\n'; IFS= read -r next; printf '\033[Hgone\r\n'")
	waitPane(t, p, func(s terminal.Snapshot) bool { return strings.Contains(paneText(s), "READY") })
	paneEvent(t, p, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 0, Y: 0})
	paneEvent(t, p, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 3, Y: 0})
	// Bypass presentation-level Paste clearing to simulate output arriving
	// while the user holds a selection in an unrelated row.
	if err := p.term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	waitPane(t, p, func(s terminal.Snapshot) bool { return strings.Contains(paneText(s), "other output") })
	if text, ok := p.Copy(); !ok || text != "keep" {
		t.Fatalf("unrelated output cleared selection: %q %v", text, ok)
	}
	if err := p.term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	waitPane(t, p, func(s terminal.Snapshot) bool { return s.Exited })
	if _, ok := p.Copy(); ok {
		t.Fatal("selected cells changed but Copy still advertised old selection")
	}
}

func TestTerminalPaneTUIMouseAndShiftSelection(t *testing.T) {
	p := testPane(t, "stty raw -echo; printf '\033[?1049h\033[?1000h\033[?1006hREADY'; dd bs=1 count=18 2>/dev/null | od -An -tx1")
	waitPane(t, p, func(s terminal.Snapshot) bool {
		return s.AlternateScreen && s.MouseTracking && strings.Contains(paneText(s), "READY")
	})
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, Modifiers: experience.ModShift, X: 0, Y: 0},
		{Kind: experience.PointerMove, Modifiers: experience.ModShift, X: 4, Y: 0},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, Modifiers: experience.ModShift, X: 4, Y: 0},
	} {
		paneEvent(t, p, event)
	}
	if text, ok := p.Copy(); !ok || text != "READY" {
		t.Fatalf("Shift did not override TUI mouse tracking: %q %v", text, ok)
	}
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		paneEvent(t, p, experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: 2, Y: 1})
	}
	last := waitPane(t, p, func(s terminal.Snapshot) bool { return s.Exited })
	bytes := strings.Join(strings.Fields(strings.TrimPrefix(paneText(last), "READY")), " ")
	if bytes != "1b 5b 3c 30 3b 33 3b 32 4d 1b 5b 3c 30 3b 33 3b 32 6d" {
		t.Fatalf("TUI mouse bytes or Shift suppression changed: %q", bytes)
	}
}

func TestTerminalPaneFocusReportsAndScrollback(t *testing.T) {
	t.Run("focus", func(t *testing.T) {
		p := testPane(t, "stty raw -echo; printf '\033[?1004hREADY'; dd bs=1 count=6 2>/dev/null | od -An -tx1")
		waitPane(t, p, func(s terminal.Snapshot) bool { return strings.Contains(paneText(s), "READY") })
		paneEvent(t, p, experience.Event{Kind: experience.KeyboardCancel})
		paneEvent(t, p, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 0, Y: 0})
		last := waitPane(t, p, func(s terminal.Snapshot) bool { return s.Exited })
		bytes := strings.Join(strings.Fields(strings.TrimPrefix(paneText(last), "READY")), " ")
		if bytes != "1b 5b 4f 1b 5b 49" {
			t.Fatalf("focus-out/focus-in bytes: %q", bytes)
		}
	})
	t.Run("scrollback", func(t *testing.T) {
		p := testPane(t, "stty -echo; i=0; while [ $i -lt 40 ]; do printf 'line-%02d\r\n' \"$i\"; i=$((i+1)); done; printf READY; IFS= read -r next")
		waitPane(t, p, func(s terminal.Snapshot) bool { return strings.Contains(paneText(s), "READY") })
		paneEvent(t, p, experience.Event{Kind: experience.PointerScroll, ScrollY: -20})
		older := waitPane(t, p, func(s terminal.Snapshot) bool { return s.ScrollOffset > 0 })
		if strings.Contains(paneText(older), "READY") {
			t.Fatal("scrollback still showed live bottom row")
		}
		if err := p.Paste("live"); err != nil {
			t.Fatal(err)
		}
		waitPane(t, p, func(s terminal.Snapshot) bool { return s.ScrollOffset == 0 })
	})
}
