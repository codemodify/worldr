package nativeapps

import (
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/terminal"
	"github.com/codemodify/worldr/internal/textinput"
)

const (
	maxBookmarks     = 64
	maxPins          = 16
	maxPinnedBytes   = 64 << 10
	maxSearchMatches = 4096
)

type historyBackend interface {
	History() (terminal.History, error)
	ScrollToLine(uint64) bool
}
type textMatch struct {
	line        uint64
	first, last int
}
type terminalBookmark struct {
	line  uint64
	label string
	text  string
}
type terminalPin struct {
	id          uint64
	title, text string
}
type historyRow struct {
	text, kind string
	id         uint64
	line       uint64
}
type terminalTools struct {
	text                           *textinput.Translator
	mode, query, message           string
	history                        terminal.History
	revision                       uint64
	nextRefresh                    time.Time
	matches                        []textMatch
	match                          int
	bookmarks                      []terminalBookmark
	pins                           []terminalPin
	tasks                          []terminalTask
	nextPin, nextTask              uint64
	expanded                       map[uint64]bool
	rows                           []historyRow
	selected, offset               int
	owned                          [768]bool
	field                          *nativeui.Field
	controls                       nativeui.Controller
	toolbarActive                  bool
	rowsDirty                      bool
	viewCells                      []terminal.Cell
	viewLabels                     []string
	viewCols, viewRows, viewOffset int
	epoch                          uint64
}

func (p *Provider) setTool(mode string) {
	if mode != "" && p.snapshot.AlternateScreen {
		p.tools.message = "History tools resume when the full-screen app exits"
		return
	}
	if mode == p.tools.mode && mode != "find" {
		mode = ""
	}
	p.tools.mode, p.tools.message = mode, ""
	p.tools.field.CancelComposition()
	p.tools.toolbarActive = false
	p.tools.epoch++
	p.tools.text.Handle(experience.Event{Kind: experience.KeyboardCancel})
	p.tools.selected, p.tools.offset = 0, 0
	p.tools.rowsDirty = true
	p.selection = selection{}
	p.selecting = false
	// This also cancels backend-generated key repeat while a field owns input.
	p.terminal.Focus(p.focused && mode == "")
	if mode != "" {
		p.refreshHistory(true)
	} else {
		p.tools.history = terminal.History{}
		p.tools.matches = nil
		p.tools.rows = nil
		p.tools.viewCells = nil
		p.tools.viewLabels = nil
		p.tools.revision = 0
	}
	if mode == "find" {
		p.tools.field.Set(p.tools.query)
		p.tools.field.SelectAll()
		p.searchHistory(true)
	}
	p.syncToolControls()
}

func (p *Provider) refreshHistory(force bool) {
	if !force && (p.tools.revision == p.snapshot.Revision || p.now().Before(p.tools.nextRefresh)) {
		return
	}
	backend, ok := p.terminal.(historyBackend)
	if !ok {
		p.tools.message = "This terminal backend does not expose scrollback"
		return
	}
	h, err := backend.History()
	if err != nil {
		p.tools.message = err.Error()
		return
	}
	p.tools.history = h
	p.tools.rowsDirty = true
	p.refreshTaskStatuses(h)
	// Expanded command IDs are metadata too; forget them when the backend's
	// bounded command ring no longer retains that command.
	retained := make(map[uint64]bool, len(h.Commands))
	for _, c := range h.Commands {
		retained[c.ID] = true
	}
	for id := range p.tools.expanded {
		if id>>63 == 0 && !retained[id] {
			delete(p.tools.expanded, id)
		}
	}
	p.tools.revision = p.snapshot.Revision
	p.tools.nextRefresh = p.now().Add(200 * time.Millisecond)
	if p.tools.mode == "find" {
		p.searchHistory(false)
	}
}

func searchLines(lines []terminal.TextLine, query string) []textMatch {
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	var result []textMatch
	for _, line := range lines {
		text := strings.ToLower(line.Text)
		for start := 0; start < len(text); {
			i := strings.Index(text[start:], query)
			if i < 0 {
				break
			}
			i += start
			first := utf8.RuneCountInString(text[:i])
			last := first + utf8.RuneCountInString(query)
			if first < len(line.Columns) && last < len(line.Columns) {
				result = append(result, textMatch{line.ID, int(line.Columns[first]), max(int(line.Columns[first])+1, int(line.Columns[last]))})
				if len(result) == maxSearchMatches {
					return result
				}
			}
			start = i + len(query)
		}
	}
	return result
}

func (p *Provider) searchHistory(jump bool) {
	var old textMatch
	had := p.tools.match >= 0 && p.tools.match < len(p.tools.matches)
	if had {
		old = p.tools.matches[p.tools.match]
	}
	p.tools.matches = searchLines(p.tools.history.Lines, p.tools.query)
	p.tools.match = max(0, len(p.tools.matches)-1)
	if had && !jump {
		for i, hit := range p.tools.matches {
			if hit == old {
				p.tools.match = i
				break
			}
		}
	}
	if jump {
		p.jumpMatch(0)
	}
}

func (p *Provider) jumpMatch(delta int) {
	n := len(p.tools.matches)
	if n == 0 {
		return
	}
	p.tools.match = (p.tools.match + delta + n) % n
	if backend, ok := p.terminal.(historyBackend); ok {
		backend.ScrollToLine(p.tools.matches[p.tools.match].line)
	}
}

func (p *Provider) findText(text string) {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	if text == "" {
		return
	}
	p.tools.field.Commit(text)
	p.tools.query = p.tools.field.Text()
	p.searchHistory(true)
}

func (p *Provider) bookmark() {
	if p.snapshot.AlternateScreen {
		p.tools.message = "Bookmarks belong to shell history"
		return
	}
	p.refreshHistory(true)
	line := p.snapshot.FirstLine + uint64(max(0, p.snapshot.ScrollbackLen-p.snapshot.ScrollOffset))
	if p.selection.Active {
		line += uint64(max(0, p.selection.Anchor) / p.snapshot.Cols)
	} else if p.snapshot.ScrollOffset == 0 {
		line += uint64(max(0, p.snapshot.Cursor.Row))
	}
	for i, b := range p.tools.bookmarks {
		if b.line == line {
			p.tools.bookmarks = append(p.tools.bookmarks[:i], p.tools.bookmarks[i+1:]...)
			p.tools.message = "Bookmark removed"
			p.tools.rowsDirty = true
			return
		}
	}
	label := lineText(p.tools.history, line)
	if len(p.tools.bookmarks) == maxBookmarks {
		p.tools.bookmarks = p.tools.bookmarks[1:]
	}
	p.tools.bookmarks = append(p.tools.bookmarks, terminalBookmark{line, clippedText(label, 100), label})
	p.tools.rowsDirty = true
	p.tools.message = "Bookmarked line — Ctrl+Shift+B opens bookmarks"
}

func lineText(h terminal.History, id uint64) string {
	if id < h.FirstLine || id-h.FirstLine >= uint64(len(h.Lines)) {
		return "[history expired]"
	}
	return h.Lines[id-h.FirstLine].Text
}

// textRange includes only cells inside explicit OSC boundaries. It cannot
// include a following prompt even when command output lacks a final newline.
func textRange(h terminal.History, first uint64, firstCol int, last uint64, lastCol int, limit int) string {
	if last < first {
		return ""
	}
	var out strings.Builder
	if first < h.FirstLine {
		out.WriteString("[earlier output expired]\n")
		first = h.FirstLine
		firstCol = 0
	}
	if len(h.Lines) == 0 {
		return out.String()
	}
	last = min(last, h.FirstLine+uint64(len(h.Lines)-1))
	for id := first; id <= last; id++ {
		line := h.Lines[id-h.FirstLine]
		var row strings.Builder
		for i, r := range []rune(line.Text) {
			if i >= len(line.Columns) {
				break
			}
			col := int(line.Columns[i])
			if id == first && col < firstCol || id == last && col >= lastCol {
				continue
			}
			row.WriteRune(r)
		}
		if id > first {
			out.WriteByte('\n')
		}
		out.WriteString(row.String())
		if out.Len() > limit {
			return truncateResult(out.String(), limit)
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

func truncateResult(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	suffix := "\n[truncated]"
	if limit < len(suffix) {
		suffix = ""
	}
	cut := max(0, limit-len(suffix))
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + suffix
}

func commandTitle(h terminal.History, c terminal.CommandBlock) string {
	if !c.Started {
		return "Editing command…"
	}
	value := textRange(h, c.CommandLine, c.CommandColumn, c.OutputLine, c.OutputColumn, 512)
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value == "" {
		value = "Shell command"
	}
	return value
}

func commandOutput(h terminal.History, c terminal.CommandBlock) string {
	last, col := c.EndLine, c.EndColumn
	if !c.Finished {
		last = h.FirstLine + uint64(max(0, len(h.Lines)-1))
		col = 512
	}
	return textRange(h, c.OutputLine, c.OutputColumn, last, col, maxPinnedBytes)
}

func (p *Provider) selectedToolText() (string, string) {
	if p.tools.selected < 0 || p.tools.selected >= len(p.tools.rows) {
		return "", ""
	}
	row := p.tools.rows[p.tools.selected]
	switch row.kind {
	case "command":
		for _, c := range p.tools.history.Commands {
			if c.ID == row.id {
				return commandTitle(p.tools.history, c), commandOutput(p.tools.history, c)
			}
		}
	case "pin":
		for _, pin := range p.tools.pins {
			if pin.id == row.id {
				return pin.title, pin.text
			}
		}
	case "bookmark":
		return row.text, lineText(p.tools.history, row.line)
	case "task":
		for _, task := range p.tools.tasks {
			if task.id == row.id {
				return task.Name, task.Command
			}
		}
	}
	return "", row.text
}

func (p *Provider) pinResult() {
	var title, text string
	if p.tools.mode != "" && p.tools.mode != "find" {
		title, text = p.selectedToolText()
	} else if p.selection.Active {
		title, text = "Selected output", p.Selection()
	} else {
		p.refreshHistory(true)
		first := p.snapshot.FirstLine + uint64(max(0, p.snapshot.ScrollbackLen-p.snapshot.ScrollOffset))
		title, text = "Visible output", textRange(p.tools.history, first, 0, first+uint64(p.snapshot.Rows-1), p.snapshot.Cols, maxPinnedBytes)
	}
	if text == "" {
		p.tools.message = "No output to pin"
		return
	}
	if len(text) > maxPinnedBytes {
		text = truncateResult(text, maxPinnedBytes)
	}
	if len(p.tools.pins) == maxPins {
		p.tools.message = "16 pins retained — delete a pin before adding another"
		return
	}
	p.tools.nextPin++
	p.tools.pins = append(p.tools.pins, terminalPin{p.tools.nextPin, clippedText(title, 100), text})
	p.tools.rowsDirty = true
	p.tools.message = "Result pinned — Ctrl+Shift+P opens pins"
}

func (p *Provider) buildToolRows() {
	u := &p.tools
	if !u.rowsDirty {
		p.clampToolSelection()
		return
	}
	u.rows = u.rows[:0]
	switch u.mode {
	case "bookmarks":
		for _, b := range u.bookmarks {
			label := fmt.Sprintf("* %d  %s", b.line+1, b.label)
			if b.line < u.history.FirstLine {
				label += " [expired]"
			}
			u.rows = append(u.rows, historyRow{text: label, kind: "bookmark", line: b.line})
		}
		if len(u.rows) == 0 {
			u.rows = append(u.rows, historyRow{text: "Ctrl+Shift+M bookmarks the current line."}, historyRow{text: "Bookmarks last for this terminal session."})
		}
	case "blocks":
		for _, c := range u.history.Commands {
			if !c.Started {
				continue
			}
			status := "running"
			if c.Finished {
				status = "done"
				if c.Status >= 0 {
					status = fmt.Sprintf("exit %d", c.Status)
				}
			}
			sign := "+"
			if u.expanded[c.ID] {
				sign = "-"
			}
			u.rows = append(u.rows, historyRow{text: fmt.Sprintf("%s [%s] %s", sign, status, commandTitle(u.history, c)), kind: "command", id: c.ID})
			if u.expanded[c.ID] {
				for _, line := range strings.Split(commandOutput(u.history, c), "\n") {
					u.rows = append(u.rows, historyRow{text: "  " + line, kind: "command", id: c.ID})
				}
			}
		}
		if len(u.rows) == 0 {
			u.rows = append(u.rows, historyRow{text: "Command blocks use optional OSC 133 markers."}, historyRow{text: "Press I to copy Bash 4.4+ setup, then paste"}, historyRow{text: "it into your shell to opt in. Nothing runs"}, historyRow{text: "until you paste and execute the setup."}, historyRow{text: "Existing shell hooks are preserved."})
		}
	case "pins":
		for _, pin := range u.pins {
			id := pin.id | (uint64(1) << 63)
			sign := "+"
			if u.expanded[id] {
				sign = "-"
			}
			u.rows = append(u.rows, historyRow{text: sign + " " + pin.title, kind: "pin", id: pin.id})
			if u.expanded[id] {
				for _, line := range strings.Split(pin.text, "\n") {
					u.rows = append(u.rows, historyRow{text: "  " + line, kind: "pin", id: pin.id})
				}
			}
		}
		if len(u.rows) == 0 {
			u.rows = append(u.rows, historyRow{text: "Ctrl+Shift+R pins selected or visible output."}, historyRow{text: "In command blocks, P pins the selected result."}, historyRow{text: "Pins remain available after history expires."}, historyRow{text: "Pins last for this terminal session."})
		}
	case "tasks":
		for _, task := range u.tasks {
			key := task.id | taskFoldMask
			sign := "+"
			if u.expanded[key] {
				sign = "-"
			}
			u.rows = append(u.rows, historyRow{text: fmt.Sprintf("%s [%s] %s", sign, task.status, task.Name), kind: "task", id: task.id})
			if u.expanded[key] {
				u.rows = append(u.rows, historyRow{text: "  $ " + task.Command, kind: "task", id: task.id})
			}
		}
		if len(u.rows) == 0 {
			u.rows = append(u.rows,
				historyRow{text: "Save a command from Runs with T."},
				historyRow{text: "S stages a task for review without Enter."},
				historyRow{text: "Ctrl+Enter explicitly sends and executes a task."},
				historyRow{text: "Restored recipes never run automatically."},
			)
		}
	}
	u.rowsDirty = false
	u.viewCells = nil
	p.clampToolSelection()
}

func (p *Provider) clampToolSelection() {
	u := &p.tools
	u.selected = max(0, min(u.selected, len(u.rows)-1))
	if u.selected < u.offset {
		u.offset = u.selected
	}
	if u.selected >= u.offset+p.snapshot.Rows {
		u.offset = u.selected - p.snapshot.Rows + 1
	}
	u.offset = max(0, min(u.offset, max(0, len(u.rows)-p.snapshot.Rows)))
}

func (p *Provider) activateToolRow() {
	u := &p.tools
	if u.selected < 0 || u.selected >= len(u.rows) {
		return
	}
	row := u.rows[u.selected]
	switch row.kind {
	case "bookmark":
		for _, b := range u.bookmarks {
			if b.line == row.line && b.text != lineText(u.history, row.line) {
				u.message = "This bookmarked output has changed or expired"
				return
			}
		}
		if backend, ok := p.terminal.(historyBackend); ok && backend.ScrollToLine(row.line) {
			p.setTool("")
		} else {
			u.message = "This bookmarked line has expired"
		}
	case "command", "pin", "task":
		id := row.id
		if row.kind == "pin" {
			id |= uint64(1) << 63
		} else if row.kind == "task" {
			id |= taskFoldMask
		}
		u.expanded[id] = !u.expanded[id]
		u.rowsDirty = true
		// Keep the header selected when collapsing an output row.
		for u.selected > 0 && u.rows[u.selected-1].kind == row.kind && u.rows[u.selected-1].id == row.id {
			u.selected--
		}
	}
}

func (p *Provider) deleteToolRow() {
	u := &p.tools
	if u.selected >= len(u.rows) {
		return
	}
	row := u.rows[u.selected]
	u.rowsDirty = true
	if row.kind == "bookmark" {
		for i, b := range u.bookmarks {
			if b.line == row.line {
				u.bookmarks = append(u.bookmarks[:i], u.bookmarks[i+1:]...)
				break
			}
		}
	}
	if row.kind == "pin" {
		for i, pin := range u.pins {
			if pin.id == row.id {
				u.pins = append(u.pins[:i], u.pins[i+1:]...)
				delete(u.expanded, row.id|(uint64(1)<<63))
				break
			}
		}
	}
	if row.kind == "task" {
		for i, task := range u.tasks {
			if task.id == row.id {
				u.tasks = append(u.tasks[:i], u.tasks[i+1:]...)
				delete(u.expanded, row.id|taskFoldMask)
				break
			}
		}
	}
}

func (p *Provider) toolKey(event experience.Event) bool {
	u := &p.tools
	code := event.Keycode
	if code < uint32(len(u.owned)) && !event.Pressed && u.owned[code] {
		u.owned[code] = false
		return true
	}
	shortcut := event.Modifiers == experience.ModControl|experience.ModShift
	if shortcut && (code == 33 || code == 48 || code == 37 || code == 25 || code == 20 || code == 50 || code == 19) {
		if code < uint32(len(u.owned)) {
			u.owned[code] = true
		}
		if !event.Pressed || event.Repeat {
			return true
		}
		switch code {
		case 33:
			p.setTool("find")
		case 48:
			p.setTool("bookmarks")
		case 37:
			p.setTool("blocks")
		case 25:
			p.setTool("pins")
		case 20:
			p.setTool("tasks")
		case 50:
			p.bookmark()
		case 19:
			p.pinResult()
		}
		return true
	}
	if u.mode == "" {
		if u.toolbarActive {
			return p.toolbarKey(event)
		}
		return false
	}
	if code < uint32(len(u.owned)) {
		u.owned[code] = event.Pressed
	}
	if !event.Pressed {
		return true
	}
	if code == 1 {
		p.setTool("")
		return true
	}
	if code == 15 || u.toolbarActive {
		return p.toolbarKey(event)
	}
	if shortcut && code == 47 {
		p.pasteRequested = u.mode == "find"
		return true
	}
	if shortcut && code == 46 {
		if u.mode == "find" {
			if u.match < len(u.matches) {
				p.copyText = lineText(u.history, u.matches[u.match].line)
				p.copyReady = true
			}
		} else {
			_, p.copyText = p.selectedToolText()
			p.copyReady = true
		}
		return true
	}
	if u.mode == "find" {
		switch code {
		case 28, 61:
			delta := 1
			if event.Modifiers.Has(experience.ModShift) {
				delta = -1
			}
			p.jumpMatch(delta)
		case 22:
			if event.Modifiers.Has(experience.ModControl) {
				u.field.Set("")
				u.query = ""
				p.searchHistory(true)
			} else {
				if u.field.Handle(event, u.text.Handle(event)) {
					u.query = u.field.Text()
					p.searchHistory(true)
				}
			}
		default:
			if u.field.Handle(event, u.text.Handle(event)) {
				u.query = u.field.Text()
				p.searchHistory(true)
			}
		}
		return true
	}
	if u.mode == "tasks" && code == 28 && event.Modifiers.Has(experience.ModControl) {
		p.dispatchSelectedTask(true)
		return true
	}
	if event.Repeat && code != 103 && code != 108 && code != 104 && code != 109 {
		return true
	}
	switch code {
	case 103:
		u.selected--
	case 108:
		u.selected++
	case 104:
		u.selected -= p.snapshot.Rows
	case 109:
		u.selected += p.snapshot.Rows
	case 102:
		u.selected = 0
	case 107:
		u.selected = len(u.rows) - 1
	case 28, 57:
		p.activateToolRow()
	case 111:
		p.deleteToolRow()
	case 31:
		if u.mode == "tasks" {
			p.dispatchSelectedTask(false)
		}
	case 25:
		p.pinResult()
	case 20:
		if u.mode == "blocks" {
			p.saveSelectedCommandTask()
		}
	case 23:
		if u.mode == "blocks" {
			p.copyText, p.copyReady = terminal.BashIntegration, true
			u.message = "Bash setup copied; review and paste into your shell to enable"
		}
	}
	return true
}

func (p *Provider) toolPointer(event experience.Event) bool {
	p.syncToolControls()
	action := p.tools.controls.Handle(event)
	if action.Consumed {
		if action.ID == "find-field" && (event.Kind == experience.PointerDown || event.Kind == experience.PointerMove) {
			_ = p.renderer.ui.PlaceCaret(p.tools.field, findFieldRect(p.renderer.image.Rect.Dx()), image.Pt(int(event.X), int(event.Y)), event.Kind == experience.PointerMove || event.Modifiers.Has(experience.ModShift))
		}
		if strings.HasPrefix(action.ID, "row:") {
			index, _ := strconv.Atoi(strings.TrimPrefix(action.ID, "row:"))
			p.tools.selected = index
			p.tools.toolbarActive = false
			if action.Activated {
				p.activateToolRow()
			}
		} else if action.ID != "find-field" {
			p.tools.toolbarActive = true
			p.terminal.Focus(false)
			if action.Activated {
				p.setTool(action.ID)
			}
		}
		return true
	}
	if p.tools.mode == "" {
		if p.tools.toolbarActive && event.Kind == experience.PointerDown {
			p.tools.controls.Blur()
			p.tools.toolbarActive = false
			p.terminal.Focus(p.focused)
		}
		return false
	}
	if p.tools.mode == "find" {
		return event.Kind != experience.PointerScroll
	}
	if event.Kind == experience.PointerScroll {
		delta := int(event.ScrollY / 5)
		if delta == 0 && event.ScrollY != 0 {
			if event.ScrollY > 0 {
				delta = 1
			} else {
				delta = -1
			}
		}
		p.tools.selected += delta
		return true
	}
	return true
}

func terminalToolbar(width int) []nativeui.Node {
	var nodes []nativeui.Node
	for i, id := range []string{"find", "bookmarks", "blocks", "pins", "tasks"} {
		x := width - 228 + i*44
		nodes = append(nodes, nativeui.Node{ID: id, Role: nativeui.RoleButton, Label: []string{"Find", "Marks", "Runs", "Pins", "Tasks"}[i], Description: []string{"Search scrollback", "Bookmarks", "Collapsible command output", "Pinned results", "Reusable commands that run only on request"}[i], Bounds: image.Rect(x, 30, x+40, 47)})
	}
	return nodes
}
func findFieldRect(width int) image.Rectangle { return image.Rect(59, 29, width-18, 48) }
func (p *Provider) syncToolControls() {
	u := &p.tools
	nodes := terminalToolbar(p.renderer.image.Rect.Dx())
	for i := range nodes {
		nodes[i].Disabled = p.snapshot.AlternateScreen
	}
	if u.mode == "find" {
		nodes = []nativeui.Node{{ID: "find-field", Role: nativeui.RoleTextField, Label: "Search terminal scrollback", Value: u.query, Bounds: findFieldRect(p.renderer.image.Rect.Dx())}}
	} else if u.mode != "" {
		for i := u.offset; i < len(u.rows) && i < u.offset+p.snapshot.Rows; i++ {
			y := contentTop + (i-u.offset)*cellHeight
			nodes = append(nodes, nativeui.Node{ID: fmt.Sprintf("row:%d", i), Role: nativeui.RoleMenuItem, Label: u.rows[i].text, Selected: i == u.selected, Bounds: image.Rect(contentLeft, y, contentLeft+p.snapshot.Cols*cellWidth, y+cellHeight)})
		}
	}
	u.controls.SetNodes(nodes)
	if !p.focused {
		u.controls.Focus("")
	} else if u.mode == "find" {
		u.controls.Focus("find-field")
	} else if u.mode != "" && !u.toolbarActive {
		u.controls.Focus(fmt.Sprintf("row:%d", u.selected))
	}
}
func (p *Provider) toolbarKey(event experience.Event) bool {
	u := &p.tools
	if event.Keycode == 1 && event.Pressed {
		u.toolbarActive = false
		u.controls.Blur()
		p.terminal.Focus(p.focused && u.mode == "")
		return true
	}
	p.syncToolControls()
	action := u.controls.Handle(event)
	if action.ChangedFocus {
		u.toolbarActive = !strings.HasPrefix(action.ID, "row:") && action.ID != "find-field"
		if strings.HasPrefix(action.ID, "row:") {
			u.selected, _ = strconv.Atoi(strings.TrimPrefix(action.ID, "row:"))
		}
	}
	if action.Activated {
		if strings.HasPrefix(action.ID, "row:") {
			p.activateToolRow()
		} else {
			p.setTool(action.ID)
		}
	}
	return true
}

// Semantics is a copied native control tree for the host accessibility bridge.
// Live terminal cells require a separate accessible text model.
func (p *Provider) Semantics() nativeui.SemanticTree {
	if p.closed {
		return nativeui.SemanticTree{}
	}
	p.syncToolControls()
	return p.tools.controls.Semantics()
}

func (p *Provider) toolDisplay(snapshot terminal.Snapshot, decoration terminalDecoration) (terminal.Snapshot, terminalDecoration) {
	u := &p.tools
	if snapshot.AlternateScreen && u.mode != "" {
		p.setTool("")
	}
	decoration.Footer = u.message
	decoration.ToolFocus = u.controls.FocusedID()
	if u.mode == "" {
		return snapshot, decoration
	}
	p.refreshHistory(false)
	decoration.CursorOn = false
	snapshot.Cursor.Visible = false
	if u.mode == "find" {
		decoration.Header = "Find: " + u.query
		decoration.Find = u.field
		decoration.FindCaret = u.field.Caret()
		decoration.FindPreedit, decoration.FindPreeditBegin, decoration.FindPreeditEnd = u.field.Preedit()
		lo, hi := u.field.Range()
		decoration.FindAnchor = lo + hi - u.field.Caret()
		position := 0
		if len(u.matches) > 0 {
			position = u.match + 1
		}
		decoration.Footer = fmt.Sprintf("%d/%d  Enter next · Shift+Enter previous · Esc close", position, len(u.matches))
		if len(u.matches) == maxSearchMatches {
			decoration.Footer = "4096 matches (limit) · refine the query"
		}
		if u.match < len(u.matches) {
			hit := u.matches[u.match]
			first := snapshot.FirstLine + uint64(max(0, snapshot.ScrollbackLen-snapshot.ScrollOffset))
			if hit.line >= first && hit.line-first < uint64(snapshot.Rows) && hit.first < snapshot.Cols {
				row := int(hit.line - first)
				decoration.Selection = selection{row*snapshot.Cols + hit.first, row*snapshot.Cols + min(snapshot.Cols, hit.last) - 1, true}
			}
		}
		return snapshot, decoration
	}
	p.buildToolRows()
	snapshot.Title = map[string]string{"bookmarks": "Bookmarks", "blocks": "Command blocks", "pins": "Pinned results", "tasks": "Task recipes"}[u.mode]
	decoration.Footer = "↑↓ move · Enter open/fold · P pin · Ctrl+Shift+C copy · Esc close"
	if u.mode == "bookmarks" {
		decoration.Footer = "Enter jump · Delete remove · Ctrl+Shift+M bookmark · Esc close"
	}
	if u.mode == "pins" {
		decoration.Footer = "Enter fold · Delete remove · Ctrl+Shift+C copy · Esc close"
	}
	if u.mode == "blocks" {
		decoration.Footer = "Enter fold · T save task · P pin · Ctrl+Shift+C copy · Esc close"
	}
	if u.mode == "tasks" {
		decoration.Footer = "Enter details · S stage · Ctrl+Enter run · Delete remove · Esc close"
	}
	if u.message != "" {
		decoration.Footer = u.message
	}
	if u.viewCells == nil || u.viewCols != snapshot.Cols || u.viewRows != snapshot.Rows || u.viewOffset != u.offset {
		u.viewCells = make([]terminal.Cell, snapshot.Cols*snapshot.Rows)
		u.viewLabels = make([]string, snapshot.Rows)
		for i := range u.viewCells {
			u.viewCells[i] = terminal.Cell{Width: 1, Foreground: terminal.Color{R: 220, G: 235, B: 238}, Background: terminal.Color{R: 8, G: 16, B: 23}}
		}
		for row := 0; row < snapshot.Rows && row+u.offset < len(u.rows); row++ {
			u.viewLabels[row] = u.rows[row+u.offset].text
			text := clippedText(u.rows[row+u.offset].text, snapshot.Cols)
			for col, ch := range []rune(text) {
				u.viewCells[row*snapshot.Cols+col].Chars[0] = ch
			}
		}
		u.viewCols, u.viewRows, u.viewOffset = snapshot.Cols, snapshot.Rows, u.offset
	}
	snapshot.Cells = u.viewCells
	decoration.ToolRows = u.viewLabels
	row := u.selected - u.offset
	if row >= 0 && row < snapshot.Rows {
		decoration.Selection = selection{row * snapshot.Cols, (row+1)*snapshot.Cols - 1, true}
	}
	return snapshot, decoration
}
