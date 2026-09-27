//go:build linux && cgo

package terminal

import (
	"errors"
	"strings"
	"testing"
)

func TestPaletteRecolorsIndexedScreenAndHistoryWithoutChangingTruecolor(t *testing.T) {
	// I/T are indexed/truecolor foreground blue, B/D indexed/truecolor
	// background blue, L bright indexed blue, X the fixed 256-color cube.
	script := "stty -echo; printf '\033[34mI\033[38;2;0;0;255mT\033[0m\033[44mB\033[48;2;0;0;255mD\033[0m\033[94mL\033[38;5;21mX\033[0m\r\nREADY\r\n'; IFS= read -r next; i=0; while [ $i -lt 10 ]; do printf 'history-%d\r\n' \"$i\"; i=$((i+1)); done; printf SCROLLED; IFS= read -r next"
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", script}, Cols: 40, Rows: 6, Scrollback: 32})
	before := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
	var colors [16]Color
	for i := range colors {
		colors[i] = Color{R: uint8(i * 11), G: uint8(i * 13), B: uint8(i * 15)}
	}
	colors[4], colors[12] = Color{R: 78, G: 159, B: 255}, Color{R: 150, G: 190, B: 255}
	if err := term.SetPalette(colors); err != nil {
		t.Fatal(err)
	}
	changed := pollTerminal(t, term, func(s Snapshot) bool { return s.Revision > before.Revision })
	assertPaletteCells := func(s Snapshot, blue, bright Color) {
		t.Helper()
		if s.Cells[0].Chars[0] != 'I' || s.Cells[1].Chars[0] != 'T' {
			t.Fatalf("palette fixture moved: %q", screenText(s))
		}
		if s.Cells[0].Foreground != blue || s.Cells[2].Background != blue || s.Cells[4].Foreground != bright {
			t.Fatalf("indexed palette not applied: fg=%+v bg=%+v bright=%+v", s.Cells[0].Foreground, s.Cells[2].Background, s.Cells[4].Foreground)
		}
		for _, c := range []Color{s.Cells[1].Foreground, s.Cells[3].Background, s.Cells[5].Foreground} {
			if c != (Color{B: 255}) {
				t.Fatalf("explicit truecolor or 256-color cube was recolored: %+v", c)
			}
		}
		if s.Cells[2].Foreground != before.Cells[2].Foreground || s.Cells[0].Background != before.Cells[0].Background {
			t.Fatal("palette changed default foreground/background")
		}
	}
	assertPaletteCells(changed, colors[4], colors[12])
	if before.Cells[0].Foreground == changed.Cells[0].Foreground {
		t.Fatal("indexed blue did not change or earlier immutable snapshot was mutated")
	}
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "SCROLLED") })
	term.Scroll(100)
	history := pollTerminal(t, term, func(s Snapshot) bool { return s.ScrollOffset > 0 })
	assertPaletteCells(history, colors[4], colors[12])
	colors[4], colors[12] = Color{R: 104, G: 170, B: 250}, Color{R: 176, G: 206, B: 255}
	if err := term.SetPalette(colors); err != nil {
		t.Fatal(err)
	}
	recoloredHistory := pollTerminal(t, term, func(s Snapshot) bool { return s.Revision > history.Revision })
	assertPaletteCells(recoloredHistory, colors[4], colors[12])
	if recoloredHistory.ScrollOffset != history.ScrollOffset {
		t.Fatal("palette change moved the scrollback view")
	}
	// A second terminal retains its original palette and never inherits this
	// frontend's theme, including when launched after SetPalette.
	other := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf '\033[34mI\033[0m'; IFS= read -r next"}, Cols: 40, Rows: 6})
	original := pollTerminal(t, other, func(s Snapshot) bool { return s.Cells[0].Chars[0] == 'I' })
	if original.Cells[0].Foreground != before.Cells[0].Foreground {
		t.Fatal("SetPalette changed unrelated terminal defaults")
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if err := term.SetPalette(colors); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed palette update: %v", err)
	}
	var missing *Terminal
	if err := missing.SetPalette(colors); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil palette update: %v", err)
	}
}
