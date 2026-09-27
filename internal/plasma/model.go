package plasma

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

const (
	CanvasWidth  = 884
	CanvasHeight = 720
	maxBlend     = 64
)

var panelArea = fluid.Rect{X: 24, Y: 104, Width: 836, Height: 512}

// Panel content remains independent when its decorative outline joins another
// panel. Bounds is the current presentation; committed targets live in Model.
type Panel struct {
	ID, Title, Body string
	Bounds          fluid.Rect
	Radius          float32
	Fuse            bool
}

type panelDrag struct {
	index          int
	offset         fluid.Point
	bounds, target fluid.Rect
	spring         fluid.Spring
	active         int
	order          []int
	moved          bool
}

type modelKey struct {
	code uint32
	name experience.Key
}

// Model owns layout and interaction, never renderer resources. Blend is in
// logical pixels. Fusion controls appearance, while Snap controls placement;
// neither creates a movement group.
type Model struct {
	Panels               []Panel
	Blend                float32
	Snap, Motion, Fusion bool
	Active               int
	targets              []fluid.Rect
	springs              []fluid.Spring
	order                []int
	drag                 *panelDrag
	keys                 map[modelKey]bool
}

func NewModel() *Model {
	m := &Model{}
	m.Reset()
	return m
}

func (m *Model) Reset() {
	m.Panels = []Panel{
		{ID: "inbox", Title: "Inbox", Body: "4 unread, 2 flagged.", Bounds: fluid.Rect{X: 40, Y: 120, Width: 214, Height: 132}, Radius: 22, Fuse: true},
		{ID: "tasks", Title: "Tasks", Body: "Ship the release notes.", Bounds: fluid.Rect{X: 258, Y: 120, Width: 214, Height: 132}, Radius: 22, Fuse: true},
		{ID: "notes", Title: "Notes", Body: "Draft for Thursday's demo.", Bounds: fluid.Rect{X: 130, Y: 420, Width: 214, Height: 132}, Radius: 22, Fuse: true},
		{ID: "files", Title: "Files", Body: "23 items, 1.2 GB.", Bounds: fluid.Rect{X: 350, Y: 420, Width: 214, Height: 132}, Radius: 22, Fuse: true},
		{ID: "player", Title: "Player", Body: "Side B · 12:41 remaining.", Bounds: fluid.Rect{X: 350, Y: 284, Width: 214, Height: 132}, Radius: 22, Fuse: true},
	}
	m.Blend, m.Snap, m.Motion, m.Fusion, m.Active = 32, true, true, true, -1
	m.targets = make([]fluid.Rect, len(m.Panels))
	m.springs = make([]fluid.Spring, len(m.Panels))
	m.order = make([]int, len(m.Panels))
	for i, panel := range m.Panels {
		m.targets[i] = panel.Bounds
		m.springs[i].Position = fluid.Point{X: panel.Bounds.X, Y: panel.Bounds.Y}
		m.order[i] = i
	}
	m.drag, m.keys = nil, nil
}

// DrawOrder returns stable panel indices in back-to-front order. Selecting a
// panel changes stacking without changing its index, identity or content.
func (m *Model) DrawOrder() []int { return append([]int(nil), m.order...) }
func (m *Model) Dragging() bool   { return m.drag != nil }

func (m *Model) Joined(index int) bool {
	if !m.Fusion || index < 0 || index >= len(m.Panels) {
		return false
	}
	panel := m.Panels[index]
	if !panel.Fuse {
		return false
	}
	a := fluid.Surface{Bounds: panel.Bounds, Radius: panel.Radius, Fuse: true}
	for i, other := range m.Panels {
		if i != index && fluid.Joined(a, fluid.Surface{Bounds: other.Bounds, Radius: other.Radius, Fuse: other.Fuse}, m.Blend) {
			return true
		}
	}
	return false
}

func contains(r fluid.Rect, x, y float32) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.Width && y < r.Y+r.Height
}

func primary(event experience.Event) bool {
	return event.Button == experience.ButtonPrimary || event.ButtonCode == 272
}

func finite(value float32) bool { return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) }

func stroke(event experience.Event) modelKey {
	if event.Keycode != 0 {
		return modelKey{code: event.Keycode}
	}
	return modelKey{name: event.Key}
}

func (m *Model) front(index int) {
	m.Active = index
	for at, value := range m.order {
		if value == index {
			copy(m.order[at:], m.order[at+1:])
			m.order[len(m.order)-1] = index
			return
		}
	}
}

func (m *Model) cancelDrag() bool {
	if m.drag == nil {
		return false
	}
	drag := m.drag
	m.Panels[drag.index].Bounds = drag.bounds
	m.targets[drag.index], m.springs[drag.index] = drag.target, drag.spring
	m.Active = drag.active
	copy(m.order, drag.order)
	m.drag = nil
	return true
}

func (m *Model) moveDrag(x, y float32) {
	if m.drag == nil || !finite(x) || !finite(y) {
		return
	}
	i := m.drag.index
	bounds := m.Panels[i].Bounds
	bounds.X, bounds.Y = x-m.drag.offset.X, y-m.drag.offset.Y
	bounds = fluid.ClampRect(bounds, panelArea)
	if bounds == m.Panels[i].Bounds {
		return
	}
	m.drag.moved = true
	// Follow the pointer exactly. Spring presentation is reserved for settling
	// after a snap or keyboard move, so capture never trails behind the cursor.
	m.Panels[i].Bounds, m.targets[i] = bounds, bounds
	m.springs[i] = fluid.Spring{Position: fluid.Point{X: bounds.X, Y: bounds.Y}}
}

func (m *Model) place(index int, target fluid.Rect) {
	target = fluid.ClampRect(target, panelArea)
	m.targets[index] = target
	if !m.Motion {
		m.Panels[index].Bounds = target
		m.springs[index] = fluid.Spring{Position: fluid.Point{X: target.X, Y: target.Y}}
	}
}

func (m *Model) finishDrag() {
	index := m.drag.index
	if !m.drag.moved {
		// A focus click does not snap the panel or interrupt an earlier settle.
		m.targets[index], m.springs[index] = m.drag.target, m.drag.spring
		m.drag = nil
		return
	}
	target := m.targets[index]
	if m.Snap {
		point := fluid.SnapGrid(fluid.Point{X: target.X, Y: target.Y}, 8)
		target.X, target.Y = point.X, point.Y
		peers := make([]fluid.Rect, 0, len(m.Panels)-1)
		for i := range m.Panels {
			if i != index {
				peers = append(peers, m.targets[i])
			}
		}
		target, _ = fluid.SnapMagnetic(target, peers, 10)
	}
	m.drag = nil
	m.place(index, target)
}

// Handle consumes logical coordinates. Hosts must route an already captured
// drag here before testing footer controls; decorative bridges are not targets.
func (m *Model) Handle(event experience.Event) bool {
	switch event.Kind {
	case experience.PointerCancel:
		return m.cancelDrag()
	case experience.KeyboardCancel:
		changed := m.Active >= 0 || len(m.keys) != 0
		if m.drag != nil {
			m.drag.active = -1
		}
		m.Active, m.keys = -1, nil
		return changed
	case experience.PointerDown:
		if m.drag != nil {
			return true
		}
		if !primary(event) || !finite(event.X) || !finite(event.Y) {
			return false
		}
		for at := len(m.order) - 1; at >= 0; at-- {
			index := m.order[at]
			bounds := m.Panels[index].Bounds
			if contains(bounds, event.X, event.Y) {
				m.drag = &panelDrag{index: index, offset: fluid.Point{X: event.X - bounds.X, Y: event.Y - bounds.Y}, bounds: bounds, target: m.targets[index], spring: m.springs[index], active: m.Active, order: m.DrawOrder()}
				m.front(index)
				return true
			}
		}
		changed := m.Active >= 0
		m.Active = -1
		return changed
	case experience.PointerMove:
		if m.drag != nil {
			m.moveDrag(event.X, event.Y)
			return true
		}
	case experience.PointerUp:
		if m.drag != nil {
			if primary(event) {
				m.moveDrag(event.X, event.Y)
				m.finishDrag()
			}
			return true
		}
	case experience.PointerScroll:
		return m.drag != nil
	case experience.KeyInput:
		key := stroke(event)
		if !event.Pressed {
			if m.keys[key] {
				delete(m.keys, key)
				return true
			}
			return false
		}
		if event.Modifiers&(experience.ModControl|experience.ModAlt|experience.ModSuper) != 0 {
			return false
		}
		handled := false
		switch {
		case event.Key == experience.KeyEscape || event.Keycode == 1:
			handled = m.cancelDrag()
		case m.drag != nil:
			if m.keys == nil {
				m.keys = make(map[modelKey]bool)
			}
			m.keys[key] = true
			return true
		case event.Key == "Tab" || event.Keycode == 15:
			next := m.Active + 1
			if event.Modifiers.Has(experience.ModShift) {
				next = m.Active - 1
				if m.Active < 0 {
					next = len(m.Panels) - 1
				}
			}
			m.front((next + len(m.Panels)) % len(m.Panels))
			handled = true
		case event.Key == experience.KeyR || event.Key == "r" || event.Keycode == 19:
			if !event.Repeat {
				m.Reset()
			}
			handled = true
		default:
			if m.Active >= 0 && m.Active < len(m.Panels) {
				dx, dy := float32(0), float32(0)
				switch {
				case event.Key == experience.KeyLeft || event.Keycode == 105:
					dx = -1
				case event.Key == experience.KeyRight || event.Keycode == 106:
					dx = 1
				case event.Key == "ArrowUp" || event.Keycode == 103:
					dy = -1
				case event.Key == "ArrowDown" || event.Keycode == 108:
					dy = 1
				}
				if dx != 0 || dy != 0 {
					step := float32(8)
					if event.Modifiers.Has(experience.ModShift) {
						step = 1
					}
					target := m.targets[m.Active]
					target.X, target.Y = target.X+dx*step, target.Y+dy*step
					m.place(m.Active, target)
					handled = true
				}
			}
		}
		if handled {
			if m.keys == nil {
				m.keys = make(map[modelKey]bool)
			}
			m.keys[key] = true
		}
		return handled
	}
	return false
}

func (m *Model) Update(delta time.Duration) {
	if m.Motion && delta <= 0 {
		return
	}
	for i := range m.Panels {
		if m.drag != nil && m.drag.index == i {
			continue
		}
		target := m.targets[i]
		point := fluid.Point{X: target.X, Y: target.Y}
		if !m.Motion {
			m.springs[i] = fluid.Spring{Position: point}
		} else {
			m.springs[i].Step(point, min(float32(delta.Seconds()), 10), 5, 1)
		}
		position := m.springs[i].Position
		if math.Abs(float64(position.X-point.X))+math.Abs(float64(position.Y-point.Y)) < .005 && math.Abs(float64(m.springs[i].Velocity.X))+math.Abs(float64(m.springs[i].Velocity.Y)) < .01 {
			position = point
			m.springs[i] = fluid.Spring{Position: point}
		}
		bounds := target
		bounds.X, bounds.Y = position.X, position.Y
		bounds = fluid.ClampRect(bounds, panelArea)
		if bounds.X != position.X {
			m.springs[i].Velocity.X = 0
		}
		if bounds.Y != position.Y {
			m.springs[i].Velocity.Y = 0
		}
		m.springs[i].Position = fluid.Point{X: bounds.X, Y: bounds.Y}
		m.Panels[i].Bounds = bounds
	}
}

type savedPanel struct {
	ID     string     `json:"id"`
	Bounds fluid.Rect `json:"bounds"`
	Radius float32    `json:"radius"`
	Fuse   bool       `json:"fuse"`
}

type savedModel struct {
	Version int          `json:"version"`
	Panels  []savedPanel `json:"panels"`
	Blend   float32      `json:"blend"`
	Snap    bool         `json:"snap"`
	Motion  bool         `json:"motion"`
	Fusion  bool         `json:"fusion"`
	Active  int          `json:"active"`
	Order   []int        `json:"order"`
}

func (state savedModel) validate() error {
	defaults := NewModel()
	if state.Version != 1 || len(state.Panels) != len(defaults.Panels) || len(state.Order) != len(state.Panels) || state.Active < -1 || state.Active >= len(state.Panels) || !finite(state.Blend) || state.Blend < 0 || state.Blend > maxBlend {
		return fmt.Errorf("invalid plasma layout version, panels or settings")
	}
	seen := make([]bool, len(state.Panels))
	for i, panel := range state.Panels {
		b := panel.Bounds
		if panel.ID != defaults.Panels[i].ID || !finite(b.X) || !finite(b.Y) || !finite(b.Width) || !finite(b.Height) || b.Width < 96 || b.Height < 64 || b.X < panelArea.X || b.Y < panelArea.Y || b.X+b.Width > panelArea.X+panelArea.Width || b.Y+b.Height > panelArea.Y+panelArea.Height || !finite(panel.Radius) || panel.Radius < 0 || panel.Radius > min(b.Width, b.Height)/2 {
			return fmt.Errorf("invalid plasma panel %d", i)
		}
		index := state.Order[i]
		if index < 0 || index >= len(state.Panels) || seen[index] {
			return fmt.Errorf("invalid plasma stacking order")
		}
		seen[index] = true
	}
	return nil
}

// SaveState and CheckpointState serialize committed destinations, not live
// capture or intermediate spring frames. Neither changes ongoing interaction.
func (m *Model) SaveState() ([]byte, error) {
	if len(m.Panels) != len(m.targets) || len(m.Panels) != len(m.springs) {
		return nil, fmt.Errorf("invalid plasma model storage")
	}
	state := savedModel{Version: 1, Blend: m.Blend, Snap: m.Snap, Motion: m.Motion, Fusion: m.Fusion, Active: m.Active, Order: m.DrawOrder()}
	if m.drag != nil {
		state.Active, state.Order = m.drag.active, append([]int(nil), m.drag.order...)
	}
	for i, panel := range m.Panels {
		bounds := m.targets[i]
		if m.drag != nil && m.drag.index == i {
			bounds = m.drag.target
		}
		state.Panels = append(state.Panels, savedPanel{ID: panel.ID, Bounds: bounds, Radius: panel.Radius, Fuse: panel.Fuse})
	}
	if err := state.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(state)
}

func (m *Model) CheckpointState() ([]byte, error) { return m.SaveState() }

func (m *Model) LoadState(data []byte) error {
	if len(data) > 64<<10 {
		return fmt.Errorf("plasma layout exceeds 64 KiB")
	}
	var state savedModel
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode plasma layout: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("plasma layout contains trailing data")
	}
	if err := state.validate(); err != nil {
		return err
	}
	next := NewModel()
	next.Blend, next.Snap, next.Motion, next.Fusion, next.Active = state.Blend, state.Snap, state.Motion, state.Fusion, state.Active
	copy(next.order, state.Order)
	for i, saved := range state.Panels {
		next.Panels[i].Bounds, next.Panels[i].Radius, next.Panels[i].Fuse = saved.Bounds, saved.Radius, saved.Fuse
		next.targets[i] = saved.Bounds
		next.springs[i] = fluid.Spring{Position: fluid.Point{X: saved.Bounds.X, Y: saved.Bounds.Y}}
	}
	*m = *next
	return nil
}

// Demo replays a short logical-input script at an absolute clock. Reconstructing
// its small state makes screenshots independent of update frequency or skips.
func (m *Model) Demo(elapsed time.Duration) {
	blend, snap, motion, fusion := m.Blend, m.Snap, m.Motion, m.Fusion
	m.Reset()
	m.Blend, m.Snap, m.Motion, m.Fusion = blend, snap, motion, fusion
	t := math.Mod(max(0, elapsed.Seconds()), 17)
	previous := 0.
	for _, step := range []struct {
		panel      int
		start, end float64
		x, y       float32
	}{{0, 1, 4, 620, 160}, {0, 5, 8, 40, 120}, {4, 9, 12, 620, 400}, {4, 13, 16, 350, 284}} {
		if t < step.start {
			break
		}
		m.Update(time.Duration((step.start - previous) * float64(time.Second)))
		bounds := m.Panels[step.panel].Bounds
		m.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: bounds.X + 20, Y: bounds.Y + 20})
		fraction := min(1., (t-step.start)/(step.end-step.start))
		fraction = fraction * fraction * (3 - 2*fraction)
		x := bounds.X + (step.x-bounds.X)*float32(fraction) + 20
		y := bounds.Y + (step.y-bounds.Y)*float32(fraction) + 20
		m.Handle(experience.Event{Kind: experience.PointerMove, X: x, Y: y})
		if t < step.end {
			return
		}
		m.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y})
		previous = step.end
	}
	m.Update(time.Duration((t - previous) * float64(time.Second)))
}
