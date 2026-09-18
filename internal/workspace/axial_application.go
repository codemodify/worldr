package workspace

import (
	"fmt"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

const AxialApplicationKey = "native:axial-07"

// AxialApplicationState is the bounded, inert session record for the hosted
// AXIAL study. It contains presentation state only; restoring it never starts a
// process or a data producer.
type AxialApplicationState struct {
	Key      string  `json:"key"`
	Time     float64 `json:"time"`
	Yaw      float32 `json:"yaw"`
	Pitch    float32 `json:"pitch"`
	Zoom     float32 `json:"zoom"`
	Selected int     `json:"selected"`
	Playing  bool    `json:"playing,omitempty"`
	Exploded bool    `json:"exploded,omitempty"`
}

func (s AxialApplicationState) Validate() error {
	if s.Key != AxialApplicationKey || s.Selected < 0 || s.Selected >= len(components) ||
		math.IsNaN(s.Time) || math.IsInf(s.Time, 0) || s.Time < 0 || s.Time >= duration ||
		!finite32(s.Yaw) || !finite32(s.Pitch) || !finite32(s.Zoom) ||
		s.Pitch < -.95 || s.Pitch > .95 || s.Zoom < -.8 || s.Zoom > .8 {
		return fmt.Errorf("invalid hosted AXIAL state")
	}
	return nil
}

func finite32(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

type axialApplication struct {
	id                uint64
	model             model
	panel             *instrumentPanel
	spatial           experience.SpatialContent
	focused, dragging bool
	lastX, lastY      float32
	lastRefresh       time.Time
}

// AxialApplicationManager hosts the original engineering study through the
// same generic application/spatial-content contract as models and research.
// The standalone experience remains available for deterministic demos while
// this provider makes AXIAL an ordinary movable, groupable workspace tool.
type AxialApplicationManager struct {
	meshes                           [3]*scene.Mesh
	accent                           *scene.Mesh
	current                          *axialApplication
	next                             uint64
	surfaces                         []experience.ApplicationSurface
	retiredTextures, retiredGeometry []uint64
	closed                           bool
}

func NewAxialApplicationManager() (*AxialApplicationManager, error) {
	return new(AxialApplicationManager), nil
}

func (m *AxialApplicationManager) ensureGeometry() error {
	if m.meshes[0] != nil {
		return nil
	}
	var meshes [3]*scene.Mesh
	builders := []func() (*scene.Mesh, error){housingMesh, rotorMesh, shaftMesh}
	for i, build := range builders {
		mesh, err := build()
		if err != nil {
			return fmt.Errorf("build hosted AXIAL %s: %w", componentIDs[i], err)
		}
		meshes[i] = mesh
	}
	accent, err := housingAccentMesh()
	if err != nil {
		return fmt.Errorf("build hosted AXIAL accent: %w", err)
	}
	m.meshes, m.accent = meshes, accent
	return nil
}

func (m *AxialApplicationManager) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{{Kind: "axial", Title: "Open AXIAL / 07"}}
}

func (m *AxialApplicationManager) ApplicationLaunchAddsSurface(kind string) bool {
	return kind == "axial" && m.current == nil
}

func (m *AxialApplicationManager) LaunchApplication(kind string) (string, error) {
	if kind != "axial" || m.closed {
		return "", fmt.Errorf("AXIAL launcher is unavailable")
	}
	if m.current != nil {
		return AxialApplicationKey, nil
	}
	state := initialModel()
	state.focused, state.panelBehind = false, false
	return m.open(state)
}

func (m *AxialApplicationManager) Restore(state AxialApplicationState) (string, error) {
	if m.closed || m.current != nil {
		return "", fmt.Errorf("hosted AXIAL is unavailable")
	}
	if err := state.Validate(); err != nil {
		return "", err
	}
	value := initialModel()
	value.clock, value.yaw, value.pitch, value.zoom = state.Time, state.Yaw, state.Pitch, state.Zoom
	value.selected, value.playing, value.exploded = state.Selected, state.Playing, state.Exploded
	value.explosion = 0
	if value.exploded {
		value.explosion = 1
	}
	value.focused, value.panelBehind = false, false
	return m.open(value)
}

func (m *AxialApplicationManager) open(state model) (string, error) {
	if err := m.ensureGeometry(); err != nil {
		return "", err
	}
	panel, err := newInstrumentPanel()
	if err != nil {
		return "", err
	}
	m.next++
	app := &axialApplication{id: m.next, model: state, panel: panel, lastRefresh: time.Now()}
	m.current = app
	m.refresh(app)
	return AxialApplicationKey, nil
}

func (m *AxialApplicationManager) Poll() error {
	if m.current == nil || m.closed {
		return nil
	}
	now := time.Now()
	delta := now.Sub(m.current.lastRefresh)
	m.current.lastRefresh = now
	if delta < 0 {
		delta = 0
	}
	if delta > 50*time.Millisecond {
		delta = 50 * time.Millisecond
	}
	m.current.model.update(delta)
	m.refresh(m.current)
	return nil
}

func (m *AxialApplicationManager) refresh(app *axialApplication) {
	state := &app.model
	root := scene.Translate(0, .01, .13).
		Mul(scene.RotateY(state.yaw)).
		Mul(scene.RotateX(state.pitch)).
		Mul(scene.Scale(.13*float32(math.Exp(float64(state.zoom))), .13*float32(math.Exp(float64(state.zoom))), .13*float32(math.Exp(float64(state.zoom)))))
	rotation := float32(state.clock) * .6
	app.spatial.Objects = []experience.SpatialObject{
		{ID: 1, Node: scene.Node{Transform: root}},
		{ID: 2, Parent: 1, Node: scene.Node{Transform: scene.Translate(-state.explosion*1.65, 0, 0), Mesh: m.meshes[0], Color: axialApplicationColor(state, 0), WireColor: axialApplicationWire(state, 0), WireWidth: .7, Material: components[0].appearance, CastShadow: true, ReceiveShadow: true}},
		{ID: 3, Parent: 1, Node: scene.Node{Transform: scene.Translate(state.explosion*.2, 0, 0).Mul(scene.RotateX(rotation)), Mesh: m.meshes[1], Color: axialApplicationColor(state, 1), WireColor: axialApplicationWire(state, 1), WireWidth: .7, Material: components[1].appearance, CastShadow: true, ReceiveShadow: true}},
		{ID: 4, Parent: 1, Node: scene.Node{Transform: scene.Translate(state.explosion*1.15, 0, 0).Mul(scene.RotateX(rotation)), Mesh: m.meshes[2], Color: axialApplicationColor(state, 2), WireColor: axialApplicationWire(state, 2), WireWidth: .7, Material: components[2].appearance, CastShadow: true, ReceiveShadow: true}},
		{ID: 5, Parent: 2, Node: scene.Node{Mesh: m.accent, Color: scene.ColorHex(0x72dcfa, .88), Unlit: true, Unpickable: true, DepthReadOnly: true, Glow: [3]float32{.18, .56, .72}}},
		{ID: 6, Parent: 2, Node: scene.Node{Transform: scene.Scale(1.012, 1.012, 1.012), Mesh: m.meshes[0], Color: scene.ColorHex(0x72dcfa, .28), Unlit: true, Unpickable: true, Translucent: true, Material: render.Material{Hologram: .68, RimColor: [3]float32{.18, .78, 1}}}},
	}
	labelAt := root.TransformPoint(scene.Vec3{Y: 2.1})
	app.spatial.Labels = []experience.SpatialLabel{{Text: components[state.selected].name, Position: labelAt, Color: scene.ColorHex(0xbcefff, 1)}}
	app.panel.updateEmbedded(*state)
}

func axialApplicationColor(state *model, component int) scene.Color {
	color := scene.ColorHex(components[component].color, 1)
	if state.selected != component {
		color.R *= .63
		color.G *= .68
		color.B *= .74
	}
	return color
}

func axialApplicationWire(state *model, component int) scene.Color {
	if state.selected == component {
		return scene.ColorHex(0x86e8ff, .62)
	}
	return scene.Color{}
}

func (m *AxialApplicationManager) Surfaces() []experience.ApplicationSurface {
	m.surfaces = m.surfaces[:0]
	if m.current != nil && !m.closed {
		m.surfaces = append(m.surfaces, experience.ApplicationSurface{
			ID: m.current.id, Key: AxialApplicationKey, AppID: "worldr.axial",
			Title: "AXIAL / 07", Texture: m.current.panel.texture,
			Spatial: &m.current.spatial, FrameStyle: experience.FrameCinematic,
		})
	}
	return m.surfaces
}

func (m *AxialApplicationManager) Focus(id uint64) {
	if m.current == nil {
		return
	}
	m.current.focused = id == m.current.id
	if !m.current.focused {
		m.current.dragging = false
	}
}

func (m *AxialApplicationManager) Send(id uint64, event experience.Event) {
	app := m.current
	if app == nil || id != app.id {
		return
	}
	if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel {
		app.dragging = false
		return
	}
	if event.Kind == experience.KeyInput && event.Pressed && !event.Repeat && app.focused && event.Modifiers == 0 {
		switch event.Key {
		case experience.KeySpace:
			app.model.playing = !app.model.playing
		case experience.KeyE:
			app.model.exploded = !app.model.exploded
		case experience.KeyLeft:
			app.model.clock = math.Mod(app.model.clock+duration-.25, duration)
		case experience.KeyRight:
			app.model.clock = math.Mod(app.model.clock+.25, duration)
		case experience.Key1, experience.Key2, experience.Key3:
			app.model.selected = int(event.Key[0] - '1')
		default:
			return
		}
		m.refresh(app)
		return
	}
	if event.Kind == experience.PointerDown && event.Button == experience.ButtonPrimary {
		if event.SpatialObject >= 2 && event.SpatialObject <= 4 {
			app.model.selected = int(event.SpatialObject - 2)
			app.dragging, app.lastX, app.lastY = true, event.X, event.Y
		} else {
			x, y := event.X, event.Y
			if panelPlay.contains(x, y) {
				app.model.playing = !app.model.playing
			} else if panelDepth.contains(x, y) {
				app.model.exploded = !app.model.exploded
			} else if panelChart.contains(x, y) {
				app.model.clock = math.Max(0, math.Min(duration-.000001, float64((x-panelChart.x)/panelChart.w)*duration))
			}
		}
		m.refresh(app)
		return
	}
	if event.Kind == experience.PointerMove && app.dragging {
		app.model.yaw += (event.X - app.lastX) * .008
		app.model.pitch = max(float32(-.95), min(float32(.95), app.model.pitch+(event.Y-app.lastY)*.008))
		app.lastX, app.lastY = event.X, event.Y
		m.refresh(app)
		return
	}
	if event.Kind == experience.PointerUp {
		app.dragging = false
	}
	if event.Kind == experience.PointerScroll {
		app.model.zoom = max(float32(-.8), min(float32(.8), app.model.zoom-event.ScrollY*.012))
		m.refresh(app)
	}
}

func (m *AxialApplicationManager) Resize(uint64, int, int) {}

func (m *AxialApplicationManager) CloseApplication(id uint64) {
	if m.current == nil || m.current.id != id {
		return
	}
	m.retiredTextures = append(m.retiredTextures, m.current.panel.texture.ID())
	m.current.panel.close()
	m.current = nil
	m.surfaces = nil
	// AXIAL is a single-instance application, so no live surface can share its
	// immutable geometry after this point. Drop the CPU references and let the
	// host retire the matching GPU buffers. Reopening builds fresh resources;
	// an empty general workspace therefore does not retain the study merely
	// because it was opened once.
	m.retireGeometry()
}

func (m *AxialApplicationManager) retireGeometry() {
	for i, mesh := range m.meshes {
		if mesh != nil {
			m.retiredGeometry = append(m.retiredGeometry, mesh.Geometry().ID())
			m.meshes[i] = nil
		}
	}
	if m.accent != nil {
		m.retiredGeometry = append(m.retiredGeometry, m.accent.Geometry().ID())
		m.accent = nil
	}
}

func (m *AxialApplicationManager) Close() error {
	if m.closed {
		return nil
	}
	if m.current != nil {
		m.CloseApplication(m.current.id)
	}
	// This also covers a failed panel construction after geometry succeeded.
	m.retireGeometry()
	m.closed = true
	return nil
}

func (m *AxialApplicationManager) RetiredTextures() []uint64 {
	result := m.retiredTextures
	m.retiredTextures = nil
	return result
}

func (m *AxialApplicationManager) RetiredGeometryIDs() []uint64 {
	result := m.retiredGeometry
	m.retiredGeometry = nil
	return result
}

func (m *AxialApplicationManager) SessionStates() []AxialApplicationState {
	if m.current == nil {
		return nil
	}
	state := m.current.model
	return []AxialApplicationState{{
		Key: AxialApplicationKey, Time: state.clock, Yaw: state.yaw, Pitch: state.pitch,
		Zoom: state.zoom, Selected: state.selected, Playing: state.playing, Exploded: state.exploded,
	}}
}

var (
	_ experience.Applications               = (*AxialApplicationManager)(nil)
	_ experience.ApplicationPoller          = (*AxialApplicationManager)(nil)
	_ experience.ApplicationLauncher        = (*AxialApplicationManager)(nil)
	_ experience.ApplicationLaunchCatalog   = (*AxialApplicationManager)(nil)
	_ experience.ApplicationCloser          = (*AxialApplicationManager)(nil)
	_ experience.ApplicationProviderCloser  = (*AxialApplicationManager)(nil)
	_ experience.ApplicationTextureRetirer  = (*AxialApplicationManager)(nil)
	_ experience.ApplicationGeometryRetirer = (*AxialApplicationManager)(nil)
)
