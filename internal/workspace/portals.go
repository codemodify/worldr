package workspace

import (
	"fmt"
	"math"

	"github.com/codemodify/worldr/internal/experience"
)

// SpatialPortal is a stable navigation target derived from a named space and
// one live window group. Ungrouped windows have individual portals. Empty named
// spaces retain a portal so they never become unreachable.
type SpatialPortal struct {
	ID           string
	Space        uint8
	SpaceName    string
	Group        uint8
	Key          string
	Title        string
	Applications int
	X, Y, Depth  float32
}

type portalGroup struct {
	space uint8
	group uint8
	key   string
}

func portalIdentity(space, group uint8, key string) string {
	if group != 0 {
		return fmt.Sprintf("space:%d/group:%d", space, group)
	}
	if key != "" {
		return fmt.Sprintf("space:%d/window:%s", space, key)
	}
	return fmt.Sprintf("space:%d", space)
}

func (w *Workspace) navigationPortals() []SpatialPortal {
	v := w.m.applicationState
	live := make(map[string]experience.ApplicationSurface, len(w.applicationSurfaces))
	for _, surface := range w.applicationSurfaces {
		live[surface.Key] = surface
	}
	portals := make([]SpatialPortal, 0, len(w.applicationSurfaces)+len(v.Spaces))
	for spaceIndex := range v.Spaces {
		space := uint8(spaceIndex)
		if !v.spaceExists(space) {
			continue
		}
		groups := make(map[portalGroup]int)
		spaceStart := len(portals)
		for _, placement := range v.Layouts {
			surface, ok := live[placement.Key]
			if !ok || placement.Space != space {
				continue
			}
			identity := portalGroup{space: space, group: placement.Group, key: placement.Key}
			if placement.Group != 0 {
				identity.key = ""
			}
			if index, ok := groups[identity]; ok {
				portal := &portals[index]
				count := float32(portal.Applications)
				portal.X = (portal.X*count + placement.X) / (count + 1)
				portal.Y = (portal.Y*count + placement.Y) / (count + 1)
				portal.Depth = (portal.Depth*count + placement.Depth) / (count + 1)
				portal.Applications++
				continue
			}
			title := shortApplicationTitle(surface.Title)
			if placement.Group != 0 {
				title = fmt.Sprintf("Group %02d / %s", placement.Group, title)
			}
			groups[identity] = len(portals)
			portals = append(portals, SpatialPortal{
				ID: portalIdentity(space, placement.Group, placement.Key), Space: space,
				SpaceName: v.spaceName(space), Group: placement.Group, Key: placement.Key,
				Title: title, Applications: 1, X: placement.X, Y: placement.Y, Depth: placement.Depth,
			})
		}
		if len(portals) == spaceStart {
			portals = append(portals, SpatialPortal{ID: portalIdentity(space, 0, ""), Space: space, SpaceName: v.spaceName(space), Title: "Empty space"})
		}
	}
	// Navigation uses the group's stable centroid. Include remembered members
	// so closing and reopening one cannot make the camera target drift.
	for i := range portals {
		portal := &portals[i]
		if portal.Group == 0 {
			continue
		}
		portal.X, portal.Y, portal.Depth = 0, 0, 0
		count := float32(0)
		for _, placement := range v.Layouts {
			if placement.Key == "" || placement.Space != portal.Space || placement.Group != portal.Group {
				continue
			}
			portal.X += placement.X
			portal.Y += placement.Y
			portal.Depth += placement.Depth
			count++
		}
		portal.X, portal.Y, portal.Depth = portal.X/count, portal.Y/count, portal.Depth/count
	}
	return portals
}

// SpatialPortals returns a detached snapshot for launchers, automation and
// future native navigation surfaces.
func (w *Workspace) SpatialPortals() []SpatialPortal {
	w.syncApplications()
	portals := w.navigationPortals()
	return append([]SpatialPortal(nil), portals...)
}

func reducePortalNavigation(d *Document, action Action) error {
	v := &d.View.Application
	targetSpace := action.Space
	index := -1
	if action.ApplicationKey != "" {
		index = v.index(action.ApplicationKey)
		if index < 0 {
			return fmt.Errorf("portal application does not exist")
		}
		targetSpace = v.Layouts[index].Space
	}
	if !v.spaceExists(targetSpace) {
		return fmt.Errorf("portal space does not exist")
	}
	if targetSpace != v.Space {
		if err := reduceSpace(d, Action{Kind: SwitchSpace, Space: targetSpace}); err != nil {
			return err
		}
		v = &d.View.Application
	}
	if index < 0 {
		v.Reading, v.Overview, v.Placing = false, false, false
		return nil
	}
	placement := v.Layouts[index]
	mask := uint32(1 << index)
	if placement.Group != 0 {
		for i, candidate := range v.Layouts {
			if candidate.Key != "" && candidate.Space == targetSpace && candidate.Group == placement.Group {
				mask |= 1 << i
			}
		}
	}
	// A portal is an explicit request to retrieve its destination. Restore any
	// collapsed members of that destination so the subsequent live-surface sync
	// cannot replace the requested active window with a different one.
	for i := range v.Layouts {
		if mask&(1<<i) != 0 {
			v.Layouts[i].Minimized = false
		}
	}
	v.Active, v.Selected = action.ApplicationKey, mask
	v.Reading, v.Overview, v.Placing = false, false, false
	v.aliases()
	centerX, centerY, centerDepth, count := float32(0), float32(0), float32(0), float32(0)
	for i, candidate := range v.Layouts {
		if mask&(1<<i) == 0 {
			continue
		}
		centerX += candidate.X
		centerY += candidate.Y
		centerDepth += candidate.Depth
		count++
	}
	centerX, centerY, centerDepth = centerX/count, centerY/count, centerDepth/count
	d.View.Camera.TargetX, d.View.Camera.TargetY, d.View.Camera.TargetDepth = centerX, centerY, centerDepth

	// Fit the whole group, with a bounded detail-biased zoom for a single app.
	spanX, spanY := float32(4.6), float32(3)
	for i, candidate := range v.Layouts {
		if mask&(1<<i) == 0 {
			continue
		}
		spanX = max(spanX, 2*abs(candidate.X-centerX)+4.6)
		spanY = max(spanY, 2*abs(candidate.Y-centerY)+3)
	}
	aspect := float32(1.8)
	requiredDistance := max(spanY, spanX/aspect) / (2 * float32(math.Tan(.69/2))) * 1.12
	zoom := -float32(math.Log(float64(requiredDistance / 9.8)))
	d.View.Camera.Zoom = max(float32(-.2), min(float32(.25), zoom))
	return nil
}

type portalNavigation struct {
	open      bool
	selected  int
	held      uint16
	pressed   int
	pressedID string
}

const (
	portalKeyG uint16 = 1 << iota
	portalKeyLeft
	portalKeyRight
	portalKeyUp
	portalKeyDown
	portalKeyEnter
	portalKeyKeypadEnter
	portalKeyEscape
)

func portalNavigationKey(event experience.Event) uint16 {
	switch event.Keycode {
	case 34:
		return portalKeyG
	case 105:
		return portalKeyLeft
	case 106:
		return portalKeyRight
	case 103:
		return portalKeyUp
	case 108:
		return portalKeyDown
	case 28:
		return portalKeyEnter
	case 96:
		return portalKeyKeypadEnter
	case 1:
		return portalKeyEscape
	case 0:
		switch event.Key {
		case experience.KeyG:
			return portalKeyG
		case experience.KeyLeft:
			return portalKeyLeft
		case experience.KeyRight:
			return portalKeyRight
		case experience.KeyEscape:
			return portalKeyEscape
		}
	}
	return 0
}

func (w *Workspace) currentPortal(portals []SpatialPortal) int {
	view := w.m.applicationState
	if index := view.index(view.Active); index >= 0 {
		placement := view.Layouts[index]
		id := portalIdentity(placement.Space, placement.Group, placement.Key)
		for i, portal := range portals {
			if portal.ID == id {
				return i
			}
		}
	}
	for i, portal := range portals {
		if portal.Space == view.Space {
			return i
		}
	}
	return 0
}

func (w *Workspace) openPortalAtlas() {
	w.cancelPointer()
	w.clearApplicationFocus()
	w.helpOpen = false
	if w.commands != nil {
		w.commands.open = false
	}
	portals := w.navigationPortals()
	w.portals.open = true
	w.portals.selected = w.currentPortal(portals)
	w.portals.pressed, w.portals.pressedID = -1, ""
}

func (w *Workspace) activatePortal(portal SpatialPortal) {
	w.cancelPointer()
	w.clearApplicationFocus()
	w.portals.open = false
	w.portals.pressed, w.portals.pressedID = -1, ""
	if err := w.Dispatch(Action{Kind: NavigatePortal, Space: portal.Space, ApplicationKey: portal.Key}); err != nil {
		w.Notify(err.Error())
	}
}

func (w *Workspace) cyclePortal(delta int) {
	portals := w.navigationPortals()
	if len(portals) == 0 {
		return
	}
	current := w.currentPortal(portals)
	next := (current + delta) % len(portals)
	if next < 0 {
		next += len(portals)
	}
	w.activatePortal(portals[next])
}

const portalsPerPage = 12

var portalButton = box{752, 871, 174, 24}

func portalCard(index int) box {
	local := index % portalsPerPage
	return box{282 + float32(local%4)*272, 224 + float32(local/4)*152, 250, 128}
}

func (w *Workspace) portalAt(x, y float32, portals []SpatialPortal) int {
	if len(portals) == 0 {
		return -1
	}
	page := w.portals.selected / portalsPerPage
	start := page * portalsPerPage
	for i := start; i < len(portals) && i < start+portalsPerPage; i++ {
		if portalCard(i).contains(x, y) {
			return i
		}
	}
	return -1
}

func (w *Workspace) handlePortalNavigation(event experience.Event) bool {
	if !w.desktop {
		return false
	}
	if event.Kind == experience.KeyboardCancel {
		owned := w.portals.open || w.portals.held != 0
		w.portals.open, w.portals.held = false, 0
		w.portals.pressed, w.portals.pressedID = -1, ""
		return owned
	}
	if event.Kind == experience.PointerCancel {
		owned := w.portals.open || w.portals.pressed >= 0
		w.portals.pressed, w.portals.pressedID = -1, ""
		return owned
	}
	if event.Kind == experience.KeyInput {
		key := portalNavigationKey(event)
		if key != 0 && w.portals.held&key != 0 {
			if !event.Pressed {
				w.portals.held &^= key
			}
			return true
		}
		global := event.Modifiers == experience.ModControl|experience.ModAlt
		if event.Pressed && !event.Repeat && global && key == portalKeyG {
			w.portals.held |= key
			if w.portals.open {
				w.portals.open = false
			} else {
				w.openPortalAtlas()
			}
			return true
		}
		if event.Pressed && !event.Repeat && global && (key == portalKeyLeft || key == portalKeyRight) {
			w.portals.held |= key
			if key == portalKeyLeft {
				w.cyclePortal(-1)
			} else {
				w.cyclePortal(1)
			}
			return true
		}
		if !w.portals.open {
			return false
		}
		if !event.Pressed {
			return true
		}
		if key != 0 {
			w.portals.held |= key
		}
		if event.Repeat || event.Modifiers != 0 {
			return true
		}
		portals := w.navigationPortals()
		if len(portals) == 0 {
			return true
		}
		w.portals.selected = max(0, min(w.portals.selected, len(portals)-1))
		switch key {
		case portalKeyLeft:
			w.portals.selected = max(0, w.portals.selected-1)
		case portalKeyRight:
			w.portals.selected = min(len(portals)-1, w.portals.selected+1)
		case portalKeyUp:
			w.portals.selected = max(0, w.portals.selected-4)
		case portalKeyDown:
			w.portals.selected = min(len(portals)-1, w.portals.selected+4)
		case portalKeyEnter, portalKeyKeypadEnter:
			w.activatePortal(portals[w.portals.selected])
		case portalKeyEscape:
			w.portals.open = false
		}
		return true
	}

	if w.portals.open {
		portals := w.navigationPortals()
		x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
		switch event.Kind {
		case experience.PointerDown:
			if applicationButton(event) == 272 {
				w.portals.pressed = w.portalAt(x, y, portals)
				if w.portals.pressed >= 0 {
					w.portals.selected = w.portals.pressed
					w.portals.pressedID = portals[w.portals.pressed].ID
				}
			}
		case experience.PointerUp:
			pressed, pressedID := w.portals.pressed, w.portals.pressedID
			w.portals.pressed, w.portals.pressedID = -1, ""
			current := w.portalAt(x, y, portals)
			if applicationButton(event) == 272 && pressed >= 0 && current >= 0 && portals[current].ID == pressedID {
				w.activatePortal(portals[current])
			}
		case experience.PointerScroll:
			if event.ScrollY > 0 {
				w.portals.selected = min(len(portals)-1, w.portals.selected+4)
			} else if event.ScrollY < 0 {
				w.portals.selected = max(0, w.portals.selected-4)
			}
		}
		return true
	}

	if w.portals.pressed == -2 {
		if event.Kind == experience.PointerUp && applicationButton(event) == 272 {
			x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
			w.portals.pressed, w.portals.pressedID = -1, ""
			if portalButton.contains(x, y) {
				w.openPortalAtlas()
			}
		}
		return true
	}
	if event.Kind == experience.PointerDown && applicationButton(event) == 272 {
		x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
		if portalButton.contains(x, y) {
			w.clearApplicationFocus()
			w.portals.pressed, w.portals.pressedID = -2, ""
			return true
		}
	}
	return false
}

func (w *Workspace) drawPortalAtlas() {
	if !w.portals.open {
		return
	}
	portals := w.navigationPortals()
	if len(portals) == 0 {
		return
	}
	w.portals.selected = max(0, min(w.portals.selected, len(portals)-1))
	page := w.portals.selected / portalsPerPage
	pages := (len(portals) + portalsPerPage - 1) / portalsPerPage
	start := page * portalsPerPage
	w.rect(253, 166, 1135, 615, bg, .97)
	w.line(253, 166, 1388, 166, 2, teal, .8)
	w.text(282, 184, 22, "SPATIAL PORTALS", ink, 1)
	w.text(1072, 190, 11, fmt.Sprintf("PAGE %02d / %02d", page+1, pages), muted, 1)
	for i := start; i < len(portals) && i < start+portalsPerPage; i++ {
		portal := portals[i]
		card := portalCard(i)
		selected := i == w.portals.selected
		alpha, color := float32(.08), muted
		if selected {
			alpha, color = .16, teal
		}
		w.rect(card.x, card.y, card.w, card.h, teal, alpha)
		w.line(card.x, card.y, card.x+42, card.y, 2, color, .9)
		w.line(card.x, card.y, card.x, card.y+42, 2, color, .9)
		w.line(card.x+card.w-42, card.y+card.h, card.x+card.w, card.y+card.h, 2, color, .65)
		w.line(card.x+card.w, card.y+card.h-42, card.x+card.w, card.y+card.h, 2, color, .65)
		w.text(card.x+16, card.y+14, 10, fmt.Sprintf("%02d / %s", i+1, portal.SpaceName), color, 1)
		w.shapedText(w.ox+(card.x+16)*w.scale, w.oy+(card.y+45)*w.scale, 15*w.scale, (card.w-32)*w.scale, portal.Title, w.color(ink, 1))
		count := "NO LIVE WINDOWS"
		if portal.Applications == 1 {
			count = "1 WINDOW"
		} else if portal.Applications > 1 {
			count = fmt.Sprintf("%d WINDOWS", portal.Applications)
		}
		w.text(card.x+16, card.y+91, 10, count, muted, .9)
		if portal.Applications > 0 {
			w.text(card.x+134, card.y+91, 9, fmt.Sprintf("%+.1f / %+.1f / %+.1f", portal.X, portal.Y, portal.Depth), muted, .75)
		}
	}
	w.text(282, 704, 12, "ARROWS / SELECT    ENTER / TRAVEL    ESC / CLOSE", teal, 1)
	w.text(282, 730, 11, "Ctrl+Alt+Left/Right travels between groups without opening this atlas.", muted, 1)
}

func (w *Workspace) drawPortalButton() {
	w.line(portalButton.x, portalButton.y+portalButton.h, portalButton.x+portalButton.w, portalButton.y+portalButton.h, 1, muted, .25)
	w.text(portalButton.x+8, portalButton.y+5, 10, "PORTALS / CTRL+ALT+G", teal, .9)
}
