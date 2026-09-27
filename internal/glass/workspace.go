package glass

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/glass/commands"
	"github.com/codemodify/worldr/internal/scene"
	"github.com/codemodify/worldr/internal/terminal"
)

type blockTarget struct {
	id  uint64
	box rect
}

func (a *App) contextLabel() string {
	path := a.cwd
	if home, err := os.UserHomeDir(); err == nil && (path == home || strings.HasPrefix(path, home+"/")) {
		path = "~" + strings.TrimPrefix(path, home)
	}
	if path == "" {
		return "Local session"
	}
	return path
}

func (a *App) updateWorkspace() error {
	p := a.pane()
	if p == nil {
		return nil
	}
	if a.elapsed >= a.contextPoll {
		if dir, err := p.term.WorkingDirectory(); err == nil && dir != a.cwd {
			a.cwd, a.dirty = dir, true
		}
		a.contextPoll = a.elapsed + 500*time.Millisecond
	}
	if a.historyView && p.snapshot.Revision != a.historyRevision && a.elapsed >= a.historyPoll {
		h, err := p.term.History()
		if err != nil {
			return err
		}
		a.cards = commands.FromHistory(h)
		a.historyRevision = p.snapshot.Revision
		a.historyPoll = a.elapsed + 160*time.Millisecond
		found := false
		for _, card := range a.cards {
			if card.ID == a.selectedCard {
				found = true
				break
			}
		}
		if !found && len(a.cards) > 0 {
			a.selectedCard = a.cards[len(a.cards)-1].ID
		}
		a.dirty = true
	}
	if a.searchOpen && p.snapshot.Revision != a.searchRevision && a.elapsed >= a.searchPoll {
		return a.refreshSearch(false)
	}
	return nil
}

func (a *App) setView(blocks bool) {
	a.setSearch(false)
	a.setSettings(false)
	a.historyView = blocks
	a.viewReveal = 0
	a.historyRevision, a.historyPoll = 0, 0
	if p := a.pane(); p != nil {
		p.selecting = false
		if blocks {
			a.remember(p.Handle(experience.Event{Kind: experience.KeyboardCancel}))
		} else {
			p.term.Focus(a.focused)
		}
	}
	a.dirty = true
}

func (a *App) workspaceButton(id, label string, box rect) {
	button := button{id: id, label: label, box: box}
	a.buttons = append(a.buttons, button)
	a.drawButton(button)
}

func (a *App) drawHistory() {
	w := a.workspace
	a.historyTargets = a.historyTargets[:0]
	if len(a.cards) == 0 {
		x, y := w.x+20, w.y+16
		a.text(x, y, 16, truncate("Your commands, in context.", int((w.w-40)/9.6)), a.accent)
		buttonY := w.y + w.h - 42
		lines := []string{"Run a command in Live, then return here.", "Each block keeps its output and exit status.", "Copy results or jump back to the original output."}
		if !a.integrated {
			lines = []string{"This shell has not reported any command boundaries.", "Bash setup is available below; copy and source it", "in an interactive Bash session to enable blocks."}
		}
		for i, line := range lines {
			ly := y + 34 + float32(i)*22
			if ly+20 > buttonY-6 {
				break
			}
			a.text(x, ly, 12, truncate(line, int((w.w-40)/7.2)), scene.ColorHex(0xa8c7ce, 1))
		}
		a.workspaceButton("live", "Back to Live", rect{x, buttonY, 126, 30})
		if !a.integrated {
			a.workspaceButton("integration", "Copy Bash setup", rect{x + 136, buttonY, min(156, w.w-176), 30})
		}
		a.historyHeight = 0
		a.historyTarget, a.historyScroll = 0, 0
		return
	}
	lineHeight := float32(20)
	shift := 8 * (1 - a.viewReveal)
	y := w.y + 12 + shift - a.historyScroll
	for i := len(a.cards) - 1; i >= 0; i-- {
		card := a.cards[i]
		lines := strings.Split(card.Output, "\n")
		if card.Output == "" {
			lines = nil
		}
		limit := 4
		if a.expanded[card.ID] {
			limit = 200
		}
		count := min(len(lines), limit)
		h := float32(99) + float32(max(1, count))*lineHeight
		if card.Truncated || len(lines) > limit {
			h += 22
		}
		r := rect{w.x + 10, y, w.w - 20, h}
		y += h + 12
		if r.y+r.h < w.y || r.y > w.y+w.h {
			continue
		}
		visible := rect{r.x, max(w.y, r.y), r.w, min(w.y+w.h, r.y+r.h) - max(w.y, r.y)}
		a.historyTargets = append(a.historyTargets, blockTarget{card.ID, visible})
		a.roundFill(visible, 9, scene.ColorHex(0x102e3a, .52))
		selected := card.ID == a.selectedCard
		edge := float32(.17)
		if selected {
			edge = .48
		}
		a.roundStroke(visible, 9, 1, a.accent.WithAlpha(edge))
		status, statusColor := "RUNNING", a.accent
		if card.Finished {
			status = fmt.Sprintf("EXIT %d", card.Status)
			statusColor = scene.ColorHex(0x9cd9b2, 1)
			if card.Status < 0 {
				status = "FINISHED"
			}
			if card.Status > 0 {
				statusColor = scene.ColorHex(0xed98a4, 1)
			}
		}
		if r.y+14 >= w.y && r.y+36 <= w.y+w.h {
			a.text(r.x+16, r.y+14, 10, fmt.Sprintf("%03d  /  %s", card.ID, status), statusColor)
			id := strconv.FormatUint(card.ID, 10)
			a.workspaceButton("block:toggle:"+id, map[bool]string{true: "Fold", false: "Expand"}[a.expanded[card.ID]], rect{r.x + r.w - 239, r.y + 8, 68, 27})
			a.workspaceButton("block:jump:"+id, "Locate", rect{r.x + r.w - 168, r.y + 8, 64, 27})
			a.workspaceButton("block:copy:"+id, "Copy output", rect{r.x + r.w - 101, r.y + 8, 91, 27})
		}
		if r.y+47 >= w.y && r.y+67 <= w.y+w.h {
			command := strings.ReplaceAll(card.Command, "\n", " ↵ ")
			a.text(r.x+16, r.y+46, 13, truncate(command, int((r.w-32)/7.8)), scene.ColorHex(0xd9eff2, 1))
		}
		if len(lines) == 0 {
			lines = []string{"No output"}
			count = 1
		}
		for k := 0; k < count; k++ {
			ly := r.y + 79 + float32(k)*lineHeight
			if ly < w.y || ly+lineHeight > w.y+w.h {
				continue
			}
			a.text(r.x+16, ly, 12, truncate(lines[k], int((r.w-32)/7.2)), scene.ColorHex(0xaacbd2, 1))
		}
		if card.Truncated || len(lines) > limit {
			ly := r.y + h - 24
			if ly >= w.y && ly+18 <= w.y+w.h {
				label := "More output · expand or copy the full retained result"
				if card.Truncated {
					label = "Earlier rows have left scrollback · retained output shown"
				}
				a.text(r.x+16, ly, 10, truncate(label, int((r.w-32)/6)), a.accent.WithAlpha(.72))
			}
		}
	}
	a.historyHeight = y + a.historyScroll - w.y - shift
	maxScroll := max(0, a.historyHeight-w.h)
	a.historyTarget = min(a.historyTarget, maxScroll)
	if maxScroll > 0 {
		top := w.y + 6 + (w.h-40)*min(1, a.historyScroll/maxScroll)
		a.line(w.x+w.w-3, top, w.x+w.w-3, top+32, 2, a.accent.WithAlpha(.6))
	}
}

func (a *App) activateWorkspace(id string) bool {
	switch id {
	case "live":
		a.setView(false)
	case "blocks":
		a.setView(true)
	case "search":
		a.setSearch(!a.searchOpen)
	case "search:close":
		a.setSearch(false)
	case "search:prev":
		a.nextMatch(true)
	case "search:next":
		a.nextMatch(false)
	case "search:field":
		a.searchSelect = true
	case "integration":
		a.copyText(terminal.BashIntegration, "Bash setup copied; source it in Bash")
	default:
		parts := strings.Split(id, ":")
		if len(parts) != 3 || parts[0] != "block" {
			return false
		}
		cardID, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			return true
		}
		for _, card := range a.cards {
			if card.ID != cardID {
				continue
			}
			a.selectedCard = cardID
			switch parts[1] {
			case "toggle":
				a.expanded[cardID] = !a.expanded[cardID]
			case "copy":
				a.copyText(card.Output, "Command output copied")
			case "jump":
				a.setView(false)
				if !a.pane().term.ScrollToLine(card.OutputLine) {
					a.say("This output has left scrollback")
				}
			}
			break
		}
	}
	a.dirty = true
	return true
}

func (a *App) copyText(text, notice string) {
	if a.host == nil {
		return
	}
	if err := a.host.WriteClipboard(text); err != nil {
		a.say("Copy: " + err.Error())
	} else {
		a.say(notice)
	}
}

func (a *App) historyKey(e experience.Event) {
	if !e.Pressed {
		return
	}
	switch e.Keycode {
	case 1:
		a.setView(false)
	case 104:
		a.historyTarget = max(0, a.historyTarget-a.workspace.h*.8)
	case 109:
		a.historyTarget = min(max(0, a.historyHeight-a.workspace.h), a.historyTarget+a.workspace.h*.8)
	case 102:
		a.historyTarget = 0
	case 107:
		a.historyTarget = max(0, a.historyHeight-a.workspace.h)
	case 103:
		a.historyTarget = max(0, a.historyTarget-60)
	case 108:
		a.historyTarget = min(max(0, a.historyHeight-a.workspace.h), a.historyTarget+60)
	case 28:
		a.expanded[a.selectedCard] = !a.expanded[a.selectedCard]
	}
}

func (a *App) setSearch(open bool) {
	if a.translator != nil && a.searchOpen != open {
		a.translator.Handle(experience.Event{Kind: experience.KeyboardCancel})
	}
	if open {
		a.historyView = false
		a.setSettings(false)
		a.remember(a.pane().Handle(experience.Event{Kind: experience.KeyboardCancel}))
		a.searchRevision, a.searchPoll = 0, 0
		a.searchCursor = len([]rune(a.searchText))
		a.searchSelect = true
	} else if a.pane() != nil {
		a.pane().term.Focus(a.focused && !a.historyView && !a.settings)
	}
	a.searchOpen = open
	a.dirty = true
}

func (a *App) refreshSearch(reveal bool) error {
	results, err := a.pane().term.Search(a.searchText, false)
	if err != nil {
		return err
	}
	a.searchResults = results
	a.searchRows = map[uint64][]terminal.SearchMatch{}
	for _, match := range results.Matches {
		a.searchRows[match.Line] = append(a.searchRows[match.Line], match)
	}
	a.searchIndex = min(a.searchIndex, len(results.Matches)-1)
	if a.searchIndex < 0 && len(results.Matches) > 0 {
		a.searchIndex = 0
	}
	a.searchRevision = a.pane().snapshot.Revision
	a.searchPoll = a.elapsed + 200*time.Millisecond
	if reveal && a.searchIndex >= 0 {
		a.pane().term.RevealMatch(results.Matches[a.searchIndex])
	}
	a.dirty = true
	return nil
}

func (a *App) nextMatch(backwards bool) {
	match, index, ok := a.searchResults.Next(a.searchIndex, backwards)
	a.searchIndex = index
	if ok {
		a.pane().term.RevealMatch(match)
	}
	a.dirty = true
}

func (a *App) searchCell(row, col int) bool {
	if !a.searchOpen || a.pane() == nil || a.pane().snapshot.AlternateScreen {
		return false
	}
	s := a.pane().snapshot
	line := s.FirstLine + uint64(s.ScrollbackLen-s.ScrollOffset+row)
	for _, match := range a.searchRows[line] {
		if col >= match.StartColumn && col < match.EndColumn {
			return true
		}
	}
	return false
}

func (a *App) insertSearch(text string) {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	runes := []rune(a.searchText)
	if a.searchSelect {
		runes = nil
		a.searchCursor = 0
		a.searchSelect = false
	}
	added := []rune(text)
	if len(added)+len(runes) > 256 {
		return
	}
	a.searchCursor = min(len(runes), max(0, a.searchCursor))
	a.searchText = string(runes[:a.searchCursor]) + string(added) + string(runes[a.searchCursor:])
	a.searchCursor += len(added)
	a.searchIndex = -1
	a.remember(a.refreshSearch(true))
}

func (a *App) searchKey(e experience.Event) {
	if !e.Pressed {
		return
	}
	if e.Modifiers.Has(experience.ModControl) && e.Keycode == 30 {
		a.searchSelect = true
		return
	}
	if e.Modifiers.Has(experience.ModControl) && e.Keycode == 47 {
		a.paste()
		return
	}
	if e.Modifiers.Has(experience.ModControl) && e.Keycode == 46 {
		a.copy()
		return
	}
	runes := []rune(a.searchText)
	switch e.Keycode {
	case 1:
		a.setSearch(false)
		return
	case 28:
		a.nextMatch(e.Modifiers.Has(experience.ModShift))
		return
	case 105:
		a.searchCursor = max(0, a.searchCursor-1)
		a.searchSelect = false
		return
	case 106:
		a.searchCursor = min(len(runes), a.searchCursor+1)
		a.searchSelect = false
		return
	case 102:
		a.searchCursor = 0
		a.searchSelect = false
		return
	case 107:
		a.searchCursor = len(runes)
		a.searchSelect = false
		return
	case 14, 111:
		if a.searchSelect {
			a.searchText = ""
			a.searchCursor = 0
			a.searchSelect = false
		} else if e.Keycode == 14 && a.searchCursor > 0 {
			a.searchText = string(runes[:a.searchCursor-1]) + string(runes[a.searchCursor:])
			a.searchCursor--
		} else if e.Keycode == 111 && a.searchCursor < len(runes) {
			a.searchText = string(runes[:a.searchCursor]) + string(runes[a.searchCursor+1:])
		}
		a.searchIndex = -1
		a.remember(a.refreshSearch(true))
		return
	}
	if a.translator != nil {
		if text := a.translator.Handle(e); text != "" {
			a.insertSearch(text)
		}
	}
}

func (a *App) drawSearch() {
	w := a.workspace
	fade := a.searchReveal
	a.searchBox = rect{w.x + 10, w.y + 10 - 6*(1-fade), max(0, w.w-20), 100}
	s := a.searchBox
	a.roundFill(s, 10, scene.ColorHex(0x091c27, .98*fade))
	a.roundStroke(s, 10, 1, a.accent.WithAlpha(.65*fade))
	field := rect{s.x + 14, s.y + 13, max(0, s.w-230), 33}
	a.roundFill(field, 4, a.accent.WithAlpha(.06*fade))
	if a.searchSelect {
		a.roundFill(field, 4, a.accent.WithAlpha(.13*fade))
	}
	text := a.searchText
	if text == "" {
		text = "Search output…"
	}
	available := max(1, int((field.w-18)/8))
	runes := []rune(text)
	start := max(0, a.searchCursor-available+1)
	if start < len(runes) {
		text = string(runes[start:])
	}
	a.text(field.x+8, field.y+8, 13, truncate(text, available), scene.ColorHex(0xd9eff2, fade))
	if a.cursorOn {
		x := field.x + 8 + float32(max(0, a.searchCursor-start))*7.8
		a.line(x, field.y+7, x, field.y+25, 1, a.accent.WithAlpha(fade))
	}
	if a.searchOpen {
		a.buttons = append(a.buttons, button{id: "search:field", box: field})
	}
	control := func(id, label string, r rect) {
		b := button{id: id, label: label, box: r}
		if a.searchOpen {
			a.buttons = append(a.buttons, b)
		}
		a.drawButtonWithAlpha(b, fade)
	}
	control("search:prev", "Previous", rect{s.x + s.w - 211, s.y + 14, 80, 30})
	control("search:next", "Next", rect{s.x + s.w - 127, s.y + 14, 61, 30})
	control("search:close", "×", rect{s.x + s.w - 57, s.y + 14, 41, 30})
	status := fmt.Sprintf("%d / %d matches · Enter next · Shift+Enter previous", max(0, a.searchIndex+1), len(a.searchResults.Matches))
	if a.searchResults.Truncated {
		status += " · limited"
	}
	if a.pane().snapshot.AlternateScreen {
		status = "Search resumes when the interactive program exits"
	}
	a.text(s.x+20, s.y+55, 10, truncate(status, int((s.w-40)/6)), a.accent.WithAlpha(.8*fade))
	if a.searchIndex >= 0 && a.searchIndex < len(a.searchResults.Matches) {
		a.text(s.x+20, s.y+75, 10, truncate(a.searchResults.Matches[a.searchIndex].Text, int((s.w-40)/6)), scene.ColorHex(0xabcad1, fade))
	}
}
