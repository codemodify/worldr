package nativeui

import (
	"image"

	"github.com/codemodify/worldr/internal/experience"
)

type Role string

const (
	RoleButton    Role = "button"
	RoleTextField Role = "textbox"
	RoleMenuItem  Role = "menuitem"
	RoleLabel     Role = "label"
	RoleSlider    Role = "slider"
	RoleImage     Role = "image"
	RoleDocument  Role = "document"
	RoleStatus    Role = "status"
)

type Node struct {
	ID                        string
	Role                      Role
	Label, Value, Description string
	Bounds                    image.Rectangle
	Disabled, Selected        bool
}

type SemanticTree struct {
	Nodes     []Node
	FocusedID string
}
type Action struct {
	ID                                string
	Activated, ChangedFocus, Consumed bool
}

// Controller uses press/release activation, one pointer capture and one focus
// owner. A semantic tree is data for the host accessibility adapter; publishing
// it alone does not claim integration with an OS accessibility service.
type Controller struct {
	nodes            []Node
	focused, pressed string
	owned            [768]bool
}

func (c *Controller) SetNodes(nodes []Node) {
	seen := make(map[string]bool, len(nodes))
	c.nodes = c.nodes[:0]
	for _, node := range nodes {
		if len(c.nodes) == 1024 {
			break
		}
		if node.ID == "" || seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		c.nodes = append(c.nodes, node)
	}
	if !c.focusable(c.focused) {
		c.focused = ""
	}
	if !c.focusable(c.pressed) {
		c.pressed = ""
	}
}
func (c *Controller) Semantics() SemanticTree {
	return SemanticTree{Nodes: append([]Node(nil), c.nodes...), FocusedID: c.focused}
}
func (c *Controller) FocusedID() string { return c.focused }
func (c *Controller) Focus(id string) bool {
	if id != "" && !c.focusable(id) {
		return false
	}
	c.focused = id
	return true
}
func (c *Controller) Blur() { c.focused = ""; c.pressed = ""; c.owned = [768]bool{} }
func (c *Controller) focusable(id string) bool {
	for _, n := range c.nodes {
		if n.ID == id {
			return !n.Disabled && (n.Role == RoleButton || n.Role == RoleTextField || n.Role == RoleMenuItem) && !n.Bounds.Empty()
		}
	}
	return false
}
func (c *Controller) focusedNode() Node {
	for _, n := range c.nodes {
		if n.ID == c.focused {
			return n
		}
	}
	return Node{}
}
func (c *Controller) move(delta int) Action {
	var ids []string
	current := -1
	for _, n := range c.nodes {
		if c.focusable(n.ID) {
			if n.ID == c.focused {
				current = len(ids)
			}
			ids = append(ids, n.ID)
		}
	}
	if len(ids) == 0 {
		return Action{}
	}
	if current < 0 {
		if delta < 0 {
			current = 0
		} else {
			current = -1
		}
	}
	c.focused = ids[(current+delta+len(ids))%len(ids)]
	return Action{ID: c.focused, ChangedFocus: true, Consumed: true}
}
func (c *Controller) Handle(e experience.Event) Action {
	switch e.Kind {
	case experience.KeyboardCancel:
		c.Blur()
	case experience.PointerCancel:
		c.pressed = ""
	case experience.PointerMove:
		if c.pressed != "" {
			return Action{ID: c.pressed, Consumed: true}
		}
	case experience.PointerDown:
		if e.Button != experience.ButtonPrimary && e.ButtonCode != 272 {
			return Action{}
		}
		point := image.Pt(int(e.X), int(e.Y))
		for i := len(c.nodes) - 1; i >= 0; i-- {
			n := c.nodes[i]
			if point.In(n.Bounds) && c.focusable(n.ID) {
				changed := c.focused != n.ID
				c.focused = n.ID
				c.pressed = n.ID
				return Action{ID: n.ID, ChangedFocus: changed, Consumed: true}
			}
		}
	case experience.PointerUp:
		if e.Button != experience.ButtonPrimary && e.ButtonCode != 272 {
			return Action{}
		}
		id := c.pressed
		c.pressed = ""
		if id == "" {
			return Action{}
		}
		for _, n := range c.nodes {
			if n.ID == id {
				return Action{ID: id, Activated: image.Pt(int(e.X), int(e.Y)).In(n.Bounds) && n.Role != RoleTextField && !n.Disabled, Consumed: true}
			}
		}
	case experience.KeyInput:
		if e.Keycode < uint32(len(c.owned)) && !e.Pressed && c.owned[e.Keycode] {
			c.owned[e.Keycode] = false
			return Action{Consumed: true}
		}
		if !e.Pressed {
			return Action{}
		}
		if e.Keycode == 15 && !e.Modifiers.Has(experience.ModControl) && !e.Modifiers.Has(experience.ModAlt) && !e.Modifiers.Has(experience.ModSuper) {
			c.owned[e.Keycode] = true
			delta := 1
			if e.Modifiers.Has(experience.ModShift) {
				delta = -1
			}
			return c.move(delta)
		}
		n := c.focusedNode()
		if n.Role == RoleMenuItem && (e.Keycode == 103 || e.Keycode == 108) {
			delta := 1
			if e.Keycode == 103 {
				delta = -1
			}
			c.owned[e.Keycode] = true
			return c.move(delta)
		}
		if c.focusable(c.focused) && (n.Role == RoleButton || n.Role == RoleMenuItem) && (e.Keycode == 28 || e.Keycode == 57) && e.Modifiers == 0 {
			c.owned[e.Keycode] = true
			return Action{ID: c.focused, Activated: !e.Repeat, Consumed: true}
		}
	}
	return Action{}
}
