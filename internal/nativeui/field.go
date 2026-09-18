package nativeui

import (
	"image"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
)

// Field stores UTF-8 byte positions at grapheme boundaries, not rune columns.
// Clipboard acquisition and IME preedit remain host responsibilities; Commit
// accepts committed text from either textinput or the host input method.
type Field struct {
	text                         string
	caret, anchor, limit, scroll int
	preedit                      string
	preeditBegin, preeditEnd     int32
}

func NewField(limit int) *Field {
	if limit <= 0 || limit > 65536 {
		limit = 4096
	}
	return &Field{limit: limit}
}
func (f *Field) Text() string      { return f.text }
func (f *Field) Caret() int        { return f.caret }
func (f *Field) Range() (int, int) { return min(f.caret, f.anchor), max(f.caret, f.anchor) }
func (f *Field) Selection() string { lo, hi := f.Range(); return f.text[lo:hi] }
func (f *Field) SelectAll()        { f.anchor = 0; f.caret = len(f.text) }
func (f *Field) Set(text string) {
	f.text = ""
	f.caret = 0
	f.anchor = 0
	f.scroll = 0
	f.Commit(text)
}
func (f *Field) Commit(text string) bool {
	f.CancelComposition()
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	lo, hi := f.Range()
	limit := f.limit
	if limit <= 0 {
		limit = 4096
	}
	remaining := limit - (len(f.text) - (hi - lo))
	if len(text) > remaining {
		end := 0
		for _, stop := range graphemeStops(text) {
			if stop > remaining {
				break
			}
			end = stop
		}
		text = text[:end]
	}
	value := f.text[:lo] + text + f.text[hi:]
	changed := value != f.text
	f.text = value
	f.caret = lo + len(text)
	f.anchor = f.caret
	f.snap()
	return changed
}
func (f *Field) snap() {
	stops := graphemeStops(f.text)
	for _, pos := range []*int{&f.caret, &f.anchor} {
		*pos = max(0, min(*pos, len(f.text)))
		i := sort.SearchInts(stops, *pos)
		if i == len(stops) {
			*pos = len(f.text)
		} else {
			*pos = stops[i]
		}
	}
}
func (f *Field) SetCaret(index int, extend bool) {
	f.CancelComposition()
	f.caret = index
	if !extend {
		f.anchor = index
	}
	f.snap()
}
func (f *Field) Handle(e experience.Event, committed string) bool {
	switch e.Kind {
	case experience.KeyboardCancel:
		f.CancelComposition()
		return false
	case experience.TextCommit:
		if len(e.Text) > 4000 || !utf8.ValidString(e.Text) || strings.ContainsRune(e.Text, 0) {
			return false
		}
		lo, hi := f.Range()
		if uint64(e.DeleteBefore) > uint64(lo) || uint64(e.DeleteAfter) > uint64(len(f.text)-hi) {
			return false
		}
		first, last := lo-int(e.DeleteBefore), hi+int(e.DeleteAfter)
		if !textBoundary(f.text, first) || !textBoundary(f.text, last) {
			return false
		}
		f.anchor, f.caret = first, last
		return f.Commit(e.Text)
	case experience.TextPreedit:
		if len(e.Text) > 4000 || !utf8.ValidString(e.Text) || strings.ContainsRune(e.Text, 0) || !(e.PreeditBegin == -1 && e.PreeditEnd == -1 || textBoundary(e.Text, int(e.PreeditBegin)) && textBoundary(e.Text, int(e.PreeditEnd))) {
			return false
		}
		changed := false
		if e.Text != "" && f.Selection() != "" {
			changed = f.Commit("")
		}
		f.preedit, f.preeditBegin, f.preeditEnd = e.Text, e.PreeditBegin, e.PreeditEnd
		return changed
	}
	if e.Kind != experience.KeyInput || !e.Pressed {
		return false
	}
	shift := e.Modifiers.Has(experience.ModShift)
	switch e.Keycode {
	case 30:
		if e.Modifiers.Has(experience.ModControl) {
			f.SelectAll()
			return false
		}
	case 14, 111:
		lo, hi := f.Range()
		if lo == hi {
			stops := graphemeStops(f.text)
			i := sort.SearchInts(stops, lo)
			if e.Keycode == 14 && i > 0 {
				lo = stops[i-1]
			}
			if e.Keycode == 111 && i+1 < len(stops) {
				hi = stops[i+1]
			}
		}
		if lo == hi {
			return false
		}
		f.anchor = lo
		f.caret = hi
		return f.Commit("")
	case 105, 106:
		direction := 1
		if e.Keycode == 105 {
			direction = -1
		}
		lo, hi := f.Range()
		if lo != hi && !shift {
			layout, err := newTextLayout(f.text, "Sans", 14, -1)
			if err == nil {
				left, right := lo, hi
				if layout.caret(left).X > layout.caret(right).X {
					left, right = right, left
				}
				layout.close()
				if direction < 0 {
					f.SetCaret(left, false)
				} else {
					f.SetCaret(right, false)
				}
				return false
			}
		}
		f.SetCaret(visualMove(f.text, f.caret, direction), shift)
		return false
	case 102:
		f.SetCaret(0, shift)
		return false
	case 107:
		f.SetCaret(len(f.text), shift)
		return false
	}
	if committed != "" {
		return f.Commit(committed)
	}
	return false
}

func textBoundary(text string, index int) bool {
	return index >= 0 && index <= len(text) && (index == len(text) || utf8.RuneStart(text[index]))
}
func (f *Field) CancelComposition()              { f.preedit = ""; f.preeditBegin = 0; f.preeditEnd = 0 }
func (f *Field) Preedit() (string, int32, int32) { return f.preedit, f.preeditBegin, f.preeditEnd }

// TextInput describes a bounded surrounding-text window without including the
// transient preedit. A selection larger than the protocol's 4000-byte maximum
// temporarily disables IME; normal physical keys and clipboard editing remain.
func (f *Field) TextInput(contextID string, rect image.Rectangle) experience.TextInputState {
	state := experience.TextInputState{Enabled: true, ContextID: contextID, CursorRect: [4]int{rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy()}}
	lo, hi := f.Range()
	if hi-lo > 4000 {
		return experience.TextInputState{}
	}
	start := max(0, lo-(4000-(hi-lo))/2)
	end := min(len(f.text), start+4000)
	start = max(0, end-4000)
	for start < len(f.text) && !textBoundary(f.text, start) {
		start++
	}
	for !textBoundary(f.text, end) {
		end--
	}
	state.Surrounding = f.text[start:end]
	state.Cursor = f.caret - start
	state.Anchor = f.anchor - start
	return state
}
