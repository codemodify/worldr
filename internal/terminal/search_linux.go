//go:build linux && cgo

package terminal

/*
#include "terminal.h"
*/
import "C"

// Search copies matches from retained scrollback and the current primary
// screen, without polling the PTY or changing focus, cursor, or scroll position.
// It is literal, allows overlapping matches, and uses Unicode simple folding
// unless caseSensitive is true. Empty queries have no results. Like History,
// physical wraps remain separate and trailing blank padding is not searchable.
// Alternate-screen applications return an empty set and retain their state.
func (t *Terminal) Search(query string, caseSensitive bool) (SearchResults, error) {
	if t == nil || t.closed {
		return SearchResults{}, ErrClosed
	}
	pattern, failure, err := searchPattern(query, caseSensitive)
	if err != nil {
		return SearchResults{}, err
	}
	info := C.worldr_term_snapshot(t.ptr, nil)
	var results SearchResults
	if len(pattern) == 0 || info.alternate != 0 {
		return results, nil
	}
	// Stream rows instead of copying all history: unmatched lines are released
	// immediately and the bounded results retain only their source-row strings.
	count := int(info.scrollback_len) + int(info.rows)
	var cells [512]C.worldr_term_text_cell
	for row := 0; row < count; row++ {
		n := int(C.worldr_term_text_line(t.ptr, C.int(row), &cells[0], C.int(len(cells))))
		if !textCellsContain(cells[:n], pattern, failure, caseSensitive) {
			continue
		}
		line := plainTextLine(uint64(info.first_line)+uint64(row), cells[:n])
		if !searchLine(&results, line, pattern, failure, caseSensitive) {
			break
		}
	}
	return results, nil
}

// Reject nonmatching rows directly in the reusable C-cell buffer. Most searches
// touch thousands of rows but return few hits; this keeps those queries free of
// per-row string, rune, and column-map allocations.
func textCellsContain(cells []C.worldr_term_text_cell, pattern []rune, failure []int, caseSensitive bool) bool {
	j := 0
	for _, cell := range cells {
		if cell.width <= 0 {
			continue
		}
		for k, ch := range cell.chars {
			if ch == 0 && k != 0 {
				break
			}
			r := rune(ch)
			if r == 0 {
				r = ' '
			}
			if !caseSensitive {
				r = searchFold(r)
			}
			for j > 0 && r != pattern[j] {
				j = failure[j-1]
			}
			if r == pattern[j] {
				j++
			}
			if j == len(pattern) {
				return true
			}
		}
	}
	return false
}

// RevealMatch scrolls an unchanged, retained search row into view. It refuses
// expired or overwritten rows and alternate-screen transitions rather than
// showing a stale match against unrelated output. It never sends shell input.
// Poll after revealing to obtain the new viewport snapshot.
func (t *Terminal) RevealMatch(match SearchMatch) bool {
	if t == nil || t.closed || match.Text == "" || match.lineText == "" {
		return false
	}
	info := C.worldr_term_snapshot(t.ptr, nil)
	first := uint64(info.first_line)
	if info.alternate != 0 || match.Line < first || match.Line-first >= uint64(info.scrollback_len)+uint64(info.rows) {
		return false
	}
	var cells [512]C.worldr_term_text_cell
	n := int(C.worldr_term_text_line(t.ptr, C.int(match.Line-first), &cells[0], C.int(len(cells))))
	line := plainTextLine(match.Line, cells[:n])
	if line.Text != match.lineText || match.startRune < 0 || match.endRune >= len(line.Columns) ||
		int(line.Columns[match.startRune]) != match.StartColumn || int(line.Columns[match.endRune]) != match.EndColumn {
		return false
	}
	return t.ScrollToLine(match.Line)
}
