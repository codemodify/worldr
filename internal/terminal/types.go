// Package terminal embeds a real PTY and libvterm without a display protocol.
// A Terminal and its input, polling and close methods belong to one goroutine.
package terminal

import (
	"errors"
	"os"
)

var ErrClosed = errors.New("native terminal is closed")

type Options struct {
	Command string
	Args    []string
	// Env is a copied child-environment base. Nil inherits os.Environ; a
	// nonnil slice replaces it. TERM and COLORTERM are set by the backend.
	Env                    []string
	Cols, Rows, Scrollback int
	// Directory anchors the child's working directory to this open directory.
	// Open borrows it only until returning: the caller must keep it open during
	// the call and remains responsible for closing it. Nil inherits the current
	// working directory. PWD is removed from Env when Directory is provided.
	Directory *os.File
}

type Color struct{ R, G, B uint8 }

type Cell struct {
	Chars                                    [6]rune
	Width                                    int
	Foreground, Background                   Color
	Bold, Italic, Underline, Reverse, Strike bool
}

type Cursor struct {
	Row, Col int
	Visible  bool
	Blink    bool // Application-requested blinking; the frontend owns its clock.
	Shape    int  // 1 block, 2 underline, 3 bar (libvterm cursor shapes).
}

// Snapshot owns immutable row-major cells. Retaining it across future Poll
// calls is safe; an unchanged Revision can reuse the same cells.
type Snapshot struct {
	Cols, Rows                  int
	Cells                       []Cell
	Cursor                      Cursor
	Title                       string
	Revision                    uint64
	Exited                      bool
	ExitError                   string
	ScrollOffset, ScrollbackLen int
	MouseTracking               bool
	AlternateScreen             bool
	// FirstLine is the identity of the oldest retained physical text row.
	// Screen rows start at FirstLine+ScrollbackLen; IDs survive ring eviction.
	FirstLine uint64
}

// TextLine contains plain text and a rune-to-terminal-column map. Columns has
// one entry per rune plus the exclusive final column, preserving wide glyphs
// and combining marks without assuming byte offsets are screen positions.
type TextLine struct {
	ID      uint64
	Text    string
	Columns []uint16
}

type CommandBlock struct {
	ID, CommandLine, OutputLine, EndLine   uint64
	CommandColumn, OutputColumn, EndColumn int
	Started, Finished                      bool
	Status                                 int // -1 when the shell did not supply an exit status.
}

// History is a bounded immutable copy of retained primary-screen text. It
// intentionally contains no alternate-screen program text. Physical wrapped
// rows remain separate; no synthetic prompt guessing or command replay occurs.
type History struct {
	FirstLine uint64
	Lines     []TextLine
	Commands  []CommandBlock
}
