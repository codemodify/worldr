package app

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/host"
	"unicode/utf8"
)

// textInputState bounds the protocol's surrounding-text payload without cutting
// UTF-8 or shifting deletion relative to the real caret. Selections larger than
// the protocol budget retain ordinary keyboard/clipboard editing until narrowed.
func textInputState(work experience.Experience) host.TextInputState {
	source, ok := work.(experience.TextInputSource)
	if !ok {
		return host.TextInputState{}
	}
	s := source.TextInput()
	if !s.Enabled {
		return host.TextInputState{}
	}
	text := s.Surrounding
	if !utf8.ValidString(text) || s.Cursor < 0 || s.Anchor < 0 || s.Cursor > len(text) || s.Anchor > len(text) {
		return host.TextInputState{}
	}
	for _, i := range []int{s.Cursor, s.Anchor} {
		if i < len(text) && !utf8.RuneStart(text[i]) {
			return host.TextInputState{}
		}
	}
	if len(text) > 4000 {
		lo, hi := min(s.Cursor, s.Anchor), max(s.Cursor, s.Anchor)
		if hi-lo > 4000 {
			return host.TextInputState{}
		}
		start := max(0, min(lo-(4000-(hi-lo))/2, len(text)-4000))
		for start < lo && !utf8.RuneStart(text[start]) {
			start++
		}
		end := min(len(text), start+4000)
		for end > hi && end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		if start > lo || end < hi {
			return host.TextInputState{}
		}
		s.Surrounding, s.Cursor, s.Anchor = text[start:end], s.Cursor-start, s.Anchor-start
	}
	return host.TextInputState{Enabled: true, ContextID: s.ContextID, Surrounding: s.Surrounding, Cursor: s.Cursor, Anchor: s.Anchor, CursorRect: s.CursorRect}
}
