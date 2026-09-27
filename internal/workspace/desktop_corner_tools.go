package workspace

const skinCornerDockIndex = 2000

// Corner tools leave the main display available for authored application
// interfaces. They use the same completed-click capture as the full launcher.
func (w *Workspace) skinCornerEntries() []skinDesktopEntry {
	x := (float32(w.width)-w.ox)/w.scale - 334
	y := (float32(w.height)-w.oy)/w.scale - 40
	entries := []skinDesktopEntry{
		{kind: "launcher", label: "Tools", hint: "Find tools and open windows"},
		{kind: "overview", label: "Windows", hint: "See all open windows"},
		{kind: "skins", label: "Skins", hint: "Customize window and control appearance"},
		{kind: "help", label: "Help", hint: "Workspace controls and shortcuts"},
	}
	for i := range entries {
		entries[i].bounds = box{x + float32(i)*80, y, 74, 30}
	}
	return entries
}

func (w *Workspace) drawSkinCornerTools() {
	background := w.desktopChromeColor("surface", 0x142033)
	edge := w.desktopChromeColor("border", 0x506981)
	text := w.desktopChromeColor("text", 0xdde8f0)
	accent := w.desktopChromeColor("accent", 0x8fbfdf)
	entries := w.skinCornerEntries()
	for i, entry := range entries {
		b := entry.bounds
		w.rect(b.x, b.y, b.w, b.h, background, .95)
		color, alpha := edge, float32(.7)
		if w.applicationDockHover == skinCornerDockIndex+i {
			color, alpha = accent, 1
			w.rect(b.x, b.y, b.w, b.h, accent, .10)
		}
		w.line(b.x, b.y, b.x+b.w, b.y, 1, color, alpha)
		w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, color, alpha*.6)
		w.text(b.x+10, b.y+9, 12, entry.label, text, 1)
	}
	if i := w.applicationDockHover - skinCornerDockIndex; i >= 0 && i < len(entries) {
		b := entries[i].bounds
		x := entries[0].bounds.x
		w.rect(x, b.y-31, 314, 25, background, .98)
		w.text(x+8, b.y-23, 10, entries[i].hint, text, 1)
	}
}
