//go:build linux && cgo

package terminal

/*
#include "terminal.h"
*/
import "C"

import "strings"

// History is copied only when the frontend opens or refreshes a history tool,
// never on the ordinary frame path. The backend already caps history at 10,000
// rows / 32 MiB and command metadata at 128 entries.
func (t *Terminal) History() (History, error) {
	if t == nil || t.closed {
		return History{}, ErrClosed
	}
	info := C.worldr_term_snapshot(t.ptr, nil)
	count := int(info.scrollback_len)
	if info.alternate == 0 {
		count += int(info.rows)
	}
	h := History{FirstLine: uint64(info.first_line), Lines: make([]TextLine, 0, count)}
	var cells [512]C.worldr_term_text_cell
	for row := 0; row < count; row++ {
		n := int(C.worldr_term_text_line(t.ptr, C.int(row), &cells[0], C.int(len(cells))))
		var text strings.Builder
		columns := make([]uint16, 0, n+1)
		end := 0
		for col := 0; col < n; col++ {
			cell := cells[col]
			if cell.width <= 0 {
				continue
			}
			if cell.chars[0] == 0 {
				text.WriteByte(' ')
				columns = append(columns, uint16(col))
			} else {
				for _, ch := range cell.chars {
					if ch == 0 {
						break
					}
					text.WriteRune(rune(ch))
					columns = append(columns, uint16(col))
				}
			}
			end = col + int(cell.width)
		}
		value := text.String()
		trimmed := strings.TrimRight(value, " ")
		removed := len(value) - len(trimmed)
		if removed > 0 {
			end = int(columns[len(columns)-removed])
			columns = columns[:len(columns)-removed]
		}
		columns = append(columns, uint16(end))
		h.Lines = append(h.Lines, TextLine{ID: h.FirstLine + uint64(row), Text: trimmed, Columns: columns})
	}
	var commands [128]C.worldr_term_command
	n := int(C.worldr_term_commands(t.ptr, &commands[0], C.int(len(commands))))
	for _, c := range commands[:n] {
		h.Commands = append(h.Commands, CommandBlock{ID: uint64(c.id), CommandLine: uint64(c.command_line), OutputLine: uint64(c.output_line), EndLine: uint64(c.end_line), CommandColumn: int(c.command_col), OutputColumn: int(c.output_col), EndColumn: int(c.end_col), Started: c.started != 0, Finished: c.finished != 0, Status: int(c.status)})
	}
	return h, nil
}

// ScrollToLine puts an existing row as close to the top as possible. Expired
// references never accidentally jump to an unrelated, reused ring-buffer row.
func (t *Terminal) ScrollToLine(line uint64) bool {
	if t == nil || t.closed {
		return false
	}
	info := C.worldr_term_snapshot(t.ptr, nil)
	first, screen := uint64(info.first_line), uint64(info.first_line)+uint64(info.scrollback_len)
	if info.alternate != 0 || line < first || line >= screen+uint64(info.rows) {
		return false
	}
	offset := 0
	if line < screen {
		offset = int(screen - line)
	}
	C.worldr_term_scroll(t.ptr, C.int(offset-int(info.scroll_offset)))
	return true
}
