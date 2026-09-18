package host

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type TextInputState struct {
	Enabled        bool
	ContextID      string
	Surrounding    string
	Cursor, Anchor int
	CursorRect     [4]int
}

func validTextPosition(text string, pos int) bool {
	return pos >= 0 && pos <= len(text) && (pos == len(text) || utf8.RuneStart(text[pos]))
}
func (s TextInputState) validate() error {
	if !s.Enabled {
		return nil
	}
	if s.ContextID == "" || len(s.ContextID) > 512 || strings.ContainsRune(s.ContextID, 0) || !utf8.ValidString(s.ContextID) {
		return fmt.Errorf("invalid text input context")
	}
	if len(s.Surrounding) > 4000 || !utf8.ValidString(s.Surrounding) || strings.ContainsRune(s.Surrounding, 0) || !validTextPosition(s.Surrounding, s.Cursor) || !validTextPosition(s.Surrounding, s.Anchor) {
		return fmt.Errorf("text input surrounding text requires at most 4000 UTF-8 bytes and valid cursor/anchor offsets")
	}
	for _, value := range s.CursorRect {
		if value < -(1<<28) || value > 1<<28 {
			return fmt.Errorf("text input cursor rectangle is out of range")
		}
	}
	if s.CursorRect[2] < 0 || s.CursorRect[3] < 0 {
		return fmt.Errorf("text input cursor rectangle has negative extent")
	}
	return nil
}

func validTextEvent(e Event) bool {
	if e.Kind != TextCommit && e.Kind != TextPreedit {
		return true
	}
	if len(e.Text) > 4000 || !utf8.ValidString(e.Text) || strings.ContainsRune(e.Text, 0) || len(e.TextContext) > 512 || !utf8.ValidString(e.TextContext) {
		return false
	}
	if e.Kind == TextPreedit {
		return e.PreeditBegin == -1 && e.PreeditEnd == -1 || validTextPosition(e.Text, int(e.PreeditBegin)) && validTextPosition(e.Text, int(e.PreeditEnd))
	}
	return e.DeleteBefore <= 4000 && e.DeleteAfter <= 4000
}
