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
	{kind: "files", label: "Files", hint: "Browse projects and documents"},
	{kind: "terminal", label: "Terminal", hint: "Start a native shell"},
	{kind: "photo", label: "Photo", hint: "Choose an image to view"},
	{kind: "media", label: "Media", hint: "Choose a video to play"},
	{kind: "model", label: "Model", hint: "Choose a 3D model"},
	{kind: "research", label: "Research", hint: "Choose a data set"},
	{kind: "note", label: "Notes", hint: "Start a native note"},
	{kind: "axial", label: "AXIAL", hint: "Open the native 3D engineering study"},
}

var applicationDockBounds = box{1348, 122, 76, 620}

const (
	applicationDockTop    = float32(155)
	applicationDockPitch  = float32(72)
	applicationDockButton = float32(54)
)

func applicationDockButtonBounds(index int) box {
	return box{1359, applicationDockTop + float32(index)*applicationDockPitch, applicationDockButton, applicationDockButton}
}

func applicationDockIndexAt(x, y float32) int {
	for i := range applicationDockEntries {
		if applicationDockButtonBounds(i).contains(x, y) {
			return i
		}
	}
	return -1
}

func (w *Workspace) applicationDockVisible() bool {
	if !w.desktop || w.applications == nil {
		return false
	}
	_, ok := w.applications.(experience.ApplicationLauncher)
	return ok
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
	catalog, ok := w.applications.(experience.ApplicationLaunchCatalog)
	if !ok {
		// The terminal launcher predates the catalog contract. Preserve that
		// one compatibility path while keeping file intents visibly disabled.
		available[1] = true
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

	// The rail remains crisp in Adaptive focused work. Cinematic presentation
	// adds a second chassis edge and small circuit ticks without moving content.
	flare := w.presentationBlend
	w.rect(applicationDockBounds.x, applicationDockBounds.y, applicationDockBounds.w, applicationDockBounds.h, bg, .78)
	w.line(1350, 143, 1350, 725, 1, muted, .24)
	w.line(1422, 143, 1422, 725, 1, teal, .22+.22*flare)
	w.line(1360, 143, 1380, 143, 2, teal, .45+.35*flare)
	w.text(1387, 134, 10, "APPS", muted, .9)
	if flare > .05 {
		w.line(1343, 171, 1350, 164, 1, teal, .35*flare)
		w.line(1343, 171, 1343, 209, 1, teal, .28*flare)
		w.line(1394, 725, 1422, 725, 2, teal, .55*flare)
		for i := range applicationDockEntries {
			y := float32(158 + i*72)
			w.line(1417, y, 1422, y, 1, teal, .22*flare)
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
	// Modal portal input and an already-started footer/atlas click retain
	// ownership over the fixed launcher rail.
	if w.portals.open || w.portals.pressed != -1 {
		w.applicationDockHover = -1
		return false
	}

	if w.pointer.kind == captureApplicationDock {
		switch event.Kind {
		case experience.PointerMove:
			w.movePointer(event.X, event.Y)
			w.applicationDockHover = applicationDockIndexAt(w.pointer.lastX, w.pointer.lastY)
			return true
		case experience.PointerUp:
			if applicationButton(event) != 272 {
				return true
			}
			w.movePointer(event.X, event.Y)
			p := w.pointer
			w.pointer = pointerCapture{}
			index := applicationDockIndexAt(p.lastX, p.lastY)
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
	inside := applicationDockBounds.contains(x, y)
	index := applicationDockIndexAt(x, y)
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
	if index < 0 || index >= len(applicationDockEntries) {
		return
	}
	entry := applicationDockEntries[index]
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
