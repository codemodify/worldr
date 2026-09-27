package plasma

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func testPlayground(t testing.TB) *Playground {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func playgroundControl(t testing.TB, p *Playground, id string) fluid.Rect {
	t.Helper()
	p.buildControls()
	for _, control := range p.controls {
		if control.id == id {
			return control.bounds
		}
	}
	t.Fatalf("missing control %q", id)
	return fluid.Rect{}
}

func playgroundPointer(p *Playground, kind experience.EventKind, x, y float32) bool {
	return p.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: p.left + x*p.scale, Y: p.top + y*p.scale})
}

func playgroundClick(t testing.TB, p *Playground, id string) {
	t.Helper()
	b := playgroundControl(t, p, id)
	x, y := b.X+b.Width/2, b.Y+b.Height/2
	if !playgroundPointer(p, experience.PointerDown, x, y) || !playgroundPointer(p, experience.PointerUp, x, y) {
		t.Fatalf("control %q did not handle its click", id)
	}
}

func playgroundState(t testing.TB, p *Playground) []byte {
	t.Helper()
	data, err := p.CheckpointState()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPlaygroundWidgetsOwnPointerCapture(t *testing.T) {
	for _, id := range []string{"lumen", "studio", "slate", "fusion", "snap", "motion", "read", "task", "play", "reset"} {
		t.Run(id, func(t *testing.T) {
			p := testPlayground(t)
			before := append([]Panel(nil), p.model.Panels...)
			b := playgroundControl(t, p, id)
			x, y := b.X+b.Width/2, b.Y+b.Height/2
			if !playgroundPointer(p, experience.PointerDown, x, y) || p.pressed != id || p.model.Dragging() {
				t.Fatalf("widget down did not capture independently: pressed=%q dragging=%v", p.pressed, p.model.Dragging())
			}
			for i, panel := range p.model.Panels {
				if panelButton(panel.ID) == id && (p.model.Active != i || p.model.order[len(p.model.order)-1] != i) {
					t.Fatal("child button did not focus and raise its containing panel")
				}
			}
			playgroundPointer(p, experience.PointerMove, x+1, y+1)
			if p.model.Dragging() || !reflect.DeepEqual(before, p.model.Panels) {
				t.Fatal("moving a captured widget moved a panel")
			}
			playgroundPointer(p, experience.PointerUp, x+1, y+1)
			if p.pressed != "" || p.model.Dragging() {
				t.Fatal("widget release left capture active")
			}
			switch id {
			case "studio", "slate":
				if p.preset != id {
					t.Fatal("preset did not activate")
				}
			case "fusion":
				if p.model.Fusion {
					t.Fatal("fusion did not toggle")
				}
			case "snap":
				if p.model.Snap {
					t.Fatal("snap did not toggle")
				}
			case "motion":
				if p.model.Motion {
					t.Fatal("motion did not toggle")
				}
			case "read":
				if !p.read {
					t.Fatal("read did not activate")
				}
			case "task":
				if !p.done {
					t.Fatal("task did not activate")
				}
			case "play":
				if p.playing {
					t.Fatal("play did not toggle")
				}
			}
		})
	}
}

func TestPlaygroundCoveredChildDoesNotInterceptPanel(t *testing.T) {
	for _, top := range []int{1, 2} {
		t.Run(fmt.Sprintf("panel-%d", top), func(t *testing.T) {
			p := testPlayground(t)
			p.model.Panels[top].Bounds = p.model.Panels[0].Bounds
			b := playgroundControl(t, p, "read")
			x, y := b.X+b.Width/2, b.Y+b.Height/2
			playgroundPointer(p, experience.PointerDown, x, y)
			if top == 1 {
				if p.pressed != "task" || p.model.Dragging() {
					t.Fatal("topmost panel's button did not own input")
				}
				playgroundPointer(p, experience.PointerUp, x, y)
				if !p.done || p.read {
					t.Fatal("covered button activated")
				}
			} else if p.pressed != "" || !p.model.Dragging() || p.model.Active != top {
				t.Fatal("covered child intercepted a drag on the topmost panel")
			}
		})
	}
}

func TestPlaygroundPanelCaptureCrossesWidgets(t *testing.T) {
	p := testPlayground(t)
	playgroundPointer(p, experience.PointerDown, 65, 145)
	if !p.model.Dragging() {
		t.Fatal("panel did not capture the pointer")
	}
	for _, id := range []string{"fusion", "blend", "lens"} {
		b := playgroundControl(t, p, id)
		playgroundPointer(p, experience.PointerMove, b.X+b.Width/2, b.Y+b.Height/2)
		if !p.model.Dragging() || p.pressed != "" {
			t.Fatal("widget stole an ongoing panel capture")
		}
	}
	b := playgroundControl(t, p, "fusion")
	playgroundPointer(p, experience.PointerUp, b.X+b.Width/2, b.Y+b.Height/2)
	if p.model.Dragging() || !p.model.Fusion || p.model.Blend != 32 || p.lens != .65 {
		t.Fatal("releasing a panel over widgets activated a widget")
	}
}

func TestPlaygroundSliderCancelAndCheckpoint(t *testing.T) {
	for _, id := range []string{"blend", "lens"} {
		for _, cancel := range []experience.Event{{Kind: experience.PointerCancel}, {Kind: experience.KeyInput, Key: experience.KeyEscape, Pressed: true}} {
			t.Run(fmt.Sprintf("%s/%d", id, cancel.Kind), func(t *testing.T) {
				p := testPlayground(t)
				before := playgroundState(t, p)
				b := playgroundControl(t, p, id)
				playgroundPointer(p, experience.PointerDown, b.X+b.Width*.2, b.Y+12)
				playgroundPointer(p, experience.PointerMove, b.X+b.Width+300, b.Y-400)
				if p.model.Dragging() || p.pressed != id {
					t.Fatal("slider lost independent capture")
				}
				point := p.pointer
				for _, kind := range []experience.EventKind{experience.PointerMove, experience.PointerUp} {
					if p.Handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: float32(math.NaN()), Y: 10}) {
						t.Fatal("captured slider accepted a nonfinite pointer")
					}
					if p.pressed != id || p.pointer != point {
						t.Fatal("invalid pointer disturbed widget capture")
					}
				}
				if id == "blend" && p.model.Blend != 64 || id == "lens" && p.lens != 1 {
					t.Fatal("captured slider did not clamp outside its track")
				}
				if got := playgroundState(t, p); !bytes.Equal(got, before) {
					t.Fatal("checkpoint persisted an uncommitted slider value")
				}
				if p.pressed != id || id == "blend" && p.model.Blend != 64 || id == "lens" && p.lens != 1 {
					t.Fatal("checkpoint changed live capture")
				}
				// Keyboard focus loss is independent of an active pointer capture.
				p.Handle(experience.Event{Kind: experience.KeyboardCancel})
				if p.pressed != id {
					t.Fatal("keyboard cancel ended pointer capture")
				}
				p.Handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonSecondary, X: b.X, Y: b.Y})
				if p.pressed != id {
					t.Fatal("unrelated button release ended pointer capture")
				}
				if !p.Handle(cancel) {
					t.Fatal("canceling a captured slider was not reported handled")
				}
				if p.pressed != "" || !bytes.Equal(playgroundState(t, p), before) {
					t.Fatal("cancel did not restore committed slider value")
				}
			})
		}
	}
}

func TestPlaygroundPointerCancelReportsChanges(t *testing.T) {
	p := testPlayground(t)
	if p.Handle(experience.Event{Kind: experience.PointerCancel}) {
		t.Fatal("idle pointer cancel reported a change")
	}
	playgroundPointer(p, experience.PointerMove, 625, 50)
	if !p.Handle(experience.Event{Kind: experience.PointerCancel}) || p.pointerActive {
		t.Fatal("canceling hover did not report and clear the change")
	}
	if p.Handle(experience.Event{Kind: experience.PointerCancel}) {
		t.Fatal("repeated pointer cancel reported a change")
	}
}

func TestPlaygroundKeyboardPanelActions(t *testing.T) {
	p := testPlayground(t)
	actions := func() [3]bool { return [3]bool{p.read, p.done, p.playing} }
	initial := actions()
	for _, key := range []experience.Key{"Enter", experience.KeySpace} {
		if p.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true}) || actions() != initial {
			t.Fatal("unfocused keyboard action activated a child button")
		}
	}
	for i, panel := range p.model.Panels {
		if !p.Handle(experience.Event{Kind: experience.KeyInput, Key: "Tab", Pressed: true}) {
			t.Fatal("Tab did not select a panel")
		}
		p.Handle(experience.Event{Kind: experience.KeyInput, Key: "Tab"})
		if p.model.Active != i || p.model.order[len(p.model.order)-1] != i {
			t.Fatalf("Tab did not focus and raise panel %q", panel.ID)
		}
		for _, key := range []experience.Key{"Enter", experience.KeySpace} {
			before := actions()
			for _, modifier := range []experience.Modifiers{experience.ModControl, experience.ModAlt, experience.ModSuper} {
				p.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true, Modifiers: modifier})
				if actions() != before {
					t.Fatalf("modified %s activated %q", key, panel.ID)
				}
			}
			handled := p.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true})
			expected := before
			switch panel.ID {
			case "inbox":
				expected[0] = !expected[0]
			case "tasks":
				expected[1] = !expected[1]
			case "player":
				expected[2] = !expected[2]
			}
			if actions() != expected || handled != (panelButton(panel.ID) != "") {
				t.Fatalf("%s activated the wrong action for %q", key, panel.ID)
			}
			p.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true, Repeat: true})
			p.Handle(experience.Event{Kind: experience.KeyInput, Key: key})
			if actions() != expected {
				t.Fatal("repeat or key release activated a child button")
			}
		}
	}
	if actions() != initial {
		t.Fatal("Enter and Space did not toggle each child action independently")
	}
	p.Handle(experience.Event{Kind: experience.KeyboardCancel})
	if p.model.Active != -1 {
		t.Fatal("keyboard focus loss retained selected panel")
	}
	p.Handle(experience.Event{Kind: experience.KeyInput, Key: "Enter", Pressed: true})
	if actions() != initial {
		t.Fatal("keyboard action survived loss of focus")
	}
	playgroundPointer(p, experience.PointerDown, 65, 145)
	for _, key := range []experience.Key{"Enter", experience.KeySpace} {
		p.Handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true})
		if actions() != initial || !p.model.Dragging() {
			t.Fatal("keyboard child action interrupted panel capture")
		}
	}
}

func TestPlaygroundStateRoundtripAndTransactionalLoad(t *testing.T) {
	p := testPlayground(t)
	for _, id := range []string{"studio", "fusion", "snap", "motion", "read", "task", "play"} {
		playgroundClick(t, p, id)
	}
	for _, id := range []string{"blend", "lens"} {
		b := playgroundControl(t, p, id)
		playgroundPointer(p, experience.PointerDown, b.X+b.Width*.75, b.Y+10)
		playgroundPointer(p, experience.PointerUp, b.X+b.Width*.75, b.Y+10)
	}
	playgroundPointer(p, experience.PointerDown, 65, 145)
	playgroundPointer(p, experience.PointerMove, 615, 315)
	playgroundPointer(p, experience.PointerUp, 615, 315)
	s, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetSkin(s); err != nil {
		t.Fatal(err)
	}
	colorBefore := p.custom.Palette["background"]
	s.Palette["background"] = "#123456"
	if p.custom.Palette["background"] != colorBefore {
		t.Fatal("SetSkin retained caller-owned palette")
	}
	data := playgroundState(t, p)
	q := testPlayground(t)
	if err := q.LoadState(data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, playgroundState(t, q)) {
		t.Fatal("state roundtrip lost committed values")
	}
	if q.model.Blend != 48 || q.lens != .75 || q.model.Fusion || q.model.Snap || q.model.Motion || !q.done || !q.read || q.playing || q.preset != "studio" || q.custom == nil || q.custom.ID != "plasma" {
		t.Fatal("state roundtrip lost controls or custom skin")
	}
	if q.model.Panels[0].Bounds == NewModel().Panels[0].Bounds {
		t.Fatal("panel layout was not persisted")
	}

	var valid document
	if err := json.Unmarshal(data, &valid); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*document){
		"version": func(d *document) { d.Version = 2 },
		"preset":  func(d *document) { d.Preset = "missing" },
		"lens":    func(d *document) { d.Lens = 2 },
		"layout":  func(d *document) { d.Layout = json.RawMessage(`{}`) },
		"skin":    func(d *document) { s := d.Skin.Clone(); s.Palette["background"] = "not-a-color"; d.Skin = &s },
	}
	invalid := map[string][]byte{"unknown": append([]byte(`{"unknown":true,`), data[1:]...), "trailing": append(append([]byte(nil), data...), []byte(` {}`)...), "truncated": data[:len(data)/2]}
	for name, mutate := range mutations {
		d := valid
		mutate(&d)
		encoded, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		invalid[name] = encoded
	}
	for name, corrupt := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := q.LoadState(corrupt); err == nil {
				t.Fatal("invalid state accepted")
			}
			if !bytes.Equal(data, playgroundState(t, q)) {
				t.Fatal("failed load changed existing document")
			}
		})
	}
	badSkin := q.custom.Clone()
	badSkin.Palette["background"] = "invalid"
	if err := q.SetSkin(badSkin); err == nil || !bytes.Equal(data, playgroundState(t, q)) {
		t.Fatal("invalid SetSkin was not transactional")
	}
}

func TestPlaygroundFinitePointersAndShortcutRepeats(t *testing.T) {
	p := testPlayground(t)
	playgroundPointer(p, experience.PointerMove, 80, 150)
	before := playgroundState(t, p)
	point, active := p.pointer, p.pointerActive
	for _, kind := range []experience.EventKind{experience.PointerMove, experience.PointerDown, experience.PointerUp} {
		for _, v := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), fluid.MaxCoordinate + 1} {
			for axis := 0; axis < 2; axis++ {
				e := experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: 80, Y: 150}
				if axis == 0 {
					e.X = v
				} else {
					e.Y = v
				}
				if p.Handle(e) {
					t.Fatal("invalid pointer was accepted")
				}
				if p.pointer != point || p.pointerActive != active || p.model.Dragging() || !bytes.Equal(before, playgroundState(t, p)) {
					t.Fatal("invalid pointer changed state")
				}
			}
		}
	}
	for _, key := range []experience.Key{"F", "S", "M"} {
		e := experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true}
		if !p.Handle(e) {
			t.Fatal("shortcut not handled")
		}
		changed := playgroundState(t, p)
		e.Repeat = true
		p.Handle(e)
		if !bytes.Equal(changed, playgroundState(t, p)) {
			t.Fatal("key repeat toggled a setting")
		}
		e.Repeat, e.Modifiers = false, experience.ModControl
		p.Handle(e)
		if !bytes.Equal(changed, playgroundState(t, p)) {
			t.Fatal("modified shortcut changed a setting")
		}
	}
}

func TestPlaygroundScaleAndResizeCancel(t *testing.T) {
	p := testPlayground(t)
	frame := p.Draw(2000, 1440)
	if p.scale != 2 || p.left != 116 || p.top != 0 {
		t.Fatalf("incorrect letterbox transform: %g,%g,%g", p.scale, p.left, p.top)
	}
	field := playgroundField(t, frame)
	if field.Bounds != (fluid.Rect{Width: 2000, Height: 1440}) || field.Style.Blend != p.model.Blend*2 || field.Surfaces[0].Bounds != p.screen(p.model.Panels[0].Bounds) {
		t.Fatal("fluid geometry did not use framebuffer scaling")
	}
	playgroundClick(t, p, "read")
	if !p.read {
		t.Fatal("scaled child click missed")
	}
	before := p.model.Panels[0].Bounds
	playgroundPointer(p, experience.PointerDown, before.X+25, before.Y+25)
	playgroundPointer(p, experience.PointerMove, before.X+125, before.Y+65)
	if !p.model.Dragging() || p.model.Panels[0].Bounds.X != before.X+100 || p.model.Panels[0].Bounds.Y != before.Y+40 {
		t.Fatal("scaled pointer did not move panel in logical units")
	}
	p.Draw(1326, 1080)
	if p.model.Dragging() || p.model.Panels[0].Bounds != before {
		t.Fatal("resize did not cancel panel capture")
	}
	blend := p.model.Blend
	b := playgroundControl(t, p, "blend")
	playgroundPointer(p, experience.PointerDown, b.X+b.Width*.9, b.Y+10)
	p.Draw(884, 720)
	if p.pressed != "" || p.model.Blend != blend {
		t.Fatal("resize did not roll back slider capture")
	}
	// The maximum supported native framebuffer still carries the scaled blend,
	// rather than silently weakening joins at high pixel density.
	p.model.Blend = 64
	field = playgroundField(t, p.Draw(7680, 4320))
	if field.Style.Blend != 384 {
		t.Fatalf("high-DPI blend was clamped: %g", field.Style.Blend)
	}
	if err := field.Validate(); err != nil {
		t.Fatal(err)
	}
}

func playgroundField(t testing.TB, frame render.Frame) fluid.Field {
	t.Helper()
	if !frame.LinearColor || len(frame.Commands) == 0 || frame.Commands[0].Kind != render.FluidCommand {
		t.Fatal("frame does not begin with a linear-color fluid pass")
	}
	if err := frame.Commands[0].Fluid.Validate(); err != nil {
		t.Fatal(err)
	}
	return frame.Commands[0].Fluid
}

func playgroundImages(frame render.Frame) map[uint64][][4]float32 {
	images := make(map[uint64][][4]float32)
	for _, command := range frame.Commands {
		if command.Kind == render.ImageCommand {
			id := command.Image.Texture.ID()
			images[id] = append(images[id], command.Image.Bounds)
		}
	}
	return images
}

func TestPlaygroundDragReusesUnchangedLabelTextures(t *testing.T) {
	p := testPlayground(t)
	beforeImages := playgroundImages(p.Draw(884, 720))
	if len(beforeImages) == 0 {
		t.Fatal("frame contains no labels")
	}
	before := make(map[labelKey]*render.Texture, len(p.labels))
	pixels := make(map[uint64]render.TextureUpdate, len(p.labels))
	for key, texture := range p.labels {
		before[key] = texture
		pixels[texture.ID()], _ = texture.Snapshot(0)
	}
	playgroundPointer(p, experience.PointerDown, 65, 145)
	playgroundPointer(p, experience.PointerMove, 655, 180)
	afterImages := playgroundImages(p.Draw(884, 720))
	for key := range p.labels {
		if before[key] == nil && key.text != "Standalone" {
			t.Fatalf("moving geometry rerasterized unchanged content %q", key.text)
		}
	}
	moved := false
	for key, old := range before {
		if now := p.labels[key]; now != old {
			t.Fatalf("unchanged label %q was rerasterized", key.text)
		}
		if after, ok := afterImages[old.ID()]; ok && !reflect.DeepEqual(beforeImages[old.ID()], after) {
			moved = true
		}
		current, _ := old.Snapshot(0)
		if !reflect.DeepEqual(pixels[old.ID()], current) {
			t.Fatalf("published label %q changed its pixels", key.text)
		}
	}
	if !moved {
		t.Fatal("drag did not reposition any retained image")
	}
	if retired := p.RetiredTextures(); len(retired) != 0 {
		t.Fatalf("drag retired %d retained labels", len(retired))
	}
	stable := playgroundImages(p.Draw(884, 720))
	if !reflect.DeepEqual(stable, afterImages) {
		t.Fatal("warm frame changed retained image identities or bounds")
	}
}

func TestPlaygroundLabelRetirementOccursBetweenFrames(t *testing.T) {
	for _, reason := range []string{"count", "bytes", "resize", "close"} {
		t.Run(reason, func(t *testing.T) {
			p := testPlayground(t)
			oldImages := playgroundImages(p.Draw(884, 720))
			old := make(map[uint64]bool, len(p.labels))
			for _, texture := range p.labels {
				old[texture.ID()] = true
			}
			width, height := 884, 720
			switch reason {
			case "count":
				// Seed stale, owned labels so normal next-frame eviction is exercised.
				for len(p.labels) <= 160 {
					texture, err := render.NewTexture(1, 1, []byte{0, 0, 0, 0})
					if err != nil {
						t.Fatal(err)
					}
					p.labels[labelKey{text: fmt.Sprintf("stale-%d", len(p.labels))}] = texture
					p.labelBytes += 4
					old[texture.ID()] = true
				}
			case "bytes":
				texture, err := render.NewTexture(2048, 2048, make([]byte, 16<<20))
				if err != nil {
					t.Fatal(err)
				}
				p.labels[labelKey{text: "stale-large"}] = texture
				p.labelBytes += 16 << 20
				old[texture.ID()] = true
			case "resize":
				width, height = 900, 740
			case "close":
				if err := p.Close(); err != nil {
					t.Fatal(err)
				}
			}
			newImages := playgroundImages(p.Draw(width, height))
			retired := p.RetiredTextures()
			seen := make(map[uint64]bool, len(retired))
			for _, id := range retired {
				if seen[id] || !old[id] {
					t.Fatalf("unexpected or duplicated retired texture %d", id)
				}
				seen[id] = true
				if _, live := newImages[id]; live {
					t.Fatalf("new frame still references retired texture %d", id)
				}
			}
			if len(seen) != len(old) {
				t.Fatalf("retired %d/%d old textures", len(seen), len(old))
			}
			if len(p.RetiredTextures()) != 0 {
				t.Fatal("retirement queue did not drain")
			}
			if reason != "close" && (len(newImages) == 0 || len(oldImages) == 0 || len(p.labels) > 160 || p.labelBytes > 16<<20) {
				t.Fatal("cache did not rebuild within its ordinary frame budget")
			}
			if reason == "close" && (len(newImages) != 0 || p.Handle(experience.Event{Kind: experience.PointerDown})) {
				t.Fatal("closed playground still renders or handles input")
			}
		})
	}
}

func BenchmarkPlaygroundFrame(b *testing.B) {
	for _, moving := range []bool{false, true} {
		name := "static"
		if moving {
			name = "moving"
		}
		b.Run(name, func(b *testing.B) {
			p := testPlayground(b)
			p.Draw(884, 720)
			if moving {
				playgroundPointer(p, experience.PointerDown, 65, 145)
				playgroundPointer(p, experience.PointerMove, 645, 195)
				p.Draw(884, 720) // Warm both joined and standalone labels.
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if moving {
					playgroundPointer(p, experience.PointerMove, 635+float32(i%30), 195)
				}
				p.Update(time.Second / 60)
				frame := p.Draw(884, 720)
				if len(frame.Commands) == 0 {
					b.Fatal("empty frame")
				}
			}
		})
	}
}
