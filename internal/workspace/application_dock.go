package workspace

import (
	"fmt"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
)

type applicationDockEntry struct {
	kind, label, hint string
}

var applicationDockEntries = [...]applicationDockEntry{
	{kind: "launcher", label: "Launcher", hint: "Find tools, windows and spaces"},
	{kind: "files", label: "Files", hint: "Browse projects and documents"},
	{kind: "terminal", label: "Terminal", hint: "Start a native shell"},
	{kind: "photo", label: "Photo", hint: "Choose an image to view"},
	{kind: "media", label: "Media", hint: "Choose a video to play"},
	{kind: "model", label: "Model", hint: "Choose a 3D model"},
	{kind: "research", label: "Research", hint: "Choose a data set"},
	{kind: "note", label: "Notes", hint: "Start a native note"},
	{kind: "axial", label: "AXIAL", hint: "Open the native 3D engineering study"},
}

var applicationDockBounds = box{1364, 112, 60, 506}

const (
	applicationDockTop    = float32(118)
	applicationDockPitch  = float32(56)
	applicationDockButton = float32(46)
)

func applicationDockButtonBounds(index int) box {
	return box{
		applicationDockBounds.x + (applicationDockBounds.w-applicationDockButton)/2,
		applicationDockTop + float32(index)*applicationDockPitch,
		applicationDockButton,
		applicationDockButton,
	}
}

func applicationDockIndexAt(x, y float32) int {
	for i := range applicationDockEntries {
		if applicationDockButtonBounds(i).contains(x, y) {
			return i
		}
	}
	return -1
}

func (w *Workspace) applicationDockIndexAt(x, y float32) int {
	if w.skinDesktopChromeVisible() {
		return w.skinDesktopTargetAt(x, y)
	}
	return applicationDockIndexAt(x, y)
}

func (w *Workspace) applicationDockContains(x, y float32) bool {
	if w.skinDesktopChromeVisible() {
		return w.skinDesktopContains(x, y)
	}
	return applicationDockBounds.contains(x, y)
}

func (w *Workspace) applicationDockVisible() bool {
	return w.desktop
}

func (w *Workspace) applicationDockAvailable(kind string) bool {
	available := w.applicationDockAvailability()
	for i, entry := range applicationDockEntries {
		if entry.kind == kind {
			return available[i]
		}
	}
	return false
}

func (w *Workspace) applicationDockAvailability() [len(applicationDockEntries)]bool {
	var available [len(applicationDockEntries)]bool
	if !w.applicationDockVisible() {
		return available
	}
	available[0] = true
	if w.applications == nil {
		return available
	}
	if _, ok := w.applications.(experience.ApplicationLauncher); !ok {
		return available
	}
	catalog, ok := w.applications.(experience.ApplicationLaunchCatalog)
	if !ok {
		// The terminal launcher predates the catalog contract. Preserve that
		// one compatibility path while keeping file intents visibly disabled.
		for i, entry := range applicationDockEntries {
			if entry.kind == "terminal" {
				available[i] = true
				break
			}
		}
		return available
	}
	for _, choice := range catalog.ApplicationLaunches() {
		for i, entry := range applicationDockEntries {
			if choice.Kind == entry.kind {
				available[i] = true
				break
			}
		}
	}
	return available
}

func (w *Workspace) activeApplicationDockKind() string {
	appID := w.application.AppID
	switch {
	case appID == "worldr.project-browser":
		return "files"
	case appID == "worldr.native-terminal":
		return "terminal"
	case appID == "worldr.photo-viewer":
		return "photo"
	case appID == "worldr.media-player":
		return "media"
	case appID == "worldr.model-inspector":
		return "model"
	case appID == "worldr.research-workbench":
		return "research"
	case appID == "worldr.native-note":
		return "note"
	case appID == "worldr.axial":
		return "axial"
	}
	return ""
}

func (w *Workspace) drawApplicationDock() {
	if !w.applicationDockVisible() {
		return
	}

	// A second chassis edge and small circuit ticks frame the fixed app rail
	// without moving content or changing its hit targets.
	flare := float32(1)
	b := applicationDockBounds
	left, right := b.x+2, b.x+b.w-2
	top, bottom := b.y+7, b.y+b.h-2
	w.rect(b.x, b.y, b.w, b.h, bg, .78)
	w.line(left, top, left, bottom, 1, muted, .24)
	w.line(right, top, right, bottom, 1, teal, .22+.22*flare)
	w.line(b.x+8, top, b.x+26, top, 2, teal, .45+.35*flare)
	w.text(b.x+8, b.y-11, 10, "LAUNCHER", muted, .9)
	if flare > .05 {
		w.line(b.x-5, b.y+35, left, b.y+28, 1, teal, .35*flare)
		w.line(b.x-5, b.y+35, b.x-5, b.y+67, 1, teal, .28*flare)
		w.line(b.x+b.w-30, bottom, right, bottom, 2, teal, .55*flare)
		for i := range applicationDockEntries {
			y := applicationDockButtonBounds(i).y + 3
			w.line(right-5, y, right, y, 1, teal, .22*flare)
		}
	}

	active := w.activeApplicationDockKind()
	availableEntries := w.applicationDockAvailability()
	for i, entry := range applicationDockEntries {
		bounds := applicationDockButtonBounds(i)
		available := availableEntries[i]
		hovered := w.applicationDockHover == i
		selected := active == entry.kind
		alpha := float32(.42)
		if available {
			alpha = .82
		}
		if selected {
			w.rect(bounds.x, bounds.y, bounds.w, bounds.h, teal, .12+.06*flare)
			w.rect(bounds.x, bounds.y+8, 2, bounds.h-16, teal, .9)
		}
		if hovered {
			w.rect(bounds.x, bounds.y, bounds.w, bounds.h, teal, .12)
			w.line(bounds.x+6, bounds.y, bounds.x+bounds.w-6, bounds.y, 1, teal, .9)
			w.line(bounds.x+6, bounds.y+bounds.h, bounds.x+bounds.w-6, bounds.y+bounds.h, 1, teal, .5)
			alpha = 1
		}
		w.drawApplicationDockIcon(i, bounds.x+9, bounds.y+9, alpha)
	}

	if i := w.applicationDockHover; i >= 0 && i < len(applicationDockEntries) {
		entry := applicationDockEntries[i]
		bounds := applicationDockButtonBounds(i)
		tip := box{1132, bounds.y + 3, 216, 48}
		w.rect(tip.x, tip.y, tip.w, tip.h, bg, .94)
		w.line(tip.x, tip.y, tip.x+tip.w, tip.y, 1, teal, .58)
		w.line(tip.x+tip.w, tip.y+10, tip.x+tip.w, tip.y+tip.h-10, 1, teal, .58)
		color := teal
		if !availableEntries[i] {
			color = muted
		}
		w.text(tip.x+12, tip.y+7, 12, strings.ToUpper(entry.label), color, 1)
		hint := entry.hint
		if !availableEntries[i] {
			hint = "Launcher unavailable"
		}
		w.text(tip.x+12, tip.y+26, 10, hint, muted, .92)
	}
}

// Icons use only canvas primitives so the dock has no theme, asset, or file
// dependency. Coordinates are local to a 36x36 drawing cell.
func (w *Workspace) drawApplicationDockIcon(index int, x, y, alpha float32) {
	line := func(x1, y1, x2, y2, width float32) { w.line(x+x1, y+y1, x+x2, y+y2, width, teal, alpha) }
	rect := func(xx, yy, ww, hh float32) {
		line(xx, yy, xx+ww, yy, 1.35)
		line(xx+ww, yy, xx+ww, yy+hh, 1.35)
		line(xx+ww, yy+hh, xx, yy+hh, 1.35)
		line(xx, yy+hh, xx, yy, 1.35)
	}
	circle := func(xx, yy, radius float32) { w.circle(x+xx, y+yy, radius, 1.25, teal, alpha) }

	switch applicationDockEntries[index].kind {
	case "launcher":
		line(4, 4, 15, 4, 1.5)
		line(4, 4, 4, 15, 1.5)
		line(32, 4, 21, 4, 1.5)
		line(32, 4, 32, 15, 1.5)
		line(4, 32, 15, 32, 1.5)
		line(4, 32, 4, 21, 1.5)
		line(32, 32, 21, 32, 1.5)
		line(32, 32, 32, 21, 1.5)
		circle(18, 18, 5)
		line(18, 9, 18, 13, 1.2)
		line(18, 23, 18, 27, 1.2)
		line(9, 18, 13, 18, 1.2)
		line(23, 18, 27, 18, 1.2)
	case "files":
		line(3, 10, 14, 10, 1.5)
		line(14, 10, 18, 14, 1.5)
		line(18, 14, 33, 14, 1.5)
		line(3, 10, 3, 31, 1.5)
		line(3, 31, 33, 31, 1.5)
		line(33, 31, 33, 14, 1.5)
		line(7, 19, 29, 19, 1)
	case "terminal":
		rect(3, 5, 30, 27)
		line(9, 13, 14, 18, 1.7)
		line(14, 18, 9, 23, 1.7)
		line(18, 24, 27, 24, 1.7)
	case "photo":
		rect(4, 5, 28, 27)
		circle(25, 12, 3)
		line(7, 27, 15, 18, 1.4)
		line(15, 18, 20, 23, 1.4)
		line(20, 23, 24, 19, 1.4)
		line(24, 19, 30, 27, 1.4)
	case "media":
		circle(18, 18, 15)
		line(14, 11, 14, 25, 1.8)
		line(14, 11, 25, 18, 1.8)
		line(25, 18, 14, 25, 1.8)
	case "model":
		line(18, 3, 32, 11, 1.3)
		line(32, 11, 32, 26, 1.3)
		line(32, 26, 18, 34, 1.3)
		line(18, 34, 4, 26, 1.3)
		line(4, 26, 4, 11, 1.3)
		line(4, 11, 18, 3, 1.3)
		line(4, 11, 18, 19, 1.3)
		line(32, 11, 18, 19, 1.3)
		line(18, 19, 18, 34, 1.3)
	case "research":
		line(4, 31, 4, 5, 1.2)
		line(4, 31, 33, 31, 1.2)
		line(8, 26, 14, 19, 1.5)
		line(14, 19, 20, 23, 1.5)
		line(20, 23, 29, 10, 1.5)
		circle(8, 26, 1.8)
		circle(14, 19, 1.8)
		circle(20, 23, 1.8)
		circle(29, 10, 1.8)
	case "note":
		line(6, 3, 24, 3, 1.3)
		line(24, 3, 31, 10, 1.3)
		line(31, 10, 31, 33, 1.3)
		line(31, 33, 6, 33, 1.3)
		line(6, 33, 6, 3, 1.3)
		line(24, 3, 24, 10, 1.2)
		line(24, 10, 31, 10, 1.2)
		line(11, 16, 26, 16, 1.2)
		line(11, 22, 26, 22, 1.2)
		line(11, 28, 22, 28, 1.2)
	case "axial":
		circle(18, 18, 15)
		circle(18, 18, 6)
		for _, blade := range [][4]float32{{18, 3, 20, 12}, {33, 18, 24, 20}, {18, 33, 16, 24}, {3, 18, 12, 16}} {
			line(blade[0], blade[1], blade[2], blade[3], 1.7)
		}
	}
}

func (w *Workspace) handleApplicationDock(event experience.Event) bool {
	if !w.applicationDockVisible() {
		w.applicationDockHover = -1
		return false
	}
	if w.pointer.kind == captureApplicationDock {
		if w.pointer.scale != w.scale || w.pointer.ox != w.ox || w.pointer.oy != w.oy {
			w.pointer.dragged = true
		}
		switch event.Kind {
		case experience.PointerMove:
			w.movePointer(event.X, event.Y)
			w.applicationDockHover = w.applicationDockIndexAt(w.pointer.lastX, w.pointer.lastY)
			return true
		case experience.PointerUp:
			if applicationButton(event) != 272 {
				return true
			}
			w.movePointer(event.X, event.Y)
			p := w.pointer
			w.pointer = pointerCapture{}
			index := w.applicationDockIndexAt(p.lastX, p.lastY)
			w.applicationDockHover = index
			if !p.dragged && index == p.dockIndex {
				w.launchApplicationDockEntry(index)
			}
			return true
		case experience.PointerDown:
			return true
		case experience.PointerCancel, experience.KeyboardCancel:
			w.pointer = pointerCapture{}
			w.applicationDockHover = -1
			return true
		}
	}

	// A client button grab or an in-progress spatial gesture owns its release.
	if w.pointer.kind != captureNone || len(w.applicationButtons) != 0 || len(w.windowDragButtons) != 0 {
		return false
	}
	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	inside := w.applicationDockContains(x, y)
	index := w.applicationDockIndexAt(x, y)
	switch event.Kind {
	case experience.PointerMove:
		w.applicationDockHover = index
		if inside {
			w.clearApplicationHover()
		}
		return inside
	case experience.PointerScroll:
		return inside
	case experience.PointerDown:
		if !inside {
			w.applicationDockHover = -1
			return false
		}
		w.resetApplicationReadClick()
		w.clearApplicationFocus()
		if applicationButton(event) == 272 && index >= 0 {
			w.pointer = pointerCapture{
				kind: captureApplicationDock, start: w.Document(), dockIndex: index,
				pressX: x, pressY: y, lastX: x, lastY: y, scale: w.scale, ox: w.ox, oy: w.oy,
			}
		}
		return true
	case experience.PointerUp:
		return inside
	case experience.PointerCancel:
		w.applicationDockHover = -1
	}
	return false
}

func (w *Workspace) launchApplicationDockEntry(index int) {
	if index >= skinDesktopDockIndex {
		w.launchSkinDesktopEntry(index)
		return
	}
	if index < 0 || index >= len(applicationDockEntries) {
		return
	}
	entry := applicationDockEntries[index]
	if entry.kind == "launcher" {
		w.openCommands()
		return
	}
	if !w.applicationDockAvailable(entry.kind) {
		w.showApplicationNotice(fmt.Sprintf("%s launcher is unavailable.", entry.label))
		return
	}
	launcher := w.applications.(experience.ApplicationLauncher)
	if entry.kind == "terminal" {
		w.launchTerminal(launcher)
		return
	}

	w.clearApplicationFocus()
	live := make(map[string]bool, len(w.applicationSurfaces))
	for _, surface := range w.applicationSurfaces {
		live[surface.Key] = true
	}
	existingIDs := make(map[uint64]bool)
	for _, surface := range w.applications.Surfaces() {
		existingIDs[surface.ID] = true
	}
	before := w.Document()
	key, err := launcher.LaunchApplication(entry.kind)
	if err != nil {
		w.showApplicationNotice(fmt.Sprintf("%s could not open: %v", entry.label, err))
		return
	}
	if !live[key] {
		w.PlaceNewApplication(key)
	}
	if err := w.activateApplicationFromDock(key); err != nil {
		w.install(before, false)
		if closer, ok := w.applications.(experience.ApplicationCloser); ok {
			for _, surface := range w.applications.Surfaces() {
				if surface.ID != 0 && surface.Key == key && !existingIDs[surface.ID] {
					closer.CloseApplication(surface.ID)
					break
				}
			}
		}
		w.showApplicationNotice(fmt.Sprintf("%s could not open: %v", entry.label, err))
		return
	}
	w.applicationNotice, w.applicationNoticeRemaining = "", 0
}

// Dock activation follows a direct client click: selecting the target stops
// only that target's own throw, while other windows keep coasting. It grants
// input immediately without changing the saved Read/Space presentation mode.
func (w *Workspace) activateApplicationFromDock(key string) error {
	w.syncApplications()
	live := false
	for _, surface := range w.applicationSurfaces {
		live = live || surface.Key == key
	}
	if !live {
		return fmt.Errorf("opened application is not available in the workspace")
	}
	if i := w.m.applicationState.index(key); i >= 0 && w.m.applicationState.Layouts[i].Space != w.m.applicationState.Space {
		if err := w.Dispatch(Action{Kind: SwitchSpace, Space: w.m.applicationState.Layouts[i].Space}); err != nil {
			return err
		}
	}
	if err := w.Dispatch(Action{Kind: SelectApplication, ApplicationKey: key}); err != nil {
		return err
	}
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	if w.application.ID != 0 && !w.application.DragContent {
		w.applications.Focus(w.application.ID)
		w.applicationFocusedID = w.application.ID
		w.applicationKeyboard = true
	}
	return nil
}
