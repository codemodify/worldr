package nativeui

import (
	"fmt"
	"image"
	"math"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
)

// Action is one normalized result from Controller.Handle. Changed reports a
// range value; Activated reports a completed button-like click or key action.
type Action struct {
	ID           string
	Kind         Kind
	Activated    bool
	Changed      bool
	ChangedFocus bool
	Consumed     bool
	Value        float64
}

// Controller supplies focus traversal, press/release capture, hover state and
// continuous range interaction for nativeapp v1 events. Text editing remains
// application-owned; field controls receive focus and the app handles commit,
// preedit and physical-key events with its preferred editor.
type Controller struct {
	controls                  []Control
	focused, pressed, hovered string
	owned                     map[uint32]bool
	painter                   *Painter
	rangeGrab                 float64
	rangeOrigin               image.Point
	rangeStart                float64
}

// SetTheme keeps hit geometry and text measurements synchronized with Painter.
// Set it alongside Painter.SetTheme when the active skin changes.
func (c *Controller) SetTheme(theme Theme) error {
	p, err := NewPainter(theme)
	if err != nil {
		return err
	}
	if c.painter != nil {
		_ = c.painter.Close()
	}
	c.painter = p
	return nil
}

// Close releases the controller's measurement font and clears interaction state.
func (c *Controller) Close() error {
	c.Blur()
	if c.painter == nil {
		return nil
	}
	err := c.painter.Close()
	c.painter = nil
	return err
}

func (c *Controller) layout(control Control) ControlLayout {
	if c.painter == nil {
		c.painter, _ = NewPainter(DefaultTheme())
	}
	return c.painter.Layout(control)
}

// SetControls atomically replaces the current control layout. Invalid or
// duplicate controls leave the previous layout and captures unchanged.
func (c *Controller) SetControls(controls []Control) error {
	return c.setControls(controls, image.Rectangle{})
}

// SetControlsWithin additionally proves that every semantic node fits the
// application's current framebuffer bounds.
func (c *Controller) SetControlsWithin(controls []Control, surface image.Rectangle) error {
	if surface.Empty() || surface.Min.X != 0 || surface.Min.Y != 0 || surface.Dx() > nativeapp.MaxTextureWidth || surface.Dy() > nativeapp.MaxTextureHeight {
		return fmt.Errorf("native UI: invalid v1 surface bounds")
	}
	return c.setControls(controls, surface)
}

func (c *Controller) setControls(controls []Control, surface image.Rectangle) error {
	if len(controls) > 512 {
		return fmt.Errorf("native UI: control set exceeds 512 nodes")
	}
	seen := make(map[string]bool, len(controls))
	next := make([]Control, len(controls))
	for i, control := range controls {
		if err := control.validate(true); err != nil {
			return fmt.Errorf("native UI: control %d: %w", i, err)
		}
		if seen[control.ID] {
			return fmt.Errorf("native UI: duplicate control ID %q", control.ID)
		}
		if !surface.Empty() && !control.Bounds.In(surface) {
			return fmt.Errorf("native UI: control %d bounds are outside the surface", i)
		}
		seen[control.ID] = true
		next[i] = control
	}
	c.controls = next
	if !c.interactive(c.focused) {
		c.focused = ""
	}
	if !c.interactive(c.pressed) {
		c.pressed = ""
	}
	if !c.present(c.hovered) {
		c.hovered = ""
	}
	return nil
}

// Controls returns an owned snapshot.
func (c *Controller) Controls() []Control { return append([]Control(nil), c.controls...) }
func (c *Controller) FocusedID() string   { return c.focused }

func (c *Controller) present(id string) bool {
	for _, control := range c.controls {
		if control.ID == id {
			return true
		}
	}
	return false
}

func (c *Controller) control(id string) (Control, bool) {
	for _, control := range c.controls {
		if control.ID == id {
			return control, true
		}
	}
	return Control{}, false
}

func (c *Controller) interactive(id string) bool {
	control, ok := c.control(id)
	return ok && control.interactive()
}

// Focus changes focus only to an enabled interactive control. An empty ID
// explicitly clears focus.
func (c *Controller) Focus(id string) bool {
	if id != "" && !c.interactive(id) {
		return false
	}
	c.focused = id
	return true
}

// Blur clears focus and all transient interaction state.
func (c *Controller) Blur() {
	c.focused, c.pressed, c.hovered = "", "", ""
	c.owned = nil
}

// Decorate merges controller-owned focus/hover/press state into a control just
// before painting. Application-owned Selected/Disabled/Invalid state remains.
func (c *Controller) Decorate(control Control) Control {
	control.State.Focused = control.ID != "" && control.ID == c.focused
	control.State.Hovered = control.ID != "" && control.ID == c.hovered
	control.State.Pressed = control.ID != "" && control.ID == c.pressed
	return control
}

// Semantics builds an owned v1 tree from the current control list.
func (c *Controller) Semantics() nativeapp.SemanticTree {
	nodes := make([]nativeapp.SemanticNode, 0, len(c.controls))
	for _, control := range c.controls {
		nodes = append(nodes, control.SemanticNode())
	}
	return nativeapp.SemanticTree{Nodes: nodes, FocusedID: c.focused}
}

func eventPoint(event nativeapp.Event) image.Point {
	return image.Pt(int(math.Floor(float64(event.X))), int(math.Floor(float64(event.Y))))
}

func primary(event nativeapp.Event) bool {
	return event.Button == nativeapp.ButtonPrimary || event.ButtonCode == 272
}

func (c *Controller) hit(point image.Point, interactiveOnly bool) string {
	for i := len(c.controls) - 1; i >= 0; i-- {
		control := c.controls[i]
		if point.In(control.Bounds) && (!interactiveOnly || control.interactive()) {
			return control.ID
		}
	}
	return ""
}

func rangeValue(control Control, track image.Rectangle, point image.Point) float64 {
	vertical := control.Kind == KindScrollbar && control.Bounds.Dy() >= control.Bounds.Dx() || control.Kind == KindSplitter && control.Bounds.Dy() >= control.Bounds.Dx()
	var fraction float64
	if vertical {
		fraction = float64(point.Y-track.Min.Y) / float64(max(1, track.Dy()-1))
	} else {
		fraction = float64(point.X-track.Min.X) / float64(max(1, track.Dx()-1))
	}
	fraction = min(1, max(0, fraction))
	value := control.Min + fraction*(control.Max-control.Min)
	if control.Step > 0 {
		value = control.Min + math.Round((value-control.Min)/control.Step)*control.Step
	}
	return min(control.Max, max(control.Min, value))
}

func (c *Controller) rangeAction(id string, point image.Point) Action {
	control, ok := c.control(id)
	if !ok || !control.rangeControl() {
		return Action{}
	}
	if control.Kind == KindSplitter {
		verticalBar := control.Bounds.Dy() >= control.Bounds.Dx()
		span := max(control.Bounds.Dx(), control.Bounds.Dy())
		delta := point.Y - c.rangeOrigin.Y
		if verticalBar {
			delta = point.X - c.rangeOrigin.X
		}
		if !control.RangeBounds.Empty() {
			span = control.RangeBounds.Dy()
			if verticalBar {
				span = control.RangeBounds.Dx()
			}
		}
		value := c.rangeStart + float64(delta)/float64(max(1, span))*(control.Max-control.Min)
		if control.Step > 0 {
			value = control.Min + math.Round((value-control.Min)/control.Step)*control.Step
		}
		value = min(control.Max, max(control.Min, value))
		return Action{ID: id, Kind: control.Kind, Changed: value != control.Value, Consumed: true, Value: value}
	}
	layout := c.layout(control)
	track := layout.Track
	if control.Kind == KindScrollbar {
		vertical := control.Bounds.Dy() >= control.Bounds.Dx()
		if vertical {
			point.Y -= int(c.rangeGrab)
			track.Max.Y -= layout.Thumb.Dy() - 1
		} else {
			point.X -= int(c.rangeGrab)
			track.Max.X -= layout.Thumb.Dx() - 1
		}
	}
	value := rangeValue(control, track, point)
	return Action{ID: id, Kind: control.Kind, Changed: value != control.Value, Consumed: true, Value: value}
}

func (c *Controller) moveFocus(delta int) Action {
	ids := make([]string, 0, len(c.controls))
	current := -1
	for _, control := range c.controls {
		if !control.interactive() {
			continue
		}
		if control.ID == c.focused {
			current = len(ids)
		}
		ids = append(ids, control.ID)
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
	control, _ := c.control(c.focused)
	return Action{ID: c.focused, Kind: control.Kind, ChangedFocus: true, Consumed: true}
}

func keyCode(event nativeapp.Event) uint32 { return event.Keycode }

// Handle consumes one nativeapp event and returns at most one control action.
func (c *Controller) Handle(event nativeapp.Event) Action {
	switch event.Kind {
	case nativeapp.KeyboardCancel:
		had := c.focused != "" || c.pressed != ""
		c.Blur()
		return Action{Consumed: had}
	case nativeapp.PointerCancel:
		had := c.pressed != ""
		c.pressed = ""
		return Action{Consumed: had}
	case nativeapp.PointerMove:
		point := eventPoint(event)
		if c.pressed != "" {
			c.hovered = c.hit(point, false)
			if control, ok := c.control(c.pressed); ok && control.rangeControl() {
				return c.rangeAction(c.pressed, point)
			}
			return Action{ID: c.pressed, Consumed: true}
		}
		c.hovered = c.hit(point, true)
		return Action{}
	case nativeapp.PointerDown:
		if !primary(event) {
			return Action{}
		}
		point := eventPoint(event)
		id := c.hit(point, true)
		if id == "" {
			c.hovered = ""
			return Action{}
		}
		control, _ := c.control(id)
		changedFocus := c.focused != id
		c.focused, c.pressed, c.hovered = id, id, id
		if control.rangeControl() {
			c.rangeOrigin, c.rangeStart = point, control.Value
			c.rangeGrab = 0
			if control.Kind == KindScrollbar {
				thumb := c.layout(control).Thumb
				if control.Bounds.Dy() >= control.Bounds.Dx() {
					c.rangeGrab = float64(thumb.Dy()) / 2
					if point.In(thumb) {
						c.rangeGrab = float64(point.Y - thumb.Min.Y)
					}
				} else {
					c.rangeGrab = float64(thumb.Dx()) / 2
					if point.In(thumb) {
						c.rangeGrab = float64(point.X - thumb.Min.X)
					}
				}
			}
			action := c.rangeAction(id, point)
			action.ChangedFocus = changedFocus
			return action
		}
		return Action{ID: id, Kind: control.Kind, ChangedFocus: changedFocus, Consumed: true}
	case nativeapp.PointerUp:
		if !primary(event) {
			return Action{}
		}
		id := c.pressed
		c.pressed = ""
		if id == "" {
			return Action{}
		}
		point := eventPoint(event)
		c.hovered = c.hit(point, true)
		control, ok := c.control(id)
		if !ok {
			return Action{Consumed: true}
		}
		if control.rangeControl() {
			return c.rangeAction(id, point)
		}
		activate := point.In(control.Bounds) && control.Kind != KindField && control.Kind != KindTextArea
		return Action{ID: id, Kind: control.Kind, Activated: activate, Consumed: true}
	case nativeapp.KeyInput:
		code := keyCode(event)
		if !event.Pressed {
			if c.owned != nil && c.owned[code] {
				delete(c.owned, code)
				return Action{Consumed: true}
			}
			return Action{}
		}
		if event.Modifiers&(nativeapp.ModControl|nativeapp.ModAlt|nativeapp.ModSuper) == 0 && (event.Key == "Tab" || code == 15) {
			if c.owned == nil {
				c.owned = make(map[uint32]bool)
			}
			c.owned[code] = true
			delta := 1
			if event.Modifiers.Has(nativeapp.ModShift) {
				delta = -1
			}
			return c.moveFocus(delta)
		}
		control, ok := c.control(c.focused)
		if !ok || !control.interactive() {
			return Action{}
		}
		if control.rangeControl() && event.Modifiers == 0 {
			direction := 0
			switch {
			case event.Key == "ArrowLeft" || event.Key == "ArrowDown" || code == 105 || code == 108:
				direction = -1
			case event.Key == "ArrowRight" || event.Key == "ArrowUp" || code == 106 || code == 103:
				direction = 1
			}
			if direction != 0 {
				step := control.Step
				if step <= 0 {
					step = (control.Max - control.Min) / 100
				}
				value := min(control.Max, max(control.Min, control.Value+float64(direction)*step))
				if c.owned == nil {
					c.owned = make(map[uint32]bool)
				}
				c.owned[code] = true
				return Action{ID: control.ID, Kind: control.Kind, Changed: value != control.Value, Consumed: true, Value: value}
			}
		}
		if event.Modifiers == 0 && (event.Key == "Enter" || event.Key == " " || event.Key == "Space" || code == 28 || code == 96 || code == 57) && control.Kind != KindField && control.Kind != KindTextArea {
			if c.owned == nil {
				c.owned = make(map[uint32]bool)
			}
			c.owned[code] = true
			return Action{ID: control.ID, Kind: control.Kind, Activated: !event.Repeat, Consumed: true}
		}
	}
	return Action{}
}
