package noteapp

import (
	"image"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
)

const (
	maxHistoryEntries = 128
	maxHistoryBytes   = 1 << 20
)

type editRecord struct {
	start                     int
	before, after             string
	beforeCaret, beforeAnchor int
	afterCaret, afterAnchor   int
}

type editor struct {
	text                     string
	caret, anchor            int
	preedit                  string
	preeditBegin, preeditEnd int32
	goalColumn               int
	revision                 uint64
	undo, redo               []editRecord
	undoBytes, redoBytes     int
}

func newEditor(text string, caret, anchor int) *editor {
	e := &editor{text: text, caret: caret, anchor: anchor, goalColumn: -1}
	e.snap()
	return e
}

func (e *editor) snap() {
	stops := runeStops(e.text)
	for _, position := range []*int{&e.caret, &e.anchor} {
		*position = max(0, min(*position, len(e.text)))
		index := sort.SearchInts(stops, *position)
		if index >= len(stops) || stops[index] != *position {
			index = max(0, index-1)
		}
		*position = stops[index]
	}
}

func (e *editor) Range() (int, int)  { return min(e.caret, e.anchor), max(e.caret, e.anchor) }
func (e *editor) Selection() string  { lo, hi := e.Range(); return e.text[lo:hi] }
func (e *editor) CancelComposition() { e.preedit, e.preeditBegin, e.preeditEnd = "", 0, 0 }
func (e *editor) Preedit() (string, int32, int32) {
	return e.preedit, e.preeditBegin, e.preeditEnd
}

func recordSize(record editRecord) int { return len(record.before) + len(record.after) }

func trimHistory(records []editRecord, size *int) []editRecord {
	for len(records) > maxHistoryEntries || *size > maxHistoryBytes {
		*size -= recordSize(records[0])
		copy(records, records[1:])
		records = records[:len(records)-1]
	}
	return records
}

func (e *editor) pushUndo(record editRecord) {
	e.undo = append(e.undo, record)
	e.undoBytes += recordSize(record)
	e.undo = trimHistory(e.undo, &e.undoBytes)
	e.redo, e.redoBytes = nil, 0
}

func boundedInsert(text string, remaining int) (string, bool) {
	if len(text) <= remaining {
		return text, false
	}
	end := 0
	for _, stop := range runeStops(text) {
		if stop > remaining {
			break
		}
		end = stop
	}
	return text[:end], true
}

func (e *editor) replace(first, last int, value string, remember bool) (bool, bool) {
	e.CancelComposition()
	if !textBoundary(e.text, first) || !textBoundary(e.text, last) || first > last || !validTextFragment(value) {
		return false, false
	}
	value, truncated := boundedInsert(value, MaxDocumentBytes-(len(e.text)-(last-first)))
	beforeCaret, beforeAnchor := e.caret, e.anchor
	before := e.text[first:last]
	next := e.text[:first] + value + e.text[last:]
	if next == e.text {
		e.caret, e.anchor = first+len(value), first+len(value)
		return false, truncated
	}
	e.text = next
	e.caret, e.anchor = first+len(value), first+len(value)
	e.goalColumn = -1
	e.revision++
	if remember {
		e.pushUndo(editRecord{start: first, before: before, after: value, beforeCaret: beforeCaret, beforeAnchor: beforeAnchor, afterCaret: e.caret, afterAnchor: e.anchor})
	}
	return true, truncated
}

func (e *editor) Insert(value string) (bool, bool) {
	lo, hi := e.Range()
	return e.replace(lo, hi, value, true)
}

func (e *editor) Undo() bool {
	e.CancelComposition()
	if len(e.undo) == 0 {
		return false
	}
	record := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.undoBytes -= recordSize(record)
	end := record.start + len(record.after)
	if end > len(e.text) || e.text[record.start:end] != record.after {
		e.undo, e.undoBytes = nil, 0
		return false
	}
	e.text = e.text[:record.start] + record.before + e.text[end:]
	e.caret, e.anchor = record.beforeCaret, record.beforeAnchor
	e.revision++
	e.redo = append(e.redo, record)
	e.redoBytes += recordSize(record)
	e.redo = trimHistory(e.redo, &e.redoBytes)
	return true
}

func (e *editor) Redo() bool {
	e.CancelComposition()
	if len(e.redo) == 0 {
		return false
	}
	record := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.redoBytes -= recordSize(record)
	end := record.start + len(record.before)
	if end > len(e.text) || e.text[record.start:end] != record.before {
		e.redo, e.redoBytes = nil, 0
		return false
	}
	e.text = e.text[:record.start] + record.after + e.text[end:]
	e.caret, e.anchor = record.afterCaret, record.afterAnchor
	e.revision++
	e.undo = append(e.undo, record)
	e.undoBytes += recordSize(record)
	e.undo = trimHistory(e.undo, &e.undoBytes)
	return true
}

func runeStops(text string) []int {
	result := nativeui.GraphemeStops(text)
	if len(result) == 0 {
		return []int{0}
	}
	return result
}

func graphemeCount(text string) int { return max(0, len(runeStops(text))-1) }

func (e *editor) moveHorizontal(direction int, extend bool) {
	lo, hi := e.Range()
	if lo != hi && !extend {
		if direction < 0 {
			e.caret, e.anchor = lo, lo
		} else {
			e.caret, e.anchor = hi, hi
		}
		e.goalColumn = -1
		return
	}
	stops := runeStops(e.text)
	index := sort.SearchInts(stops, e.caret)
	index = max(0, min(len(stops)-1, index+direction))
	e.setCaret(stops[index], extend)
	e.goalColumn = -1
}

func (e *editor) lineStarts() []int {
	starts := []int{0}
	for index, char := range e.text {
		if char == '\n' && index+1 <= len(e.text) {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func lineForPosition(starts []int, position int) int {
	return max(0, sort.Search(len(starts), func(index int) bool { return starts[index] > position })-1)
}

func (e *editor) lineBoundsAt(starts []int, line int) (int, int) {
	line = max(0, min(line, len(starts)-1))
	start, end := starts[line], len(e.text)
	if line+1 < len(starts) {
		end = starts[line+1] - 1
	}
	return start, end
}

func (e *editor) lineColumn(position int) (int, int) {
	starts := e.lineStarts()
	line := lineForPosition(starts, position)
	start, _ := e.lineBoundsAt(starts, line)
	return line, graphemeCount(e.text[start:position])
}

func (e *editor) positionAt(line, column int) int {
	starts := e.lineStarts()
	line = max(0, min(line, len(starts)-1))
	start, end := e.lineBoundsAt(starts, line)
	stops := runeStops(e.text[start:end])
	column = max(0, min(column, len(stops)-1))
	position := start + stops[column]
	if position > end {
		position = end
	}
	return position
}

func (e *editor) setCaret(position int, extend bool) {
	e.CancelComposition()
	e.caret = position
	if !extend {
		e.anchor = position
	}
	e.snap()
}

func (e *editor) SetCaretAt(line, column int, extend bool) {
	e.setCaret(e.positionAt(line, column), extend)
	e.goalColumn = -1
}

func (e *editor) moveVertical(direction int, extend bool) {
	line, column := e.lineColumn(e.caret)
	if e.goalColumn < 0 {
		e.goalColumn = column
	}
	e.setCaret(e.positionAt(line+direction, e.goalColumn), extend)
	// setCaret intentionally cancels composition; retain the visual column.
}

func (e *editor) moveLineBoundary(end, extend bool) {
	starts := e.lineStarts()
	line := lineForPosition(starts, e.caret)
	start, stop := e.lineBoundsAt(starts, line)
	if end {
		e.setCaret(stop, extend)
	} else {
		e.setCaret(start, extend)
	}
	e.goalColumn = -1
}

func (e *editor) delete(direction int) bool {
	lo, hi := e.Range()
	if lo == hi {
		stops := runeStops(e.text)
		index := sort.SearchInts(stops, lo)
		if direction < 0 && index > 0 {
			lo = stops[index-1]
		} else if direction > 0 && index+1 < len(stops) {
			hi = stops[index+1]
		}
	}
	if lo == hi {
		return false
	}
	changed, _ := e.replace(lo, hi, "", true)
	return changed
}

// Handle applies one native editor event. committed is the text produced by
// the current XKB keymap/compose translator for a physical key.
func (e *editor) Handle(event experience.Event, committed string) (changed, truncated bool) {
	switch event.Kind {
	case experience.KeyboardCancel:
		e.CancelComposition()
		return false, false
	case experience.TextCommit:
		if len(event.Text) > 4000 || !utf8.ValidString(event.Text) || strings.ContainsRune(event.Text, 0) {
			return false, false
		}
		lo, hi := e.Range()
		if uint64(event.DeleteBefore) > uint64(lo) || uint64(event.DeleteAfter) > uint64(len(e.text)-hi) {
			return false, false
		}
		first, last := lo-int(event.DeleteBefore), hi+int(event.DeleteAfter)
		return e.replace(first, last, event.Text, true)
	case experience.TextPreedit:
		if len(event.Text) > 4000 || !utf8.ValidString(event.Text) || strings.ContainsRune(event.Text, 0) {
			return false, false
		}
		if !(event.PreeditBegin == -1 && event.PreeditEnd == -1 || textBoundary(event.Text, int(event.PreeditBegin)) && textBoundary(event.Text, int(event.PreeditEnd))) {
			return false, false
		}
		e.preedit, e.preeditBegin, e.preeditEnd = event.Text, event.PreeditBegin, event.PreeditEnd
		return false, false
	}
	if event.Kind != experience.KeyInput || !event.Pressed {
		return false, false
	}
	shift := event.Modifiers.Has(experience.ModShift)
	if event.Modifiers == experience.ModControl && event.Keycode == 30 {
		e.CancelComposition()
		e.anchor, e.caret = 0, len(e.text)
		return false, false
	}
	switch event.Keycode {
	case 14:
		return e.delete(-1), false
	case 111:
		return e.delete(1), false
	case 105:
		e.moveHorizontal(-1, shift)
	case 106:
		e.moveHorizontal(1, shift)
	case 103:
		e.moveVertical(-1, shift)
	case 108:
		e.moveVertical(1, shift)
	case 102:
		e.moveLineBoundary(false, shift)
	case 107:
		e.moveLineBoundary(true, shift)
	case 28:
		if event.Modifiers&(experience.ModControl|experience.ModAlt|experience.ModSuper) == 0 {
			return e.Insert("\n")
		}
	case 15:
		if event.Modifiers&(experience.ModControl|experience.ModAlt|experience.ModSuper) == 0 {
			return e.Insert("\t")
		}
	default:
		if committed != "" {
			return e.Insert(committed)
		}
	}
	return false, false
}

func (e *editor) TextInput(contextID string, rect image.Rectangle) experience.TextInputState {
	state := experience.TextInputState{Enabled: true, ContextID: contextID, CursorRect: [4]int{rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy()}}
	lo, hi := e.Range()
	if hi-lo > 4000 {
		return experience.TextInputState{}
	}
	start := max(0, lo-(4000-(hi-lo))/2)
	end := min(len(e.text), start+4000)
	start = max(0, end-4000)
	for start < len(e.text) && !textBoundary(e.text, start) {
		start++
	}
	for !textBoundary(e.text, end) {
		end--
	}
	state.Surrounding = e.text[start:end]
	state.Cursor, state.Anchor = e.caret-start, e.anchor-start
	return state
}

func (e *editor) LineCount() int { return strings.Count(e.text, "\n") + 1 }

func (e *editor) Line(index int) (text string, start, end int) {
	starts := e.lineStarts()
	if index < 0 || index >= len(starts) {
		return "", len(e.text), len(e.text)
	}
	start, end = e.lineBoundsAt(starts, index)
	return e.text[start:end], start, end
}
