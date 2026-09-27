package glass

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"github.com/codemodify/worldr/internal/terminal"
)

// Single-cell ASCII is by far the hottest terminal path. Slicing this retained
// string avoids allocating a rune slice and an escaped string for every cell.
const asciiGlyphs = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

func (a *App) layout(width, height int) {
	a.unit = max(.5, a.scale)
	w, h := float32(width)/a.unit, float32(height)/a.unit
	a.frame = rect{16, 16, max(100, w-32), max(100, h-32)}
	f := a.frame
	a.font = a.prefs.FontSize
	a.cellW = a.canvas.MeasureText(a.font, "M")
	a.cellH = float32(math.Ceil(float64(a.font * 1.48)))
	a.buttons = a.buttons[:0]
	add := func(id, label string, r rect) { a.buttons = append(a.buttons, button{id, label, r}) }
	add("min", "", rect{f.x + f.w - 123, f.y + 17, 30, 28})
	add("max", "", rect{f.x + f.w - 87, f.y + 17, 30, 28})
	add("close", "", rect{f.x + f.w - 51, f.y + 17, 30, 28})
	mainX, mainWidth, toolbarY, contentY := f.x+22, f.w-44, f.y+108, f.y+166
	a.rail = rect{}
	if w >= 900 {
		a.rail = rect{f.x + 18, f.y + 69, 174, max(80, f.h-119)}
		mainX, mainWidth, toolbarY, contentY = f.x+214, f.w-236, f.y+66, f.y+132
		rowHeight := min(float32(60), max(8, (a.rail.h-70)/float32(max(1, len(a.tabs)))))
		for i := range a.tabs {
			add(fmt.Sprintf("tab:%d", i), a.shellLabel(), rect{a.rail.x + 10, a.rail.y + 35 + float32(i)*rowHeight, a.rail.w - 20, max(1, rowHeight-4)})
		}
		if len(a.tabs) < 8 {
			add("new", "+  New session", rect{a.rail.x + 10, a.rail.y + 37 + float32(len(a.tabs))*rowHeight, a.rail.w - 20, 27})
		}
		add("appearance", "Appearance", rect{mainX + mainWidth - 104, toolbarY, 104, 30})
	} else {
		// Reserve the Appearance column even when eight compact session tabs
		// are open. Numeric labels preserve distinct targets at 420px width.
		add("appearance", "Appearance", rect{f.x + f.w - 125, f.y + 66, 104, 31})
		available := max(float32(0), f.w-180)
		newWidth := float32(36)
		if len(a.tabs) >= 8 {
			newWidth = 0
		}
		tabWidth := min(float32(188), max(float32(1), (available-newWidth)/float32(max(1, len(a.tabs)))))
		for i := range a.tabs {
			label := fmt.Sprintf("%02d  %s", a.tabs[i].id, a.shellLabel())
			if tabWidth < 72 {
				label = fmt.Sprintf("%02d", a.tabs[i].id)
			}
			add(fmt.Sprintf("tab:%d", i), label, rect{f.x + 22 + float32(i)*tabWidth, f.y + 66, max(1, tabWidth-5), 31})
		}
		if len(a.tabs) < 8 {
			add("new", "+", rect{f.x + 22 + float32(len(a.tabs))*tabWidth, f.y + 66, min(30, max(0, available)), 31})
		}
	}
	add("live", "Live", rect{mainX, toolbarY, 77, 30})
	add("blocks", "Blocks", rect{mainX + 85, toolbarY, 93, 30})
	add("search", "Find", rect{mainX + 186, toolbarY, 75, 30})
	a.workspace = rect{mainX, contentY, max(40, mainWidth), max(56, f.y+f.h-48-contentY)}
	a.body = rect{a.workspace.x + 16, a.workspace.y + 14, max(40, a.workspace.w-32), max(40, a.workspace.h-28)}
	add("copy", "Copy", rect{f.x + f.w - 155, f.y + f.h - 35, 56, 24})
	add("paste", "Paste", rect{f.x + f.w - 95, f.y + f.h - 35, 65, 24})
	if a.settings || a.drawer > .002 {
		drawerHeight := max(float32(0), min(332, f.h-80))
		drawerWidth := max(float32(0), min(306, f.w-44))
		a.settingsBox = rect{f.x + f.w - drawerWidth - 23 + 24*(1-a.drawer), f.y + max(0, min(108, f.h-drawerHeight-12)), drawerWidth, drawerHeight}
		s := a.settingsBox
		v := a.settingsScale()
		column := max(float32(0), (s.w-33)/3)
		for i, label := range []string{"Cyan", "Amber", "Iris"} {
			add(fmt.Sprintf("theme:%d", i), label, rect{s.x + 18 + float32(i)*column, s.y + 56*v, max(0, column-7), 32 * v})
		}
		add("opacity", "", rect{s.x + 20, s.y + 130*v, max(0, s.w-40), 24 * v})
		add("glow", "", rect{s.x + 20, s.y + 194*v, max(0, s.w-40), 24 * v})
		add("motion", "Motion", rect{s.x + 18, s.y + s.h - 57*v, max(0, min(128, s.w-132)), 33 * v})
		add("reset", "Reset", rect{s.x + max(0, s.w-96), s.y + s.h - 57*v, min(78, s.w), 33 * v})
	}
	if p := a.pane(); p != nil {
		a.remember(p.Resize(int(a.body.w/a.cellW), int(a.body.h/a.cellH)))
	}
	a.width, a.height = width, height
}

func (a *App) Draw(width, height int) render.Frame {
	a.glyphFrame++
	if len(a.glyphs) > 1024 && a.glyphFrame > 2 {
		// Keep the current/recent view resident even if it contains thousands of
		// distinct glyphs. Only departed content is eligible for retirement.
		for key, texture := range a.glyphs {
			if a.glyphLastUsed[key] < a.glyphFrame-2 {
				a.retired = append(a.retired, texture.ID())
				delete(a.glyphs, key)
				delete(a.glyphLastUsed, key)
			}
		}
	}
	a.layout(width, height)
	c := a.canvas
	c.Reset(width, height)
	c.SetLinearColor(true)
	f := a.frame
	// The native compositor supplies the real desktop through this glass.
	// Navigation and content occupy separate planes with quiet text interiors.
	alpha := a.opacity * (.85 + .15*a.entrance)
	a.roundFill(f, 26, scene.ColorHex(0x0a1921, alpha))
	glow := a.glow * (.75 + .25*a.activity)
	for i := 3; i >= 1; i-- {
		a.roundStroke(f, 26, float32(i)*3, a.accent.WithAlpha(glow*.016*float32(4-i)))
	}
	a.roundStroke(f, 26, 1, a.accent.WithAlpha(.35+.2*glow))
	a.roundStroke(rect{f.x + 5, f.y + 5, f.w - 10, f.h - 10}, 22, .7, scene.ColorHex(0xc5f4f7, .07))
	// Broken edge segments identify the navigation junctions. Nothing moves
	// behind terminal text or changes the input coordinate frame.
	a.line(f.x+31, f.y+2, f.x+133, f.y+2, 1.8, a.accent.WithAlpha(.8))
	a.line(f.x+133, f.y+2, f.x+141, f.y+8, 1, a.accent.WithAlpha(.65))
	a.line(f.x+141, f.y+8, f.x+207, f.y+8, 1, a.accent.WithAlpha(.45))
	a.line(f.x+f.w-164, f.y+f.h-2, f.x+f.w-46, f.y+f.h-2, 1.8, a.accent.WithAlpha(.72))
	a.line(f.x+23, f.y+54, f.x+f.w-23, f.y+54, .7, a.accent.WithAlpha(.15))
	// An open chevron and cursor mark use the same language as the live mode.
	a.line(f.x+26, f.y+24, f.x+33, f.y+30, 1.7, a.accent)
	a.line(f.x+33, f.y+30, f.x+26, f.y+36, 1.7, a.accent)
	a.line(f.x+39, f.y+36, f.x+47, f.y+36, 1.7, a.accent)
	a.text(f.x+61, f.y+21, 16, "worldr", scene.ColorHex(0xe0f1f3, 1))
	a.text(f.x+135, f.y+25, 10, "TERMINAL", scene.ColorHex(0x8dabba, .95))
	a.drawSessionRail()
	// The working directory is real shell context, not a decorative prompt.
	context := a.contextLabel()
	a.text(a.workspace.x+2, a.workspace.y-23, 10, truncate(context, max(1, int((a.workspace.w-8)/6.1))), scene.ColorHex(0x9bbbc6, .96))
	a.roundFill(a.workspace, 9, scene.ColorHex(0x020a10, .22))
	a.roundStroke(a.workspace, 9, .7, a.accent.WithAlpha(.15))
	reveal := .3 + .7*a.viewReveal
	a.line(a.workspace.x+10, a.workspace.y, a.workspace.x+10+min(74, a.workspace.w*.2)*reveal, a.workspace.y, 1.6, a.accent.WithAlpha(.68))
	a.line(a.workspace.x+a.workspace.w, a.workspace.y+a.workspace.h-35, a.workspace.x+a.workspace.w, a.workspace.y+a.workspace.h-10, 1.4, a.accent.WithAlpha(.4))
	// Common controls precede mode-specific controls; drawHistory/drawSearch
	// append their live hit targets without being painted a second time here.
	for _, b := range a.buttons {
		if strings.HasPrefix(b.id, "theme:") || b.id == "opacity" || b.id == "glow" || b.id == "motion" || b.id == "reset" {
			continue
		}
		a.drawButton(b)
	}
	if a.historyView {
		a.drawHistory()
	} else {
		a.drawTerminal()
	}
	a.drawTerminalStatus()
	if a.searchOpen || a.searchReveal > .002 {
		a.drawSearch()
	}
	if a.drawer > .002 {
		a.drawSettings()
	}
	a.dirty = false
	return c.Frame()
}

func (a *App) drawButton(b button) {
	a.drawButtonWithAlpha(b, 1)
}

func (a *App) drawSessionRail() {
	r := a.rail
	if r.w <= 0 {
		return
	}
	a.roundFill(r, 9, scene.ColorHex(0x021018, .19))
	a.text(r.x+10, r.y+9, 9, "SESSIONS", scene.ColorHex(0x8dabba, .95))
	a.text(r.x+r.w-26, r.y+9, 9, fmt.Sprintf("%02d", len(a.tabs)), a.accent.WithAlpha(.85))
	a.line(r.x+10, r.y+27, r.x+r.w-10, r.y+27, .7, a.accent.WithAlpha(.18))
	for _, b := range a.buttons {
		if b.id != fmt.Sprintf("tab:%d", a.active) {
			continue
		}
		// This link follows the selected session to its working plane; the
		// other session targets stay still when the main view changes.
		y, bridge := b.box.y+b.box.h/2, a.workspace.x-10
		a.line(b.box.x+b.box.w, y, bridge-4, y, 1, a.accent.WithAlpha(.35))
		a.line(bridge-4, y, bridge, y+4, 1, a.accent.WithAlpha(.35))
		a.line(bridge, y+4, bridge, a.workspace.y+15, 1, a.accent.WithAlpha(.35))
		a.line(bridge, a.workspace.y+15, a.workspace.x, a.workspace.y+15, 1, a.accent.WithAlpha(.35))
		break
	}
	if r.h > 180 {
		a.text(r.x+11, r.y+r.h-17, 9, "Ctrl+Tab  switch", scene.ColorHex(0x75939f, .85))
	}
}

func (a *App) drawTerminalStatus() {
	p := a.pane()
	if p == nil {
		return
	}
	f, w := a.frame, a.workspace
	y := f.y + f.h - 28
	status := "SHELL CONNECTED"
	if p.snapshot.Exited {
		status = "PROCESS EXITED"
	} else if a.historyView {
		status = "COMMAND HISTORY"
	} else if p.snapshot.ScrollOffset > 0 {
		status = fmt.Sprintf("SCROLLBACK +%d", p.snapshot.ScrollOffset)
	}
	available := max(0, f.x+f.w-174-w.x)
	if a.notice != "" {
		status = a.notice
	}
	a.circle(w.x+5, y+6, 2.2, 0, a.accent.WithAlpha(.75))
	a.text(w.x+17, y, 9, truncate(status, max(1, int((available-24)/5.5))), scene.ColorHex(0x9fbcc6, .94))
	if a.notice == "" && available > 350 {
		a.text(w.x+205, y, 9, fmt.Sprintf("%d columns / %d rows", p.snapshot.Cols, p.snapshot.Rows), scene.ColorHex(0x75939f, .85))
	}
}

func (a *App) drawButtonWithAlpha(b button, fade float32) {
	active := b.id == fmt.Sprintf("tab:%d", a.active) || b.id == "appearance" && a.settings || b.id == "live" && !a.historyView || b.id == "blocks" && a.historyView || b.id == "search" && a.searchOpen
	hover := a.hover == b.id || a.pressed == b.id
	mode := b.id == "live" || b.id == "blocks" || b.id == "search"
	if active || hover {
		al := float32(.09)
		if hover {
			al = .14
		}
		a.roundFill(b.box, 5, a.accent.WithAlpha(al*fade))
	}
	col := scene.ColorHex(0x92b3c1, .9)
	if active || hover {
		col = a.accent
	}
	col.A *= fade
	r := b.box
	if mode {
		a.roundStroke(r, 5, .7, a.accent.WithAlpha(.13*fade))
		if active {
			a.line(r.x+8, r.y+r.h-1, r.x+r.w-8, r.y+r.h-1, 1.3, a.accent.WithAlpha(.85*fade))
		}
		cx, cy := r.x+15, r.y+r.h/2
		switch b.id {
		case "live":
			a.line(cx-4, cy-4, cx+1, cy, 1.1, col)
			a.line(cx+1, cy, cx-4, cy+4, 1.1, col)
			a.line(cx+3, cy+4, cx+7, cy+4, 1.1, col)
		case "blocks":
			for i := range 3 {
				a.line(cx-4, cy-5+float32(i)*4, cx+5, cy-5+float32(i)*4, 1.2, col)
			}
		case "search":
			a.circle(cx-1, cy-1, 4, 1, col)
			a.line(cx+2, cy+2, cx+6, cy+6, 1, col)
		}
		a.text(r.x+30, r.y+(r.h-13.2)/2, 11, b.label, col)
		return
	}
	if strings.HasPrefix(b.id, "tab:") && a.rail.w > 0 {
		index, _ := strconv.Atoi(strings.TrimPrefix(b.id, "tab:"))
		if index < 0 || index >= len(a.tabs) {
			return
		}
		p := a.tabs[index]
		y := r.y + (r.h-13.2)/2
		if r.h >= 42 {
			y = r.y + 9
			status := "Running"
			if p.snapshot.Exited {
				status = "Exited"
			}
			if active && !p.snapshot.Exited {
				status = "Current session"
			}
			a.text(r.x+37, r.y+28, 9, status, scene.ColorHex(0x779aa9, fade))
		}
		a.text(r.x+10, y, 11, fmt.Sprintf("%02d", p.id), a.accent.WithAlpha(.7*fade))
		a.text(r.x+37, y, 11, truncate(b.label, max(1, int((r.w-45)/6.6))), col)
		if active {
			a.line(r.x+1, r.y+7, r.x+1, r.y+r.h-7, 1.5, a.accent.WithAlpha(.85*fade))
		}
		return
	}
	switch b.id {
	case "min":
		a.line(r.x+9, r.y+16, r.x+21, r.y+16, 1.3, col)
	case "max":
		a.line(r.x+10, r.y+8, r.x+20, r.y+8, 1.2, col)
		a.line(r.x+20, r.y+8, r.x+20, r.y+19, 1.2, col)
		a.line(r.x+20, r.y+19, r.x+10, r.y+19, 1.2, col)
		a.line(r.x+10, r.y+19, r.x+10, r.y+8, 1.2, col)
	case "close":
		if hover {
			col = scene.ColorHex(0xffa6a7, fade)
		}
		a.line(r.x+10, r.y+9, r.x+20, r.y+19, 1.3, col)
		a.line(r.x+20, r.y+9, r.x+10, r.y+19, 1.3, col)
	default:
		size := min(float32(11), r.h*.5)
		if b.id == "new" && a.rail.w == 0 {
			size = min(20, r.h*.75)
		}
		if size <= 0 {
			return
		}
		padding := float32(16)
		if strings.HasPrefix(b.id, "tab:") && r.w < 40 {
			size, padding = min(10, max(6, (r.w-4)/1.25)), 4
		}
		text := truncate(b.label, max(1, int((r.w-padding)/(size*.6))))
		x := r.x + (r.w-a.canvas.MeasureText(size, text))/2
		a.text(x, r.y+(r.h-size*1.2)/2, size, text, col)
	}
	if active && strings.HasPrefix(b.id, "tab:") {
		a.line(r.x+8, r.y+r.h-1, r.x+r.w-8, r.y+r.h-1, 1.4, a.accent.WithAlpha(.85*fade))
	}
}

func (a *App) drawSettings() {
	s := a.settingsBox
	fade := a.drawer
	v := a.settingsScale()
	a.roundFill(s, 12, scene.ColorHex(0x091a24, .99*fade))
	a.roundStroke(s, 12, 1, a.accent.WithAlpha(.45*fade))
	a.text(s.x+18, s.y+19*v, 13*v, "APPEARANCE", a.accent.WithAlpha(fade))
	for _, b := range a.buttons {
		if strings.HasPrefix(b.id, "theme:") {
			a.drawButtonWithAlpha(b, fade)
			if b.id == fmt.Sprintf("theme:%d", a.prefs.Palette) {
				a.roundStroke(b.box, 5, 1, a.accent.WithAlpha(.75*fade))
			}
		}
		if b.id == "opacity" || b.id == "glow" {
			value := a.prefs.Glow
			label := "EDGE LIGHT"
			if b.id == "opacity" {
				value = (a.prefs.Opacity - .35) / .65
				label = fmt.Sprintf("GLASS OPACITY                         %d%%", int(a.prefs.Opacity*100))
			}
			a.text(b.box.x, b.box.y-18*v, 10*v, label, scene.ColorHex(0x91b1c0, fade))
			y := b.box.y + 12*v
			a.line(b.box.x, y, b.box.x+b.box.w, y, 2, scene.ColorHex(0x38515e, fade))
			a.line(b.box.x, y, b.box.x+b.box.w*value, y, 2, a.accent.WithAlpha(fade*.85))
			a.circle(b.box.x+b.box.w*value, y, 5*v, 0, a.accent.WithAlpha(fade))
		}
		if b.id == "motion" || b.id == "reset" {
			if b.id == "motion" {
				b.label = "Motion: off"
				if a.prefs.Motion {
					b.label = "Motion: on"
				}
			}
			a.drawButtonWithAlpha(b, fade)
		}
	}
}

func (a *App) settingsScale() float32 { return max(0, min(1, a.settingsBox.h/280)) }

func (a *App) drawTerminal() {
	p := a.pane()
	if p == nil {
		return
	}
	s := p.snapshot
	// Content remains fully legible throughout a tab transition. Only its
	// position settles; incoming PTY output never restarts this movement.
	alpha := float32(1)
	shift := 4 * (1 - a.tabReveal)
	for row := 0; row < s.Rows; row++ {
		for col := 0; col < s.Cols; col++ {
			i := row*s.Cols + col
			if i >= len(s.Cells) {
				continue
			}
			cell := s.Cells[i]
			if cell.Width == 0 {
				continue
			}
			x, y := a.body.x+float32(col)*a.cellW+shift, a.body.y+float32(row)*a.cellH
			fg, bg := cell.Foreground, cell.Background
			if cell.Reverse {
				fg, bg = bg, fg
			}
			if bg != (terminal.Color{R: 8, G: 16, B: 23}) {
				a.rect(rect{x, y, a.cellW * float32(max(1, cell.Width)), a.cellH}, termColor(bg, alpha))
			}
			if a.searchCell(row, col) {
				a.rect(rect{x, y, a.cellW * float32(max(1, cell.Width)), a.cellH}, scene.ColorHex(0xf3c77b, .26))
			}
			if p.Selected(i) {
				a.rect(rect{x, y, a.cellW * float32(max(1, cell.Width)), a.cellH}, a.accent.WithAlpha(.25))
			}
			if cell.Chars[0] == 0 || cell.Chars[0] == ' ' {
				continue
			}
			supported := !cell.Italic
			var text string
			if r := cell.Chars[0]; r >= 32 && r <= 126 && cell.Chars[1] == 0 {
				text = asciiGlyphs[r-32 : r-31]
			} else {
				count := 0
				for _, r := range cell.Chars {
					if r == 0 {
						break
					}
					count++
					supported = supported && a.canvas.HasGlyph(r)
				}
				text = string(cell.Chars[:count])
			}
			if fg == (terminal.Color{R: 220, G: 235, B: 238}) {
				fg = terminal.Color{R: 215, G: 237, B: 240}
			}
			color := termColor(fg, alpha)
			if supported {
				a.text(x, y+1, a.font, text, color)
				if cell.Bold {
					a.text(x+.35, y+1, a.font, text, color.WithAlpha(.6*alpha))
				}
			} else {
				a.drawFallback(x, y, text, cell, fg)
			}
			if cell.Underline {
				a.line(x, y+a.cellH-3, x+a.cellW*float32(cell.Width), y+a.cellH-3, 1, color)
			}
			if cell.Strike {
				a.line(x, y+a.cellH*.5, x+a.cellW*float32(cell.Width), y+a.cellH*.5, 1, color)
			}
		}
	}
	if s.ScrollOffset == 0 && s.Cursor.Visible && s.Cursor.Col < s.Cols && s.Cursor.Row < s.Rows && (!a.focused || a.cursorOn) {
		x, y := a.body.x+float32(s.Cursor.Col)*a.cellW+shift, a.body.y+float32(s.Cursor.Row)*a.cellH
		if !a.focused {
			a.roundStroke(rect{x, y, a.cellW, a.cellH}, 1, 1, a.accent.WithAlpha(.4))
		} else {
			switch s.Cursor.Shape {
			case 2:
				a.rect(rect{x, y + a.cellH - 3, a.cellW, 2}, a.accent)
			case 3:
				a.rect(rect{x, y, 1.7, a.cellH - 2}, a.accent)
			default:
				a.rect(rect{x, y, a.cellW, a.cellH - 1}, a.accent.WithAlpha(.34))
				a.line(x, y+a.cellH-1, x+a.cellW, y+a.cellH-1, 1, a.accent)
			}
		}
	}
	if s.ScrollOffset > 0 && s.ScrollbackLen > 0 {
		track := a.body.h - 16
		offset := float32(s.ScrollOffset) / float32(s.ScrollbackLen)
		a.line(a.body.x+a.body.w+10, a.body.y+8+track*(1-offset), a.body.x+a.body.w+10, a.body.y+min(a.body.h, track*(1-offset)+30), 2, a.accent.WithAlpha(.55))
	}
}
func termColor(c terminal.Color, alpha float32) scene.Color {
	return scene.Color{R: float32(c.R) / 255, G: float32(c.G) / 255, B: float32(c.B) / 255, A: alpha}
}

func (a *App) drawFallback(x, y float32, text string, cell terminal.Cell, foreground terminal.Color) {
	key := glyphKey{text: text, width: max(1, cell.Width), size: int(a.font * a.unit * 100), bold: cell.Bold, italic: cell.Italic, foreground: foreground}
	if a.glyphs == nil {
		a.glyphs = make(map[glyphKey]*render.Texture)
	}
	if a.glyphLastUsed == nil {
		a.glyphLastUsed = make(map[glyphKey]uint64)
	}
	a.glyphLastUsed[key] = a.glyphFrame
	t := a.glyphs[key]
	if t == nil {
		w, h := int(math.Ceil(float64(a.cellW*float32(key.width)*a.unit))), int(math.Ceil(float64(a.cellH*a.unit)))
		im := image.NewRGBA(image.Rect(0, 0, w, h))
		a.fallback.Theme.FontSize = float64(a.font * a.unit)
		a.fallback.Theme.Font = "Monospace"
		if cell.Bold {
			a.fallback.Theme.Font += " Bold"
		}
		if cell.Italic {
			a.fallback.Theme.Font += " Italic"
		}
		if err := a.fallback.DrawLabel(im, im.Rect, text, color.RGBA{foreground.R, foreground.G, foreground.B, 255}); err != nil {
			a.remember(err)
			return
		}
		var err error
		t, err = render.NewTexture(w, h, im.Pix)
		if err != nil {
			a.remember(err)
			return
		}
		a.glyphs[key] = t
	}
	w, h := t.Size()
	a.canvas.Image(t, x*a.unit, y*a.unit, float32(w), float32(h))
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return ""
	}
	return string(r[:n-1]) + "…"
}
func (a *App) text(x, y, size float32, s string, c scene.Color) {
	a.canvas.Text(x*a.unit, y*a.unit, size*a.unit, s, c)
}
func (a *App) rect(r rect, c scene.Color) {
	a.canvas.Rect(r.x*a.unit, r.y*a.unit, r.w*a.unit, r.h*a.unit, c)
}
func (a *App) line(x, y, x2, y2, width float32, c scene.Color) {
	a.canvas.Line(x*a.unit, y*a.unit, x2*a.unit, y2*a.unit, width*a.unit, c)
}
func (a *App) circle(x, y, r, width float32, c scene.Color) {
	a.canvas.Circle(x*a.unit, y*a.unit, r*a.unit, width*a.unit, c)
}
func (a *App) roundFill(r rect, radius float32, c scene.Color) {
	radius = min(radius, min(r.w, r.h)/2)
	var points [52][2]float32
	n := 0
	for corner := 0; corner < 4; corner++ {
		cx, cy := r.x+radius, r.y+radius
		if corner == 1 || corner == 2 {
			cx = r.x + r.w - radius
		}
		if corner >= 2 {
			cy = r.y + r.h - radius
		}
		for i := 0; i <= 12; i++ {
			angle := float64(corner)*math.Pi/2 + math.Pi + float64(i)*math.Pi/24
			points[n] = [2]float32{(cx + radius*float32(math.Cos(angle))) * a.unit, (cy + radius*float32(math.Sin(angle))) * a.unit}
			n++
		}
	}
	a.canvas.FillConvex(c, points[:n]...)
}
func (a *App) roundStroke(r rect, radius, width float32, c scene.Color) {
	radius = min(radius, min(r.w, r.h)/2)
	a.line(r.x+radius, r.y, r.x+r.w-radius, r.y, width, c)
	a.line(r.x+r.w, r.y+radius, r.x+r.w, r.y+r.h-radius, width, c)
	a.line(r.x+r.w-radius, r.y+r.h, r.x+radius, r.y+r.h, width, c)
	a.line(r.x, r.y+r.h-radius, r.x, r.y+radius, width, c)
	for i := 0; i < 4; i++ {
		cx, cy := r.x+radius, r.y+radius
		if i == 1 || i == 2 {
			cx = r.x + r.w - radius
		}
		if i >= 2 {
			cy = r.y + r.h - radius
		}
		start := float32(math.Pi + float64(i)*math.Pi/2)
		// Arc measures its outer radius; Line centers its width on the path.
		// Offset the arc so thin rails and broad glow join at the same radius.
		a.canvas.Arc(cx*a.unit, cy*a.unit, (radius+width/2)*a.unit, start, start+math.Pi/2, width*a.unit, c)
	}
}
