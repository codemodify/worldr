package workspace

// Rail targets share the dock's completed-click capture, but use a separate
// index range so changing appearance cannot activate an old dock press.
const skinDesktopDockIndex = 1000

func (w *Workspace) skinDesktopDockBase() int {
	if w.activeSkin != nil && w.activeSkin.Desktop.Chrome == "corner-tools" {
		return skinCornerDockIndex
	}
	return skinDesktopDockIndex
}

type skinDesktopEntry struct {
	kind, label, hint string
	bounds            box
	tab               bool
}

func (w *Workspace) skinDesktopChromeVisible() bool {
	return w.desktop && w.activeSkin != nil && (w.activeSkin.Desktop.Chrome == "slate-tabs" || w.activeSkin.Desktop.Chrome == "corner-tools")
}

// Rails attach to the physical screen edges, including the space outside the
// workspace's centered design area on wide or tall displays.
func (w *Workspace) skinDesktopRails() (left, right box) {
	x, y := -w.ox/w.scale, -w.oy/w.scale
	r := (float32(w.width) - w.ox) / w.scale
	h := float32(w.height) / w.scale
	return box{x, y, 80, h}, box{r - 80, y, 80, h}
}

func (w *Workspace) skinDesktopEntries() []skinDesktopEntry {
	if w.activeSkin != nil && w.activeSkin.Desktop.Chrome == "corner-tools" {
		return w.skinCornerEntries()
	}
	left, right := w.skinDesktopRails()
	entries := make([]skinDesktopEntry, 0, 20)
	for i, entry := range []skinDesktopEntry{
		{kind: "launcher", label: "Programs", hint: "Find tools, windows and spaces"},
		{kind: "files", label: "Files", hint: "Browse projects and documents"},
		{kind: "research", label: "Records", hint: "Open the research workbench"},
		{kind: "note", label: "Diary", hint: "Start a native note"},
		{kind: "settings", label: "Merrick", hint: "Open appearance and workspace settings"},
		{kind: "overview", label: "Personal", hint: "See your open windows"},
	} {
		entry.bounds = box{right.x - 32, right.y + 50 + float32(i)*80, 96, 78}
		entry.tab = true
		entries = append(entries, entry)
	}
	entries = append(entries,
		skinDesktopEntry{kind: "overview", label: "Workspace", hint: "See your open windows", bounds: box{left.x + 16, left.y + 16, 64, 26}},
		skinDesktopEntry{kind: "help", label: "Help", hint: "Workspace controls and shortcuts", bounds: box{right.x + 16, right.y + 16, 48, 26}},
	)
	for i, rail := range []box{left, right} {
		x, y := rail.x, rail.y+rail.h-226
		if i == 0 {
			x += 16
		}
		entries = append(entries, skinDesktopEntry{kind: "overview", label: "User", hint: "See your open windows", bounds: box{x, y, 64, 66}})
		for i, entry := range []skinDesktopEntry{
			{kind: "files", label: "Files", hint: "Browse projects and documents"},
			{kind: "settings", label: "System", hint: "Open workspace settings"},
			{kind: "terminal", label: "Terminal", hint: "Start a native shell"},
			{kind: "help", label: "Help", hint: "Workspace controls and shortcuts"},
			{kind: "settings", label: "Settings", hint: "Open workspace settings"},
		} {
			entry.bounds = box{x, y + 74 + float32(i)*24, 64, 24}
			entries = append(entries, entry)
		}
	}
	return entries
}

func (w *Workspace) skinDesktopTargetAt(x, y float32) int {
	for i, entry := range w.skinDesktopEntries() {
		if entry.bounds.contains(x, y) {
			return w.skinDesktopDockBase() + i
		}
	}
	return -1
}

func (w *Workspace) skinDesktopContains(x, y float32) bool {
	if w.activeSkin != nil && w.activeSkin.Desktop.Chrome == "corner-tools" {
		return w.skinDesktopTargetAt(x, y) >= 0
	}
	left, right := w.skinDesktopRails()
	return left.contains(x, y) || right.contains(x, y) || w.skinDesktopTargetAt(x, y) >= 0
}

func skinDesktopEntryAvailable(entry skinDesktopEntry, available [len(applicationDockEntries)]bool) bool {
	switch entry.kind {
	case "settings", "skins", "help", "overview", "launcher":
		return true
	default:
		for i, dock := range applicationDockEntries {
			if entry.kind == dock.kind {
				return available[i]
			}
		}
		return false
	}
}

func (w *Workspace) launchSkinDesktopEntry(index int) {
	if !w.skinDesktopChromeVisible() {
		return
	}
	entries := w.skinDesktopEntries()
	i := index - w.skinDesktopDockBase()
	if i < 0 || i >= len(entries) {
		return
	}
	switch entries[i].kind {
	case "settings":
		w.openSettings()
	case "skins":
		w.openSettings()
		w.settingsCategory = settingsSkins
	case "help":
		w.openHelp()
	case "overview":
		_ = w.Dispatch(Action{Kind: ToggleApplicationOverview})
	default:
		for dockIndex, entry := range applicationDockEntries {
			if entry.kind == entries[i].kind {
				w.launchApplicationDockEntry(dockIndex)
				return
			}
		}
	}
}

func (w *Workspace) desktopChromeColor(token string, fallback uint32) uint32 {
	if w.activeSkin != nil {
		if _, ok := w.activeSkin.Palette[token]; ok {
			c := w.activeSkin.Color(token)
			return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
	}
	return fallback
}

func (w *Workspace) desktopChromePolygon(rgb uint32, alpha float32, points ...[2]float32) {
	for i := range points {
		points[i][0] = w.ox + points[i][0]*w.scale
		points[i][1] = w.oy + points[i][1]*w.scale
	}
	w.canvas.FillConvex(w.color(rgb, alpha), points...)
}

func (w *Workspace) desktopChromeTab(b box, fill, edge uint32, shadow bool) {
	x, y, r, bottom := b.x, b.y, b.x+b.w, b.y+b.h
	// A rectangular cap and two convex lower pieces form the stepped tail.
	w.rect(x, y, b.w, 36, fill, 1)
	w.desktopChromePolygon(fill, 1, [2]float32{x + 28, y + 36}, [2]float32{r, y + 36}, [2]float32{r, bottom - 22}, [2]float32{r - 22, bottom}, [2]float32{x + 28, bottom})
	w.desktopChromePolygon(fill, 1, [2]float32{x, y + 36}, [2]float32{x + 28, y + 36}, [2]float32{x + 28, y + 60})
	if shadow {
		return
	}
	w.line(x, y, r, y, 1, edge, .75)
	w.line(x, y, x, y+36, 1, edge, .52)
	w.line(x, y+36, x+28, y+60, 1, edge, .4)
	w.line(x+28, y+60, x+28, bottom, 1, edge, .35)
	w.line(x+28, bottom, r-22, bottom, 1, edge, .3)
	w.line(r-22, bottom, r, bottom-22, 1, edge, .32)
	w.line(r, bottom-22, r, y, 1, edge, .28)
}

// drawSkinDesktopChrome replaces the default dock/pad only when the package
// requests this desktop composition. Application and modal painting stay with
// the ordinary workspace renderer.
func (w *Workspace) drawSkinDesktopChrome() bool {
	if !w.skinDesktopChromeVisible() {
		return false
	}
	if w.activeSkin.Desktop.Chrome == "corner-tools" {
		w.drawSkinCornerTools()
		return true
	}
	rail := w.desktopChromeColor("desktop-rail", 0x49566e)
	edge := w.desktopChromeColor("desktop-edge", 0x778591)
	text := w.desktopChromeColor("desktop-text", 0xd4dee2)
	active := w.desktopChromeColor("desktop-active", 0x657389)
	navy := w.desktopChromeColor("desktop-shadow", 0x192939)
	left, right := w.skinDesktopRails()
	w.rect(left.x+left.w, left.y, 4, left.h, navy, .4)
	w.rect(right.x-4, right.y, 4, right.h, navy, .4)
	for i, b := range []box{left, right} {
		w.rect(b.x, b.y, b.w, b.h, rail, 1)
		strip := b.x
		if i == 1 {
			strip += b.w - 15
		}
		w.rect(strip, b.y, 15, b.h, navy, .22)
		w.line(strip, b.y, strip, b.y+b.h, 1, navy, .8)
		w.line(b.x+b.w, b.y, b.x+b.w, b.y+b.h, 1, navy, .95)
		w.line(b.x+1, b.y, b.x+1, b.y+b.h, 1, edge, .45)
	}
	entries := w.skinDesktopEntries()
	selected := w.activeApplicationDockKind()
	availableEntries := w.applicationDockAvailability()
	for i, entry := range entries {
		b := entry.bounds
		hovered := w.applicationDockHover == skinDesktopDockIndex+i
		available := skinDesktopEntryAvailable(entry, availableEntries)
		fill := navy
		if entry.tab && i >= 3 {
			fill = rail
		}
		if hovered {
			fill = active
		}
		if entry.tab {
			w.desktopChromeTab(box{b.x + 2, b.y + 3, b.w, b.h}, navy, edge, true)
			w.desktopChromeTab(b, fill, edge, false)
			w.rect(b.x+1, b.y+1, b.w-2, 15, edge, .1)
		} else {
			w.rect(b.x+1, b.y+2, b.w, b.h, navy, .35)
			w.rect(b.x, b.y, b.w, b.h, fill, .94)
			w.line(b.x, b.y, b.x+b.w, b.y, 1, edge, .45)
		}
		size, inset := float32(12), float32(4)
		if entry.tab {
			size, inset = 15, 7
		} else if entry.label == "Workspace" {
			size = 10
		}
		alpha := float32(1)
		if !available {
			alpha = .48
		}
		w.text(b.x+inset, b.y+5, size, entry.label, text, alpha)
		if selected != "" && selected == entry.kind || entry.kind == "overview" && w.m.applicationState.Overview {
			w.rect(b.x+4, b.y+b.h-6, min(18, b.w-8), 2, text, .75)
		}
		if !entry.tab && b.h == 24 {
			w.rect(b.x-10, b.y+10, 4, 3, edge, .75)
		}
	}
	if i := w.applicationDockHover - skinDesktopDockIndex; i >= 0 && i < len(entries) {
		entry := entries[i]
		b := entry.bounds
		x := b.x - 258
		if b.x < left.x+left.w {
			x = left.x + left.w + 12
		}
		hint := entry.hint
		if !skinDesktopEntryAvailable(entry, availableEntries) {
			hint = "Launcher unavailable"
		}
		w.rect(x, b.y+4, 246, 28, navy, .97)
		w.line(x, b.y+4, x+246, b.y+4, 1, edge, .8)
		w.text(x+9, b.y+11, 10, hint, text, 1)
	}
	return true
}
