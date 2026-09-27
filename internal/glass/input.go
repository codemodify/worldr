package glass

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
	kit "github.com/codemodify/worldr/sdk/app/v1"
)

func (a *App) hit(x, y float32) string {
	for i := len(a.buttons) - 1; i >= 0; i-- {
		b := a.buttons[i]
		setting := strings.HasPrefix(b.id, "theme:") || b.id == "opacity" || b.id == "glow" || b.id == "motion" || b.id == "reset"
		if a.settings && a.settingsBox.contains(x, y) && !setting {
			continue
		}
		if !a.settings && (strings.HasPrefix(b.id, "theme:") || b.id == "opacity" || b.id == "glow" || b.id == "motion" || b.id == "reset") {
			continue
		}
		if b.box.contains(x, y) {
			return b.id
		}
	}
	return ""
}
func (a *App) Handle(e experience.Event) bool {
	if a.closed {
		return false
	}
	a.dirty = true
	p := a.pane()
	if p == nil {
		return false
	}
	switch e.Kind {
	case experience.KeymapChanged, experience.KeyboardRepeatInfo, experience.KeyboardModifiers:
		if a.translator != nil {
			a.translator.Handle(e)
		}
		if e.Kind == experience.KeymapChanged {
			a.keymap = e
		}
		if e.Kind == experience.KeyboardRepeatInfo {
			a.repeat = e
		}
		for _, tab := range a.tabs {
			a.remember(tab.Handle(e))
		}
		return true
	case experience.KeyboardCancel:
		if a.translator != nil {
			a.translator.Handle(e)
		}
		a.focused = false
		a.owned = map[uint32]bool{}
		a.pressed = ""
		a.slider = ""
		for _, tab := range a.tabs {
			a.remember(tab.Handle(e))
		}
		return true
	case experience.PointerCancel:
		a.pressed = ""
		a.slider = ""
		a.hover = ""
		a.remember(p.Handle(e))
		return true
	case experience.KeyInput:
		if e.Pressed {
			a.focused = true
		}
		if !e.Pressed && a.owned[e.Keycode] {
			delete(a.owned, e.Keycode)
			return true
		}
		if e.Pressed && a.shortcut(e) {
			a.owned[e.Keycode] = true
			return true
		}
		if a.settings {
			return true
		}
		if a.searchOpen {
			a.searchKey(e)
			return true
		}
		if a.historyView {
			a.historyKey(e)
			return true
		}
		a.activity = .7
		a.remember(p.Handle(e))
		return true
	case experience.PointerMove, experience.PointerDown, experience.PointerUp, experience.PointerScroll:
		x, y := e.X/a.unit, e.Y/a.unit
		hit := a.hit(x, y)
		if e.Kind == experience.PointerMove {
			a.hover = hit
			if a.slider != "" {
				a.adjustSlider(a.slider, x)
				return true
			}
		}
		if e.Kind == experience.PointerDown && e.Button == experience.ButtonPrimary {
			a.focused = true
			if !a.settings && !a.historyView && !a.searchOpen {
				p.term.Focus(true)
			}
			if hit != "" {
				a.pressed = hit
				if hit == "opacity" || hit == "glow" {
					a.slider = hit
					a.adjustSlider(hit, x)
				}
				return true
			}
			if a.settings && a.settingsBox.contains(x, y) {
				return true
			}
			if a.host != nil {
				if edge, ok := a.resizeEdge(x, y); ok {
					a.host.BeginResize(edge)
					return true
				}
				if y < a.frame.y+58 && a.frame.contains(x, y) {
					a.host.BeginMove()
					return true
				}
			}
		}
		if e.Kind == experience.PointerUp && e.Button == experience.ButtonPrimary {
			pressed := a.pressed
			a.pressed = ""
			a.slider = ""
			if pressed != "" {
				if pressed == hit {
					a.activate(hit)
				}
				return true
			}
		}
		if a.settings && a.settingsBox.contains(x, y) {
			return true
		}
		if a.searchOpen {
			return true
		}
		if a.historyView {
			if e.Kind == experience.PointerDown && e.Button == experience.ButtonPrimary && !a.settings {
				for _, target := range a.historyTargets {
					if target.box.contains(x, y) {
						a.selectedCard = target.id
						break
					}
				}
			}
			if e.Kind == experience.PointerScroll && a.workspace.contains(x, y) && !a.settings {
				a.historyTarget = min(max(0, a.historyHeight-a.workspace.h), max(0, a.historyTarget+e.ScrollY*5))
			}
			return true
		}
		if p.selecting || a.body.contains(x, y) {
			e.X = (x - a.body.x) / a.cellW
			e.Y = (y - a.body.y) / a.cellH
			a.remember(p.Handle(e))
			return true
		}
		if e.Kind == experience.PointerUp {
			a.remember(p.Handle(experience.Event{Kind: experience.PointerCancel}))
		}
		return true
	}
	return false
}

func (a *App) shortcut(e experience.Event) bool {
	mods := e.Modifiers
	ctrlshift := experience.ModControl | experience.ModShift
	if mods == ctrlshift {
		switch e.Keycode {
		case 33:
			if !e.Repeat {
				a.setSearch(!a.searchOpen)
			}
			return true
		case 35:
			if !e.Repeat {
				a.setView(!a.historyView)
			}
			return true
		case 46:
			if !e.Repeat {
				a.copy()
			}
			return true
		case 47:
			if !e.Repeat {
				a.paste()
			}
			return true
		case 20:
			if !e.Repeat {
				if err := a.addTab(); err != nil {
					a.say(err.Error())
				}
			}
			return true
		case 17:
			if !e.Repeat {
				a.closeTab()
			}
			return true
		}
	}
	if mods == experience.ModControl || mods == ctrlshift {
		switch e.Keycode {
		case 15:
			if !e.Repeat {
				direction := 1
				if mods == ctrlshift {
					direction = -1
				}
				a.switchTab(a.active + direction)
			}
			return true
		case 13, 78:
			a.zoom(1)
			return true
		case 12, 74:
			a.zoom(-1)
			return true
		case 11:
			a.prefs.FontSize = 15
			a.width = 0
			a.clearGlyphs()
			return true
		case 51:
			if !e.Repeat {
				a.setSettings(!a.settings)
				a.hover = "theme:0"
			}
			return true
		}
	}
	if !a.historyView && !a.searchOpen && mods == experience.ModShift && (e.Keycode == 104 || e.Keycode == 109) {
		lines := a.pane().snapshot.Rows - 2
		if e.Keycode == 109 {
			lines = -lines
		}
		a.pane().term.Scroll(lines)
		a.pane().ClearSelection()
		return true
	}
	if a.settings {
		if e.Key == experience.KeyEscape {
			a.setSettings(false)
			return true
		}
		ids := []string{"theme:0", "theme:1", "theme:2", "opacity", "glow", "motion", "reset"}
		if e.Key == experience.KeyTab {
			i := 0
			for k, id := range ids {
				if id == a.hover {
					i = k
					break
				}
			}
			delta := 1
			if mods.Has(experience.ModShift) {
				delta = -1
			}
			a.hover = ids[(i+len(ids)+delta)%len(ids)]
			return true
		}
		if e.Key == experience.KeyEnter || e.Key == experience.KeySpace {
			if !e.Repeat {
				a.activate(a.hover)
			}
			return true
		}
		if e.Key == experience.KeyLeft || e.Key == experience.KeyRight {
			d := float32(.05)
			if e.Key == experience.KeyLeft {
				d = -d
			}
			if a.hover == "opacity" {
				a.prefs.Opacity = min(1, max(.35, a.prefs.Opacity+d))
			}
			if a.hover == "glow" {
				a.prefs.Glow = min(1, max(0, a.prefs.Glow+d))
			}
			return true
		}
	}
	return false
}
func (a *App) zoom(delta float32) {
	a.prefs.FontSize = min(24, max(11, a.prefs.FontSize+delta))
	a.width = 0
	a.clearGlyphs()
	a.say(fmt.Sprintf("Font size %.0f", a.prefs.FontSize))
}
func (a *App) activate(id string) {
	if a.activateWorkspace(id) {
		return
	}
	switch id {
	case "new":
		if err := a.addTab(); err != nil {
			a.say(err.Error())
		}
	case "close":
		if a.host != nil {
			a.host.RequestClose()
		}
	case "min":
		if a.host != nil {
			a.host.Minimize()
		}
	case "max":
		if a.host != nil {
			a.host.SetMaximized(!a.host.Maximized())
		}
	case "appearance":
		a.setSettings(!a.settings)
		a.hover = "theme:0"
	case "motion":
		a.prefs.Motion = !a.prefs.Motion
	case "reset":
		a.prefs = DefaultPreferences()
		a.clearGlyphs()
	case "copy":
		a.copy()
	case "paste":
		a.paste()
	default:
		if s, ok := strings.CutPrefix(id, "tab:"); ok {
			i, err := strconv.Atoi(s)
			if err == nil && i < len(a.tabs) && i >= 0 {
				a.switchTab(i)
			}
		}
		if s, ok := strings.CutPrefix(id, "theme:"); ok {
			i, err := strconv.Atoi(s)
			if err == nil && i >= 0 && i < 3 {
				a.prefs.Palette = i
			}
		}
	}
	a.dirty = true
}

func (a *App) setSettings(open bool) {
	if open {
		a.searchOpen = false
	}
	a.settings = open
	if p := a.pane(); p != nil {
		if open {
			a.remember(p.term.Input(experience.Event{Kind: experience.KeyboardCancel}))
		} else {
			p.term.Focus(a.focused && !a.historyView && !a.searchOpen)
		}
	}
	a.dirty = true
}
func (a *App) adjustSlider(id string, x float32) {
	for _, b := range a.buttons {
		if b.id == id {
			v := min(1, max(0, (x-b.box.x)/b.box.w))
			if id == "opacity" {
				a.prefs.Opacity = .35 + v*.65
			} else {
				a.prefs.Glow = v
			}
			a.dirty = true
			return
		}
	}
}
func (a *App) copy() {
	if a.searchOpen {
		if a.searchSelect {
			a.copyText(a.searchText, "Search text copied")
		}
		return
	}
	if a.historyView {
		for _, card := range a.cards {
			if card.ID == a.selectedCard {
				a.copyText(card.Output, "Command output copied")
				return
			}
		}
		a.say("Select a command block to copy")
		return
	}
	text, ok := a.pane().Copy()
	if !ok {
		a.say("Select terminal text to copy")
		return
	}
	if a.host == nil {
		return
	}
	if err := a.host.WriteClipboard(text); err != nil {
		a.say("Copy: " + err.Error())
	} else {
		a.say("Selection copied")
	}
}
func (a *App) paste() {
	if a.historyView {
		a.say("Return to Live to enter a command")
		return
	}
	if a.host == nil {
		return
	}
	p := a.pane()
	search := a.searchOpen
	err := a.host.ReadClipboard(func(text string, err error) {
		if err != nil {
			a.say("Paste: " + err.Error())
			return
		}
		if a.closed || a.pane() != p || a.searchOpen != search || a.historyView || a.settings {
			a.say("Paste cancelled after changing session")
			return
		}
		if search {
			a.insertSearch(text)
			return
		}
		if err := p.Paste(text); err != nil {
			a.say("Paste: " + err.Error())
		}
		a.dirty = true
	})
	if err != nil {
		a.say("Paste: " + err.Error())
	}
}
func (a *App) resizeEdge(x, y float32) (kit.ResizeEdge, bool) {
	f := a.frame
	left, right := x < f.x+6, x > f.x+f.w-6
	top, bottom := y < f.y+6, y > f.y+f.h-6
	switch {
	case top && left:
		return kit.ResizeTopLeft, true
	case top && right:
		return kit.ResizeTopRight, true
	case bottom && left:
		return kit.ResizeBottomLeft, true
	case bottom && right:
		return kit.ResizeBottomRight, true
	case top:
		return kit.ResizeTop, true
	case bottom:
		return kit.ResizeBottom, true
	case left:
		return kit.ResizeLeft, true
	case right:
		return kit.ResizeRight, true
	}
	return 0, false
}
