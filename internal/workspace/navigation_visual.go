package workspace

import (
	"fmt"
	"math"

	"github.com/codemodify/worldr/internal/scene"
)

// Navigator is an orientation layer around live applications. Its quiet,
// consistent chrome makes movement and spatial relationships carry the design.
const (
	navigationVisualBackground uint32 = 0x0b131b
	navigationVisualPanel      uint32 = 0x12212b
	navigationVisualRaised     uint32 = 0x1a2c36
	navigationVisualEdge       uint32 = 0x304650
	navigationVisualText       uint32 = 0xe6eef0
	navigationVisualMuted      uint32 = 0x91a6af
	navigationVisualMint       uint32 = 0xb5ded1
)

func (w *Workspace) drawNavigationBackdrop() {
	if w.navigation == nil {
		return
	}
	width, height := float32(w.width), float32(w.height)
	w.navigationVisualRect(box{0, 0, width, height}, navigationVisualBackground, 1)
	glow := scene.ColorHex(0x355566, .17)
	w.canvas.RadialGradient(width*.52, height*.2, max(width, height)*.78, glow, glow.WithAlpha(0))
	// A few structural lines orient the canvas without posing as charts or
	// decorative telemetry. They remain fixed while applications travel.
	edge := scene.ColorHex(navigationVisualEdge, .20)
	for i := range 3 {
		x := width*.66 + float32(i)*width*.16
		w.canvas.Line(x, 0, x-width*.36, height, 1, edge)
	}
	n, l := w.navigation, w.navigation.layout
	u := max(.5, l.unit)
	for _, project := range l.projects {
		w.navigationVisualRect(project.bounds, navigationVisualPanel, .72)
		w.navigationVisualOutline(project.bounds, navigationVisualEdge, .7)
		id := fmt.Sprintf("project:%d", project.space)
		if n.hover == id || n.keyboard == id {
			w.navigationVisualOutline(navigationVisualInset(project.bounds, 2*u, 2*u), navigationVisualMint, .4)
		}
	}
	poses := n.motion.poses()
	for _, card := range l.apps {
		if card.slot < 0 || card.slot >= len(poses) || !poses[card.slot].visible {
			continue
		}
		b := poses[card.slot].bounds
		if b.w < 8*u || b.h < 8*u {
			continue
		}
		outer := box{b.x - u, b.y - 28*u, b.w + 2*u, b.h + 29*u}
		w.navigationVisualRect(box{outer.x + 4*u, outer.y + 5*u, outer.w, outer.h}, 0x03090e, .35)
		w.navigationVisualRect(outer, navigationVisualPanel, 1)
	}
}

func (w *Workspace) drawNavigationChrome() {
	n := w.navigation
	if n == nil {
		return
	}
	l := n.layout
	u := max(.5, l.unit)
	// The layout owns every actionable rectangle. This is presentation only.
	buttons := []struct {
		id, label string
		bounds    box
		active    bool
	}{
		{"back", "Back", l.back, false},
		{"home", "Home", l.home, n.level == navigationHome},
		{"overview", "Overview", l.overview, n.level == navigationProject},
		{"search", "Search", l.search, w.commands != nil && w.commands.open},
		{"tools", "Tools", l.tools, n.toolsOpen},
		{"motion", "Motion", l.motion, n.motionEnabled},
	}
	for _, button := range buttons {
		w.navigationVisualButton(button.bounds, button.id, button.label, button.active, false)
	}
	// A breadcrumb uses the free space between navigation and utilities; it
	// never competes with controls on narrow viewports.
	crumb := box{l.overview.x + l.overview.w + 20*u, l.home.y, l.search.x - l.overview.x - l.overview.w - 40*u, l.home.h}
	path := "Projects"
	if n.level != navigationHome {
		path = "Home  /  " + w.m.applicationState.spaceName(w.m.applicationState.Space)
	}
	if crumb.w > 100*u {
		w.navigationVisualLabel(crumb, 12*u, path, navigationVisualMuted)
	}
	heading := "Projects"
	if n.level != navigationHome {
		heading = w.m.applicationState.spaceName(w.m.applicationState.Space)
	}
	if n.level == navigationApp && w.application.Title != "" {
		heading = w.application.Title
	}
	w.navigationVisualContentLabel(box{l.content.x, 67 * u, l.content.w, 32 * u}, 22*u, heading, navigationVisualText)
	for _, tab := range l.tabs {
		w.navigationVisualButton(tab.bounds, fmt.Sprintf("tab:%d", tab.space), tab.name, tab.active, true)
	}
	for _, project := range l.projects {
		b := project.bounds
		w.navigationVisualContentLabel(box{b.x + 18*u, b.y + 10*u, b.w - 96*u, 28 * u}, 17*u, project.name, navigationVisualText)
		count := fmt.Sprintf("%d apps", project.count)
		if project.count == 1 {
			count = "1 app"
		}
		w.navigationVisualContentLabel(box{b.x + b.w - 76*u, b.y + 12*u, 62 * u, 24 * u}, 11*u, count, navigationVisualMuted)
		w.canvas.Line(b.x+18*u, b.y+45*u, b.x+b.w-18*u, b.y+45*u, 1, scene.ColorHex(navigationVisualEdge, .5))
		if project.count == 0 {
			w.navigationVisualContentLabel(box{b.x + 18*u, b.y + b.h/2 - 12*u, b.w - 36*u, 24 * u}, 13*u, "No open apps", navigationVisualMuted)
		}
	}
	poses := n.motion.poses()
	var activeContent box
	for _, card := range l.apps {
		if card.active && card.slot >= 0 && card.slot < len(poses) && poses[card.slot].visible {
			activeContent = poses[card.slot].bounds
			break
		}
	}
	for _, card := range l.apps {
		if card.slot < 0 || card.slot >= len(poses) || !poses[card.slot].visible {
			continue
		}
		b := poses[card.slot].bounds
		if b.w < 20*u || b.h < 10*u {
			continue
		}
		title := box{b.x, b.y - 28*u, b.w, 28 * u}
		outer := box{b.x - u, title.y - u, b.w + 2*u, b.h + 30*u}
		// The scene puts the active client in front during crossing motions.
		// Canvas chrome has no depth buffer, so withhold covered sibling chrome
		// until it separates instead of drawing text through the active client.
		if !card.active && navigationVisualIntersects(outer, activeContent) {
			continue
		}
		color, alpha := navigationVisualEdge, float32(.85)
		id := fmt.Sprintf("app:%d", card.slot)
		if card.active || n.hover == id || n.keyboard == id {
			color, alpha = navigationVisualMint, .74
		}
		w.navigationVisualOutline(outer, color, alpha)
		if n.keyboard == id {
			w.navigationVisualOutline(navigationVisualInset(outer, -2*u, -2*u), navigationVisualMint, 1)
		}
		w.canvas.Line(title.x, title.y+title.h-.5, title.x+title.w, title.y+title.h-.5, 1, scene.ColorHex(navigationVisualEdge, .75))
		labelX := title.x + 10*u
		if card.active {
			w.navigationVisualRect(box{title.x + 9*u, title.y + 11*u, 4 * u, 6 * u}, navigationVisualMint, 1)
			labelX += 10 * u
		}
		name := card.title
		if name == "" {
			name = card.key
		}
		w.navigationVisualContentLabel(box{labelX, title.y, title.x + title.w - labelX - 8*u, title.h}, 12*u, name, navigationVisualText)
		if card.minimized {
			w.navigationVisualContentLabel(box{b.x + 12*u, b.y + b.h/2 - 12*u, b.w - 24*u, 24 * u}, 12*u, "Minimized", navigationVisualMuted)
		}
	}
	if n.level != navigationHome {
		w.navigationVisualButton(l.previous, "previous", "Previous", false, false)
		w.navigationVisualButton(l.next, "next", "Next", false, false)
	}
	hint := "Open a project  ·  Tab to browse  ·  Enter to open"
	if n.level == navigationProject {
		hint = "Open an app  ·  Esc to projects  ·  Ctrl+Alt+Space to search"
	} else if n.level == navigationApp {
		hint = "Ctrl+Alt+O overview  ·  Ctrl+Alt+J/K switch apps"
	}
	// The footer reveals complete names when a compact preview title is clipped.
	for _, card := range l.apps {
		id := navigationID("app", card.slot)
		if card.bounds.w > 0 && (n.hover == id || n.keyboard == id) {
			hint = card.title + "  ·  Click to open"
			break
		}
	}
	if n.toolsOpen {
		hint = "Tab to browse tools  ·  Enter to select  ·  Esc to close"
	}
	hintColor := navigationVisualMuted
	if w.applicationNotice != "" {
		hint, hintColor = w.applicationNotice, navigationVisualText
	}
	footerWidth := l.content.w
	if l.pages > 1 {
		footerWidth = max(0, l.pagePrevious.x-l.content.x-24*u)
	}
	w.navigationVisualContentLabel(box{l.content.x, l.height - 38*u, footerWidth, 30 * u}, 11*u, hint, hintColor)
	if l.pages > 1 {
		w.navigationVisualButton(l.pagePrevious, "pagePrevious", "", false, false)
		w.navigationVisualButton(l.pageNext, "pageNext", "", false, false)
		middle := box{l.pagePrevious.x + l.pagePrevious.w + 8*u, l.pagePrevious.y, l.pageNext.x - l.pagePrevious.x - l.pagePrevious.w - 16*u, l.pagePrevious.h}
		w.navigationVisualContentLabel(middle, 11*u, fmt.Sprintf("%d / %d", l.page+1, l.pages), navigationVisualMuted)
	}
	w.drawNavigationToolPanel()
}

func (w *Workspace) navigationVisualButton(b box, id, label string, active, textOnly bool) {
	if b.w < 1 || b.h < 1 {
		return
	}
	n, u := w.navigation, max(.5, w.navigation.layout.unit)
	fill, edge, ink := navigationVisualPanel, navigationVisualEdge, navigationVisualMuted
	if n.hover == id || n.pressed == id || n.keyboard == id {
		fill, ink = navigationVisualRaised, navigationVisualText
	}
	if active {
		fill, edge, ink = navigationVisualRaised, navigationVisualMint, navigationVisualText
	}
	w.navigationVisualRect(b, fill, .97)
	w.navigationVisualOutline(b, edge, .65)
	if n.keyboard == id {
		w.navigationVisualOutline(navigationVisualInset(b, 2*u, 2*u), navigationVisualMint, 1)
	}
	if active {
		w.navigationVisualRect(box{b.x + 8*u, b.y + b.h - 2*u, max(0, b.w-16*u), u}, navigationVisualMint, .9)
	}
	left := b.x + 10*u
	// Compact controls retain their whole label. The icon is supplementary;
	// Back and page arrows use their complete hit area as icon-only controls.
	showIcon := !textOnly && (b.w < 64*u || label == "" || b.w >= 80*u && b.w >= (float32(len(label))*7+42)*u)
	if showIcon {
		icon := box{b.x + 10*u, b.y + (b.h-16*u)/2, 16 * u, 16 * u}
		if label == "" || b.w < 64*u {
			icon.x = b.x + (b.w-icon.w)/2
		}
		w.navigationVisualIcon(icon, id, ink)
		left += 23 * u
		if b.w < 64*u {
			return
		}
	}
	w.navigationVisualContentLabel(box{left, b.y, b.x + b.w - left - 8*u, b.h}, 12*u, label, ink)
}

func (w *Workspace) navigationVisualToolBounds() box {
	if w.navigation == nil || w.navigation.toolProgress <= 0 {
		return box{}
	}
	b := w.navigation.layout.toolPanel
	b.h *= max(0, min(1, w.navigation.toolProgress))
	return b
}

func (w *Workspace) navigationVisualContentLabel(b box, size float32, text string, rgb uint32) {
	cover := w.navigationVisualToolBounds()
	if navigationVisualIntersects(b, cover) {
		if cover.x > b.x+size {
			b.w = min(b.w, cover.x-b.x-6)
		} else {
			return
		}
	}
	w.navigationVisualLabel(b, size, text, rgb)
}

func (w *Workspace) drawNavigationToolPanel() {
	n, l := w.navigation, w.navigation.layout
	b := w.navigationVisualToolBounds()
	if b.w <= 0 || b.h <= 0 {
		return
	}
	u := max(.5, l.unit)
	w.navigationVisualRect(box{b.x + 5*u, b.y + 7*u, b.w, b.h}, 0x03090e, .65)
	w.navigationVisualRect(b, navigationVisualPanel, 1)
	w.navigationVisualOutline(b, navigationVisualEdge, 1)
	anchor := max(b.x+12*u, min(b.x+b.w-12*u, l.tools.x+l.tools.w/2))
	w.canvas.Line(anchor, l.tools.y+l.tools.h, anchor, b.y, 1, scene.ColorHex(navigationVisualMint, .5))
	if b.h >= 38*u {
		w.navigationVisualLabel(box{b.x + 15*u, b.y + 7*u, b.w - 30*u, 26 * u}, 14*u, "Tools", navigationVisualText)
		w.canvas.Line(b.x+12*u, b.y+37*u, b.x+b.w-12*u, b.y+37*u, 1, scene.ColorHex(navigationVisualEdge, .75))
	}
	for i, tool := range l.toolItems {
		r := tool.bounds
		// Reveal only complete rows; shaped labels stay crisp without spawning
		// alpha variants or leaking through the panel's unfolding edge.
		if r.y+r.h > b.y+b.h-5*u || r.y < b.y {
			continue
		}
		ink := navigationVisualText
		id := "tool:" + tool.id
		selected := n.selectedTool == i || n.hover == id || n.keyboard == id
		if tool.disabled {
			ink = 0x617984
		} else if selected {
			w.navigationVisualRect(r, navigationVisualRaised, 1)
			w.navigationVisualRect(box{r.x, r.y + 7*u, 2 * u, max(0, r.h-14*u)}, navigationVisualMint, 1)
		}
		if n.keyboard == id && !tool.disabled {
			w.navigationVisualOutline(navigationVisualInset(r, 2*u, 2*u), navigationVisualMint, 1)
		}
		w.navigationVisualLabel(box{r.x + 12*u, r.y, r.w - 24*u, r.h}, 13*u, tool.label, ink)
	}
}

func (w *Workspace) navigationVisualRect(b box, rgb uint32, alpha float32) {
	if b.w > 0 && b.h > 0 {
		w.canvas.Rect(b.x, b.y, b.w, b.h, scene.ColorHex(rgb, alpha))
	}
}

func (w *Workspace) navigationVisualOutline(b box, rgb uint32, alpha float32) {
	if b.w <= 1 || b.h <= 1 {
		return
	}
	c := scene.ColorHex(rgb, alpha)
	w.canvas.Line(b.x+.5, b.y+.5, b.x+b.w-.5, b.y+.5, 1, c)
	w.canvas.Line(b.x+b.w-.5, b.y+.5, b.x+b.w-.5, b.y+b.h-.5, 1, c)
	w.canvas.Line(b.x+b.w-.5, b.y+b.h-.5, b.x+.5, b.y+b.h-.5, 1, c)
	w.canvas.Line(b.x+.5, b.y+b.h-.5, b.x+.5, b.y+.5, 1, c)
}

func (w *Workspace) navigationVisualLabel(b box, size float32, text string, rgb uint32) {
	if b.w < 12 || b.h < size || text == "" {
		return
	}
	// Bound shaped-text cache variants while an application changes size. The
	// label's raster stays sharp; only its placement follows the animated frame.
	u := max(.5, w.navigation.layout.unit)
	quantum := 16 * u
	if b.w < 144*u {
		quantum = 4 * u
	}
	width := min(640*u, max(1, float32(math.Floor(float64(b.w/quantum)))*quantum))
	w.shapedText(b.x, b.y+(b.h-size*1.32)/2, size, width, text, scene.ColorHex(rgb, 1))
}

func navigationVisualInset(b box, x, y float32) box {
	return box{b.x + x, b.y + y, max(0, b.w-2*x), max(0, b.h-2*y)}
}

func navigationVisualIntersects(a, b box) bool {
	return a.w > 0 && a.h > 0 && b.w > 0 && b.h > 0 && a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h
}

func (w *Workspace) navigationVisualIcon(b box, name string, rgb uint32) {
	c := scene.ColorHex(rgb, 1)
	x, y, size := b.x+b.w/2, b.y+b.h/2, min(b.w, b.h)
	half, stroke := size*.36, max(1, size*.075)
	line := func(ax, ay, bx, by float32) {
		w.canvas.Line(x+ax*half, y+ay*half, x+bx*half, y+by*half, stroke, c)
	}
	switch name {
	case "back", "previous", "pagePrevious":
		line(.35, -.75, -.5, 0)
		line(-.5, 0, .35, .75)
		if name == "back" {
			line(-.5, 0, .9, 0)
		}
	case "next", "pageNext":
		line(-.35, -.75, .5, 0)
		line(.5, 0, -.35, .75)
	case "home":
		line(-1, -.05, 0, -.95)
		line(0, -.95, 1, -.05)
		line(-.72, -.05, -.72, .85)
		line(-.72, .85, .72, .85)
		line(.72, .85, .72, -.05)
	case "overview":
		for i := range 4 {
			px, py := x-half+float32(i%2)*half*1.12, y-half+float32(i/2)*half*1.12
			w.navigationVisualOutline(box{px, py, half * .82, half * .82}, rgb, 1)
		}
	case "search":
		w.canvas.Circle(x-half*.2, y-half*.2, half*.7, stroke, c)
		line(.36, .36, 1, 1)
	case "tools":
		for i := range 3 {
			v := float32(i-1) * .8
			line(-1, v, 1, v)
			shift := float32(-.4)
			if i == 1 {
				shift = .5
			}
			w.canvas.Rect(x+shift*half-stroke, y+v*half-stroke*1.8, stroke*2, stroke*3.6, c)
		}
	case "motion":
		w.canvas.Arc(x, y, half*.8, -.7, 4.6, stroke, c)
		line(.52, -.95, .2, -.8)
		line(.2, -.8, .53, -.6)
	}
}
