package workspace

import (
	"strconv"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
)

func (w *Workspace) navigationProject(space uint8) {
	n := w.navigation
	if int(space) >= len(n.projectPages) {
		return
	}
	if n.level == navigationHome {
		n.homePage = n.page
	}
	if n.level == navigationProject {
		n.projectPages[w.m.applicationState.Space] = n.page
	}
	n.page = n.projectPages[space]
	n.projectPage = n.page
	n.home = false
	n.toolsOpen = false
	n.keyboard = ""
	w.navigationRetireClientKeys()
	w.clearApplicationFocus()
	if err := w.Dispatch(Action{Kind: SwitchSpace, Space: space}); err != nil {
		w.Notify(err.Error())
		return
	}
	d := w.Document()
	d.View.Application.Overview = true
	d.View.Application.Reading = false
	d.View.Application.Placing = false
	w.install(d, false)
	w.layout(w.width, w.height)
	if slot := w.m.applicationState.index(n.returnFocus); slot >= 0 && w.m.applicationState.Layouts[slot].Space == space {
		n.keyboard = navigationID("app", slot)
	}
}
func (w *Workspace) navigationHome() {
	n := w.navigation
	if n.level == navigationProject {
		n.projectPage = n.page
		n.projectPages[w.m.applicationState.Space] = n.page
	}
	n.home = true
	n.toolsOpen = false
	n.page = n.homePage
	n.keyboard = ""
	w.navigationRetireClientKeys()
	w.clearApplicationFocus()
	w.layout(w.width, w.height)
}
func (w *Workspace) navigationBack() {
	n := w.navigation
	if n.toolsOpen {
		n.toolsOpen = false
		w.navigationRestoreFocus()
		return
	}
	if n.level == navigationApp {
		n.returnFocus = w.application.Key
		w.navigationProject(w.m.applicationState.Space)
	} else if n.level == navigationProject {
		w.navigationHome()
	}
}
func (w *Workspace) navigationOverview() {
	n := w.navigation
	if n.level == navigationProject {
		key := n.returnFocus
		if key == "" {
			key = w.m.applicationState.Active
		}
		if i := w.m.applicationState.index(key); i >= 0 && w.m.applicationState.Layouts[i].Space == w.m.applicationState.Space {
			if w.ActivateApplication(key) == nil {
				return
			}
		}
	}
	if n.level == navigationApp {
		n.returnFocus = w.application.Key
	}
	w.navigationProject(w.m.applicationState.Space)
}
func (w *Workspace) navigationRestoreFocus() {
	if w.navigationLevel() != navigationApp || w.applications == nil || w.application.DragContent {
		return
	}
	w.applications.Focus(w.application.ID)
	w.applicationFocusedID = w.application.ID
	w.applicationKeyboard = true
}
func (w *Workspace) navigationSwitch(delta int) {
	var keys []string
	for _, p := range w.m.applicationState.Layouts {
		if p.Key == "" || w.navigation.level != navigationApp && p.Space != w.m.applicationState.Space {
			continue
		}
		for _, s := range w.applicationSurfaces {
			if s.Key == p.Key {
				keys = append(keys, p.Key)
				break
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	index := 0
	for i, key := range keys {
		if key == w.m.applicationState.Active {
			index = i
			break
		}
	}
	index = (index + delta + len(keys)) % len(keys)
	_ = w.ActivateApplication(keys[index])
}

// Navigation owns a physical stroke through release. Tracking ordinary presses
// as false prevents a held client key becoming a command when modifiers change.
// Drain happens before modals so opening Search cannot strand a held shortcut.
func (w *Workspace) navigationDrain(e experience.Event) bool {
	n := w.navigation
	if e.Kind == experience.KeyboardCancel {
		n.held = make(map[helpKey]bool)
		n.pressed = ""
		n.pressButton = 0
		return false
	}
	if e.Kind != experience.KeyInput {
		return false
	}
	key := helpStroke(e)
	if owned, seen := n.held[key]; seen {
		if !e.Pressed {
			delete(n.held, key)
		}
		return owned
	}
	return false
}
func (w *Workspace) navigationClaim(e experience.Event) {
	w.navigation.held[helpStroke(e)] = true
}

// A focus handoff cancels the former client. Keep its already pressed keys
// workspace-owned until release so their tails cannot reach another client.
func (w *Workspace) navigationRetireClientKeys() {
	for key := range w.navigation.held {
		w.navigation.held[key] = true
	}
}

func navigationShortcutAction(e experience.Event) string {
	if e.Kind != experience.KeyInput || e.Modifiers != experience.ModControl|experience.ModAlt {
		return ""
	}
	switch {
	case navigationKey(e, experience.KeyO, 24):
		return "overview"
	case navigationKey(e, experience.KeyH, 35):
		return "home"
	case navigationKey(e, experience.KeyJ, 36):
		return "previous"
	case navigationKey(e, experience.KeyK, 37):
		return "next"
	case navigationKey(e, experience.KeySpace, 57):
		return "search"
	case navigationKey(e, experience.KeyEnter, 28):
		return "tool:launch:terminal"
	}
	return ""
}

func (w *Workspace) navigationShortcut(e experience.Event) bool {
	if e.Kind != experience.KeyInput || !e.Pressed {
		return false
	}
	if _, seen := w.navigation.held[helpStroke(e)]; seen {
		return false
	}
	action := navigationShortcutAction(e)
	if action == "" {
		return false
	}
	w.navigationClaim(e)
	if !e.Repeat {
		w.navigationAction(action)
	}
	return true
}

func (w *Workspace) handleNavigation(e experience.Event) bool {
	n := w.navigation
	if e.Kind == experience.TextCommit || e.Kind == experience.TextPreedit {
		state := w.TextInput()
		if !state.Enabled || state.ContextID != e.TextContext {
			return true
		}
	}
	w.syncApplications()
	w.layout(w.width, w.height)
	if w.navigationDrain(e) {
		return true
	}
	if e.Kind == experience.KeyboardCancel {
		n.toolsOpen = false
		n.restoreAfterModal = false
	}
	// Reserved commands can interrupt motion and close their previous overlay.
	if w.navigationShortcut(e) {
		return true
	}
	wasOpen := w.commands != nil && w.commands.open
	if e.Pressed && navigationShortcutAction(e) != "" {
		// A remaining chord belongs to an already held ordinary client key.
		// Do not let the shared palette's legacy Space chord reclassify it.
		if !wasOpen && !n.toolsOpen && n.level == navigationApp {
			return w.handleApplication(e)
		}
		return true
	}
	oldSpace := w.m.applicationState.Space
	if w.handleCommands(e) {
		if wasOpen && !w.commands.open {
			if e.Kind == experience.PointerDown && applicationButton(e) != 0 {
				n.pressed, n.pressButton = "dismiss", applicationButton(e)
			}
			if w.m.applicationState.Space != oldSpace {
				w.navigationProject(w.m.applicationState.Space)
			}
			if n.restoreAfterModal {
				w.navigationRestoreFocus()
			}
			n.restoreAfterModal = false
		}
		return true
	}
	// Only the existing client router owns a drag once it begins. Crossing a
	// navigation button cannot steal its release or turn it into a menu click.
	pointer := e.Kind == experience.PointerMove || e.Kind == experience.PointerDown || e.Kind == experience.PointerUp || e.Kind == experience.PointerScroll || e.Kind == experience.PointerCancel
	if pointer && w.applicationCapturedID != 0 {
		return w.handleApplication(e)
	}
	if e.Kind == experience.PointerCancel {
		n.pressed = ""
		n.pressButton = 0
		n.hover = ""
		return w.handleApplication(e)
	}
	if e.Kind == experience.KeyboardCancel {
		return w.handleApplication(e)
	}
	if e.Kind == experience.KeyInput {
		if !e.Pressed {
			if n.toolsOpen || n.level != navigationApp {
				return true
			}
			return w.handleApplication(e)
		}
		if _, seen := n.held[helpStroke(e)]; !seen {
			n.held[helpStroke(e)] = false
		}
		if n.toolsOpen || n.level != navigationApp {
			w.navigationClaim(e)
			if e.Repeat {
				return true
			}
			switch {
			case navigationKey(e, experience.KeyEscape, 1):
				w.navigationBack()
			case navigationKey(e, experience.KeyTab, 15):
				delta := 1
				if e.Modifiers.Has(experience.ModShift) {
					delta = -1
				}
				w.navigationKeyboardMove(delta)
			case navigationKey(e, experience.KeyRight, 106), navigationKey(e, experience.KeyDown, 108):
				w.navigationKeyboardMove(1)
			case navigationKey(e, experience.KeyLeft, 105), navigationKey(e, experience.KeyUp, 103):
				w.navigationKeyboardMove(-1)
			case navigationKey(e, experience.KeyEnter, 28), navigationKey(e, experience.KeySpace, 57):
				id := n.keyboard
				if id == "" {
					targets := w.navigationKeyboardTargets()
					if len(targets) > 0 {
						id = targets[0]
					}
				}
				w.navigationAction(id)
			}
			return true
		}
	}
	if pointer {
		hit := w.navigationHit(e.X, e.Y)
		if e.Kind == experience.PointerMove {
			n.hover = hit
			if n.pressed != "" {
				return true
			}
		}
		if e.Kind == experience.PointerDown {
			if n.pressed != "" {
				return true
			}
			n.keyboard = ""
			if n.toolsOpen && hit != "tools" && hit != "menu" && !strings.HasPrefix(hit, "tool:") {
				n.toolsOpen = false
				if button := applicationButton(e); button != 0 {
					n.pressed, n.pressButton = "dismiss", button
				}
				w.navigationRestoreFocus()
				return true
			}
			if applicationButton(e) == 272 && hit != "" {
				n.pressed = hit
				n.pressButton = 272
				n.pressX, n.pressY = e.X, e.Y
				return true
			}
			if n.toolsOpen {
				return true
			}
		}
		if e.Kind == experience.PointerUp && n.pressed != "" {
			if applicationButton(e) != n.pressButton {
				return true
			}
			pressed := n.pressed
			n.pressed = ""
			n.pressButton = 0
			if hit == pressed && abs(e.X-n.pressX) < 8 && abs(e.Y-n.pressY) < 8 {
				w.navigationAction(hit)
			}
			return true
		}
		if n.toolsOpen {
			return true
		}
		if e.Kind == experience.PointerScroll && n.level != navigationApp {
			if e.ScrollY > 0 {
				w.navigationAction("pageNext")
			} else if e.ScrollY < 0 {
				w.navigationAction("pagePrevious")
			}
			return true
		}
		if hit != "" {
			w.clearApplicationHover()
			return true
		}
	}
	if n.level == navigationApp {
		return w.handleApplication(e)
	}
	return e.Kind != experience.KeymapChanged
}

func (w *Workspace) navigationHit(x, y float32) string {
	n := w.navigation
	l := n.layout
	if n.toolsOpen {
		bottom := l.toolPanel.y + l.toolPanel.h*n.toolProgress - 5*l.unit
		for _, t := range l.toolItems {
			if t.bounds.contains(x, y) {
				if !t.disabled && t.bounds.y+t.bounds.h <= bottom {
					return "tool:" + t.id
				}
				return "menu"
			}
		}
		if l.toolPanel.contains(x, y) {
			return "menu"
		}
	}
	for _, b := range []struct {
		id string
		b  box
	}{{"back", l.back}, {"home", l.home}, {"overview", l.overview}, {"search", l.search}, {"tools", l.tools}, {"motion", l.motion}, {"previous", l.previous}, {"next", l.next}, {"pagePrevious", l.pagePrevious}, {"pageNext", l.pageNext}} {
		if (b.id == "previous" || b.id == "next") && n.level == navigationHome {
			continue
		}
		if (b.id == "pagePrevious" || b.id == "pageNext") && l.pages <= 1 {
			continue
		}
		if b.b.contains(x, y) {
			return b.id
		}
	}
	for _, t := range l.tabs {
		if t.bounds.contains(x, y) {
			return navigationID("tab", int(t.space))
		}
	}
	// At Home any preview belongs to its containing project, not directly to an
	// app. This keeps the hierarchy predictable for both pointer and keyboard.
	if n.level == navigationHome {
		for _, p := range l.projects {
			if p.bounds.contains(x, y) {
				return navigationID("project", int(p.space))
			}
		}
		return ""
	}
	poses := n.motion.poses()
	if n.level == navigationApp {
		for _, app := range l.apps {
			if app.active && poses[app.slot].visible && poses[app.slot].bounds.contains(x, y) {
				// The active client is drawn in front of moving companion cards.
				return ""
			}
		}
	}
	for _, a := range l.apps {
		if a.bounds.w == 0 || !poses[a.slot].visible {
			continue
		}
		b := poses[a.slot].bounds
		title := box{b.x, b.y - 28*l.unit, b.w, 28 * l.unit}
		if title.contains(x, y) || (n.level != navigationApp || !a.active) && b.contains(x, y) {
			return navigationID("app", a.slot)
		}
	}
	return ""
}

func (w *Workspace) navigationKeyboardTargets() []string {
	n := w.navigation
	l := n.layout
	var ids []string
	if n.toolsOpen {
		for _, t := range l.toolItems {
			if !t.disabled {
				ids = append(ids, "tool:"+t.id)
			}
		}
		return ids
	}
	if n.level == navigationHome {
		for _, p := range l.projects {
			ids = append(ids, navigationID("project", int(p.space)))
		}
	} else {
		for _, a := range l.apps {
			if a.bounds.w > 0 {
				ids = append(ids, navigationID("app", a.slot))
			}
		}
	}
	ids = append(ids, "back", "home", "overview", "search", "tools", "motion")
	if l.pages > 1 {
		ids = append(ids, "pagePrevious", "pageNext")
	}
	return ids
}
func (w *Workspace) navigationKeyboardMove(delta int) {
	ids := w.navigationKeyboardTargets()
	if len(ids) == 0 {
		return
	}
	current := -1
	for i, id := range ids {
		if id == w.navigation.keyboard {
			current = i
			break
		}
	}
	if current < 0 {
		current = 0
		if delta < 0 {
			current = len(ids) - 1
		}
	} else {
		current = (current + delta + len(ids)) % len(ids)
	}
	w.navigation.keyboard = ids[current]
}

func (w *Workspace) navigationAction(id string) {
	n := w.navigation
	if id != "search" && w.commands != nil && w.commands.open {
		w.commands.open = false
		n.restoreAfterModal = false
	}
	switch id {
	case "back":
		w.navigationBack()
	case "home":
		w.navigationHome()
	case "overview":
		w.navigationOverview()
	case "previous":
		w.navigationSwitch(-1)
	case "next":
		w.navigationSwitch(1)
	case "search":
		n.toolsOpen = false
		if w.commands != nil && w.commands.open {
			w.commands.open = false
			w.navigationRestoreFocus()
			n.restoreAfterModal = false
		} else {
			w.navigationRetireClientKeys()
			n.restoreAfterModal = n.level == navigationApp
			w.openCommands()
		}
	case "tools":
		n.toolsOpen = !n.toolsOpen
		n.keyboard = ""
		if n.toolsOpen {
			w.navigationRetireClientKeys()
			w.clearApplicationFocus()
		} else {
			w.navigationRestoreFocus()
		}
	case "motion":
		n.motionEnabled = !n.motionEnabled
	case "pageNext":
		n.page = min(n.layout.pages-1, n.page+1)
		n.keyboard = ""
	case "pagePrevious":
		n.page = max(0, n.page-1)
		n.keyboard = ""
	default:
		prefix, value, ok := strings.Cut(id, ":")
		if !ok {
			return
		}
		index, _ := strconv.Atoi(value)
		switch prefix {
		case "project", "tab":
			w.navigationProject(uint8(index))
		case "app":
			if index >= 0 && index < MaxApplicationLayouts {
				key := w.m.applicationState.Layouts[index].Key
				if key != "" {
					_ = w.ActivateApplication(key)
				}
			}
		case "tool":
			n.toolsOpen = false
			n.keyboard = ""
			switch value {
			case "home":
				w.navigationHome()
			case "organize":
				n.restoreAfterModal = n.level == navigationApp
				w.openCommands()
			case "minimize":
				if w.application.ID != 0 {
					_ = w.Dispatch(Action{Kind: ToggleApplicationMinimized})
					w.navigationProject(w.m.applicationState.Space)
				}
			case "close":
				if closer, ok := w.applications.(experience.ApplicationCloser); ok && w.application.ID != 0 {
					w.requestApplicationClose(w.application, closer)
					w.navigationProject(w.m.applicationState.Space)
				}
			default:
				if kind, ok := strings.CutPrefix(value, "launch:"); ok {
					if launcher, ok := w.applications.(experience.ApplicationLauncher); ok {
						live := make(map[string]bool, len(w.applicationSurfaces))
						for _, surface := range w.applicationSurfaces {
							live[surface.Key] = true
						}
						key, err := launcher.LaunchApplication(kind)
						if err == nil {
							if !live[key] {
								w.PlaceNewApplication(key)
							}
							err = w.ActivateApplication(key)
						}
						if err != nil {
							w.Notify(err.Error())
							w.navigationRestoreFocus()
						}
					}
				}
			}
		}
	}
	w.layout(w.width, w.height)
}
