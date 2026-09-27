package plasma

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

func modelBeginDrag(t *testing.T, m *Model, index int) {
	t.Helper()
	b := m.Panels[index].Bounds
	if !m.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: b.X + 20, Y: b.Y + 20}) || !m.Dragging() || m.Active != index {
		t.Fatalf("panel %d did not capture pointer: active=%d, dragging=%v", index, m.Active, m.Dragging())
	}
}

func modelMove(t *testing.T, m *Model, kind experience.EventKind, x, y float32) {
	t.Helper()
	if !m.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: x + 20, Y: y + 20}) {
		t.Fatal("captured pointer event was not consumed")
	}
}

func modelSave(t *testing.T, m *Model) []byte {
	t.Helper()
	data, err := m.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestModelFusionIsVisualAndBridgesCannotCapture(t *testing.T) {
	m := NewModel()
	for i := range m.Panels {
		if !m.Joined(i) {
			t.Fatalf("initial panel %s should belong to a joined component", m.Panels[i].ID)
		}
	}
	if fluid.Joined(fluid.Surface{Bounds: m.Panels[2].Bounds, Radius: 22, Fuse: true}, fluid.Surface{Bounds: m.Panels[4].Bounds, Radius: 22, Fuse: true}, m.Blend) {
		t.Fatal("Notes and Player should join through Files, not directly")
	}
	if m.Handle(experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 256, Y: 186}) || m.Dragging() {
		t.Fatal("decorative bridge captured the pointer")
	}
	before := append([]Panel(nil), m.Panels...)
	modelBeginDrag(t, m, 3)
	modelMove(t, m, experience.PointerMove, 646, 104)
	for i := range m.Panels {
		if i != 3 && m.Panels[i] != before[i] {
			t.Fatalf("moving Files moved neighboring panel %s", m.Panels[i].ID)
		}
	}
	for _, i := range []int{2, 3, 4} {
		if m.Joined(i) {
			t.Fatalf("panel %s stayed joined after pulling Files away", m.Panels[i].ID)
		}
	}
	if !m.Joined(0) || !m.Joined(1) {
		t.Fatal("moving Files disturbed the other joined component")
	}
	m.Handle(experience.Event{Kind: experience.PointerCancel})
	m.Fusion = false
	for i := range m.Panels {
		if m.Joined(i) {
			t.Fatal("global fusion switch was ignored")
		}
	}
	m.Fusion = true
	m.Panels[3].Fuse = false
	for _, i := range []int{2, 3, 4} {
		if m.Joined(i) {
			t.Fatal("non-fusing panel continued to bridge neighbors")
		}
	}
	m.Blend = 0
	if m.Joined(0) || m.Joined(1) || m.Joined(-1) || m.Joined(5) {
		t.Fatal("zero blend should not span a gap, and invalid indices must not join")
	}
}

func TestModelPointerCaptureBoundsAndButtonOwnership(t *testing.T) {
	m := NewModel()
	m.Snap, m.Motion = false, false
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerMove, 5000, -5000)
	b := m.Panels[0].Bounds
	if b.X != 646 || b.Y != 104 {
		t.Fatalf("capture escaped the panel area: %+v", b)
	}
	if !m.Handle(experience.Event{Kind: experience.PointerMove, X: float32(math.NaN()), Y: 150}) || m.Panels[0].Bounds != b {
		t.Fatal("nonfinite pointer changed the captured panel")
	}
	if !m.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: 0, Y: 0}) || !m.Dragging() {
		t.Fatal("secondary release ended the primary capture")
	}
	if !m.Handle(experience.Event{Kind: experience.PointerScroll, ScrollY: 10}) {
		t.Fatal("scroll escaped an active capture")
	}
	if !m.Handle(experience.Event{Kind: experience.PointerUp, ButtonCode: 272, X: 5020, Y: -4980}) || m.Dragging() || m.Panels[0].Bounds != b {
		t.Fatal("primary release outside the canvas did not complete the bounded drag")
	}
	if m.Handle(experience.Event{Kind: experience.PointerMove, X: 30, Y: 30}) {
		t.Fatal("uncaptured pointer movement was consumed")
	}
}

func TestModelCancelAndCheckpointPreserveCommittedState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event experience.Event
	}{{"pointer", experience.Event{Kind: experience.PointerCancel}}, {"escape", experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Pressed: true}}} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel()
			m.front(4)
			before := modelSave(t, m)
			modelBeginDrag(t, m, 0)
			modelMove(t, m, experience.PointerMove, 620, 170)
			live := m.Panels[0].Bounds
			checkpoint, err := m.CheckpointState()
			if err != nil || !bytes.Equal(checkpoint, before) || !m.Dragging() || m.Panels[0].Bounds != live {
				t.Fatalf("checkpoint changed or committed a pending drag: %v", err)
			}
			restored := NewModel()
			if err := restored.LoadState(checkpoint); err != nil || restored.Dragging() || !bytes.Equal(modelSave(t, restored), before) {
				t.Fatalf("checkpoint restored transient interaction: %v", err)
			}
			if !m.Handle(tc.event) || m.Dragging() || !bytes.Equal(modelSave(t, m), before) || m.Panels[0].Bounds.X != 40 {
				t.Fatal("cancel did not roll back placement, focus and stacking")
			}
		})
	}
}

func TestModelKeyboardCancelDoesNotCancelPointerCapture(t *testing.T) {
	m := NewModel()
	m.Snap, m.Motion = false, false
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerMove, 620, 170)
	if !m.Handle(experience.Event{Kind: experience.KeyboardCancel}) || !m.Dragging() || m.Active != -1 {
		t.Fatal("keyboard cancellation incorrectly released pointer capture")
	}
	modelMove(t, m, experience.PointerUp, 620, 170)
	if m.Panels[0].Bounds.X != 620 || m.Dragging() || m.Active != -1 {
		t.Fatal("pointer could not finish after keyboard focus left")
	}
	modelBeginDrag(t, m, 0)
	m.Handle(experience.Event{Kind: experience.KeyboardCancel})
	m.Handle(experience.Event{Kind: experience.PointerCancel})
	if m.Active != -1 {
		t.Fatal("pointer rollback resurrected cancelled keyboard focus")
	}
}

func TestModelClickFocusDoesNotMoveOrSnap(t *testing.T) {
	m := NewModel()
	original := m.Panels[0].Bounds
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, original.X, original.Y)
	m.Update(time.Second)
	if m.Panels[0].Bounds != original || m.Active != 0 {
		t.Fatal("a focus click changed the panel placement")
	}
	// A click during a keyboard spring must preserve its committed destination.
	m.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyRight, Pressed: true})
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, original.X, original.Y)
	m.Update(time.Second)
	if m.Panels[0].Bounds.X != original.X+8 {
		t.Fatal("focus click discarded an in-flight keyboard movement")
	}
}

func TestModelSnapAndSpringPreserveIndependentPanels(t *testing.T) {
	m := NewModel()
	before := m.Panels[1].Bounds
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, 46, 120)
	if m.targets[0].X != 44 || m.targets[0].X+m.targets[0].Width != before.X {
		t.Fatalf("nearby edge did not magnetically dock: %+v", m.targets[0])
	}
	if m.Panels[0].Bounds.X != 46 || m.Dragging() {
		t.Fatal("spring presentation jumped on release")
	}
	m.Update(30 * time.Millisecond)
	if x := m.Panels[0].Bounds.X; x <= 44 || x >= 46 {
		t.Fatalf("spring failed to move toward target: %v", x)
	}
	m.Update(20 * time.Second)
	if m.Panels[0].Bounds.X != 44 || m.Panels[1].Bounds != before {
		t.Fatal("long update failed to settle, or snapped neighbor moved")
	}
	m.Motion = false
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, 621, 171)
	if b := m.Panels[0].Bounds; b.X != 624 || b.Y != 168 {
		t.Fatalf("isolated panel did not snap to the grid: %+v", b)
	}
	m.Snap = false
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, 617, 173)
	if b := m.Panels[0].Bounds; b.X != 617 || b.Y != 173 {
		t.Fatalf("disabled snapping still adjusted placement: %+v", b)
	}
}

func TestModelKeyboardFocusMovementAndShortcuts(t *testing.T) {
	m := NewModel()
	m.Motion = false
	press := func(key experience.Key, code uint32, mods experience.Modifiers) {
		t.Helper()
		if !m.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Keycode: code, Modifiers: mods, Pressed: true}) || !m.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Keycode: code}) {
			t.Fatalf("keyboard action/release not consumed: %s (%d)", key, code)
		}
	}
	press("Tab", 15, 0)
	press(experience.KeyRight, 106, 0)
	press("ArrowUp", 103, experience.ModShift)
	if m.Active != 0 || m.Panels[0].Bounds.X != 48 || m.Panels[0].Bounds.Y != 119 || m.Panels[1].Bounds.X != 258 {
		t.Fatal("keyboard focus or independent coarse/fine movement is incorrect")
	}
	press("Tab", 15, experience.ModShift)
	if m.Active != 4 || m.DrawOrder()[4] != 4 {
		t.Fatal("reverse Tab did not wrap and raise the focused panel")
	}
	before := modelSave(t, m)
	if m.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyR, Keycode: 19, Pressed: true, Modifiers: experience.ModControl}) || !bytes.Equal(before, modelSave(t, m)) {
		t.Fatal("model stole a host shortcut or reset while Control was held")
	}
	press(experience.KeyR, 19, 0)
	if !bytes.Equal(modelSave(t, m), modelSave(t, NewModel())) {
		t.Fatal("R did not reset the layout and controls")
	}
	press("Tab", 0, experience.ModShift)
	if m.Active != 4 {
		t.Fatal("reverse Tab from no focus should start at the final panel")
	}
	modelBeginDrag(t, m, 0)
	press(experience.KeyRight, 106, 0)
	if m.Panels[0].Bounds.X != 40 {
		t.Fatal("arrow key moved a captured pointer target")
	}
}

func TestModelReducedMotionAndKeyboardBounds(t *testing.T) {
	m := NewModel()
	m.front(0)
	m.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyRight, Pressed: true})
	m.Update(10 * time.Millisecond)
	if m.Panels[0].Bounds.X == m.targets[0].X {
		t.Fatal("test requires an intermediate spring frame")
	}
	m.Motion = false
	m.Update(0)
	if m.Panels[0].Bounds.X != 48 || m.springs[0].Velocity != (fluid.Point{}) {
		t.Fatal("disabling motion did not immediately settle the destination")
	}
	for range 200 {
		m.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyLeft, Pressed: true})
		m.Handle(experience.Event{Kind: experience.KeyInput, Key: "ArrowDown", Pressed: true})
	}
	if b := m.Panels[0].Bounds; b.X != 24 || b.Y != 484 || b != m.targets[0] {
		t.Fatalf("keyboard movement escaped the panel area: %+v", b)
	}
	m.Motion = true
	m.Update(time.Second)
	if b := m.Panels[0].Bounds; b.X != 24 || b.Y != 484 {
		t.Fatal("re-enabling motion resurrected stale spring velocity")
	}
}

func TestModelStackingDoesNotChangeIdentity(t *testing.T) {
	m := NewModel()
	m.Snap, m.Motion = false, false
	modelBeginDrag(t, m, 0)
	modelMove(t, m, experience.PointerUp, 258, 120)
	modelBeginDrag(t, m, 0)
	if m.Panels[0].ID != "inbox" || m.Panels[1].ID != "tasks" || m.order[4] != 0 {
		t.Fatal("bringing the dragged panel forward changed identity or picked the covered panel")
	}
	copy := m.DrawOrder()
	copy[4] = 1
	if m.DrawOrder()[4] != 0 {
		t.Fatal("DrawOrder exposed mutable model state")
	}
}

func TestModelStateRoundTripCommitsDestinationNotAnimation(t *testing.T) {
	m := NewModel()
	m.Blend, m.Snap, m.Fusion = 17, false, false
	m.Panels[2].Fuse, m.Panels[2].Radius = false, 16
	m.Handle(experience.Event{Kind: experience.KeyInput, Key: "Tab", Pressed: true})
	m.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyRight, Pressed: true})
	m.Update(10 * time.Millisecond)
	if m.Panels[0].Bounds.X == m.targets[0].X {
		t.Fatal("test requires an intermediate spring frame")
	}
	data := modelSave(t, m)
	loaded := NewModel()
	modelBeginDrag(t, loaded, 3)
	if err := loaded.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(modelSave(t, loaded), data) || loaded.Dragging() || len(loaded.keys) != 0 || loaded.Panels[0].Bounds.X != 48 || loaded.Panels[2].Fuse || loaded.Panels[2].Radius != 16 {
		t.Fatal("state round-trip lost committed settings or restored transient interaction")
	}
	loaded.Update(time.Second)
	if loaded.Panels[0].Bounds.X != 48 {
		t.Fatal("restored panel drifted after loading a committed destination")
	}
}

func TestModelRejectsInvalidStateAtomically(t *testing.T) {
	base := modelSave(t, NewModel())
	cases := []struct {
		name string
		edit func(*savedModel)
	}{
		{"version", func(s *savedModel) { s.Version = 2 }},
		{"missing panel", func(s *savedModel) { s.Panels = s.Panels[:4] }},
		{"duplicate identity", func(s *savedModel) { s.Panels[0].ID = "tasks" }},
		{"duplicate order", func(s *savedModel) { s.Order[0] = 1 }},
		{"out of range order", func(s *savedModel) { s.Order[0] = 5 }},
		{"invalid active", func(s *savedModel) { s.Active = 5 }},
		{"negative blend", func(s *savedModel) { s.Blend = -1 }},
		{"oversize blend", func(s *savedModel) { s.Blend = 65 }},
		{"escaped canvas", func(s *savedModel) { s.Panels[0].Bounds.X = 900 }},
		{"header overlap", func(s *savedModel) { s.Panels[0].Bounds.Y = 80 }},
		{"oversize width", func(s *savedModel) { s.Panels[0].Bounds.Width = 900 }},
		{"empty width", func(s *savedModel) { s.Panels[0].Bounds.Width = 0 }},
		{"oversize radius", func(s *savedModel) { s.Panels[0].Radius = 100 }},
	}
	inputs := map[string][]byte{"trailing data": append(append([]byte{}, base...), []byte(" {}")...), "unknown field": bytes.Replace(base, []byte(`"version":1`), []byte(`"version":1,"surprise":true`), 1), "excessive size": bytes.Repeat([]byte(" "), (64<<10)+1), "nonfinite": bytes.Replace(base, []byte(`"blend":32`), []byte(`"blend":1e1000`), 1)}
	for _, c := range cases {
		var state savedModel
		if err := json.Unmarshal(base, &state); err != nil {
			t.Fatal(err)
		}
		c.edit(&state)
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name] = data
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			m := NewModel()
			modelBeginDrag(t, m, 3)
			modelMove(t, m, experience.PointerMove, 600, 160)
			before, bounds, capture := modelSave(t, m), m.Panels[3].Bounds, m.drag
			if err := m.LoadState(data); err == nil {
				t.Fatal("invalid layout was accepted")
			}
			if !bytes.Equal(before, modelSave(t, m)) || m.Panels[3].Bounds != bounds || m.drag != capture {
				t.Fatal("rejected layout mutated live interaction")
			}
		})
	}
	invalid := NewModel()
	invalid.Blend = float32(math.NaN())
	if _, err := invalid.SaveState(); err == nil {
		t.Fatal("nonfinite model setting was saved")
	}
	invalid = NewModel()
	invalid.Panels = append(invalid.Panels, Panel{})
	if _, err := invalid.SaveState(); err == nil {
		t.Fatal("inconsistent model storage was saved")
	}
}

func TestModelDemoIsCadenceIndependentAndUsesCapture(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 2500 * time.Millisecond, 4500 * time.Millisecond, 6500 * time.Millisecond, 8500 * time.Millisecond, 10500 * time.Millisecond, 14500 * time.Millisecond, 16500 * time.Millisecond, 19500 * time.Millisecond} {
		direct, stepped := NewModel(), NewModel()
		direct.Blend, stepped.Blend = 24, 24
		direct.Demo(elapsed)
		for at := time.Duration(0); at < elapsed; at += 73 * time.Millisecond {
			stepped.Demo(at)
			stepped.Update(73 * time.Millisecond)
		}
		stepped.Demo(elapsed)
		if !reflect.DeepEqual(direct.Panels, stepped.Panels) || !bytes.Equal(modelSave(t, direct), modelSave(t, stepped)) || direct.Dragging() != stepped.Dragging() || direct.Blend != 24 {
			t.Fatalf("demo depends on frame cadence at %s", elapsed)
		}
		for _, p := range direct.Panels {
			if p.Bounds != fluid.ClampRect(p.Bounds, panelArea) {
				t.Fatalf("demo moved %s outside the canvas", p.ID)
			}
		}
	}
	m := NewModel()
	m.Demo(3500 * time.Millisecond)
	if !m.Dragging() || m.Active != 0 || m.Joined(0) || m.Joined(1) {
		t.Fatal("script did not pull Inbox away using real capture")
	}
	m.Demo(8500 * time.Millisecond)
	if m.Dragging() || !m.Joined(0) || !m.Joined(1) {
		t.Fatal("script did not return Inbox to its neighbor")
	}
}
