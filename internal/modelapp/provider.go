package modelapp

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/scene"
	"github.com/codemodify/worldr/internal/textinput"
)

type loadResult struct {
	model *Model
	state SessionState
	err   error
}
type viewer struct {
	id                                                 uint64
	state                                              SessionState
	model                                              *Model
	renderer                                           *renderer
	input                                              *textinput.Translator
	spatial                                            experience.SpatialContent
	result                                             chan loadResult
	cancel                                             context.CancelFunc
	loading, dirty, focused, dragging, editing, closed bool
	mode, message                                      string
	downX, downY, lastX, lastY                         float32
	pending                                            *scene.Vec3
	annotationPoint                                    scene.Vec3
	note                                               *nativeui.Field
	controls                                           nativeui.Controller
	clipboard                                          nativeui.FieldClipboard
	componentTop                                       int
	textEpoch                                          uint64
	saveResult                                         chan error
	saving                                             bool
}

const MaxViewers = 8
const maxCollectionTriangles = 800000

type Manager struct {
	loader                           chan struct{}
	triangles                        int
	keymap                           experience.Event
	modifiers                        experience.Event
	slots                            [32]*viewer
	retiring                         []*viewer
	pasteTarget                      *viewer
	next                             uint64
	surfaces                         []experience.ApplicationSurface
	retiredTextures, retiredGeometry []uint64
	closed                           bool
}

func NewManager() *Manager { return &Manager{loader: make(chan struct{}, 1)} }
func (m *Manager) NextKey() (string, error) {
	m.collectRetiring(false)
	if m.count()+len(m.retiring) >= MaxViewers {
		return "", fmt.Errorf("model window limit reached (%d)", MaxViewers)
	}
	if m.closed {
		return "", fmt.Errorf("model manager is closed")
	}
	for i, v := range m.slots {
		if v == nil {
			return slotKey(i), nil
		}
	}
	return "", fmt.Errorf("model viewer limit reached")
}
func (m *Manager) OpenFile(file *os.File, name string) (string, error) {
	key, err := m.NextKey()
	if err != nil {
		return "", err
	}
	return m.open(file, name, key, nil)
}
func (m *Manager) Restore(state SessionState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	file, err := resourcepath.OpenFile(state.ResourcePath())
	if err != nil {
		return "", err
	}
	key, err := m.open(file, filepath.Base(state.ResourcePath()), state.Key, &state)
	if err != nil {
		file.Close()
	}
	return key, err
}
func (m *Manager) open(file *os.File, name, key string, saved *SessionState) (string, error) {
	if m.closed || file == nil || m.count()+len(m.retiring) >= MaxViewers {
		return "", fmt.Errorf("model viewer is unavailable")
	}
	slot, err := keySlot(key)
	if err != nil {
		return "", err
	}
	if m.slots[slot] != nil {
		return "", fmt.Errorf("model slot already open")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxModelBytes {
		return "", fmt.Errorf("model must be a regular file no larger than 32 MiB")
	}
	path, err := resourcepath.FromFile(file)
	if err != nil {
		return "", err
	}
	r, err := newRenderer(960, 600)
	if err != nil {
		return "", err
	}
	input, err := textinput.New()
	if err != nil {
		r.close()
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.next++
	v := &viewer{id: m.next, state: SessionState{Key: key, Source: path, Document: path + ".worldr-model.json", View: defaultView()}, renderer: r, input: input, note: nativeui.NewField(512), result: make(chan loadResult, 1), cancel: cancel, loading: true, dirty: true, mode: "inspect"}
	if strings.HasSuffix(strings.ToLower(name), ".worldr-model.json") {
		v.state.Source, v.state.Document, v.state.DocumentOnly = "", path, true
	}
	if saved != nil {
		v.state = *saved
		v.state.View = cloneView(saved.View)
	}
	if m.keymap.Kind == experience.KeymapChanged {
		input.Handle(m.keymap)
	}
	if m.modifiers.Kind == experience.KeyboardModifiers {
		input.Handle(m.modifiers)
	}
	m.slots[slot] = v
	initial := v.state
	if err := v.refresh(); err != nil {
		m.slots[slot] = nil
		cancel()
		input.Close()
		r.close()
		return "", err
	}
	// One parser/mesh builder at a time bounds transient memory and avoids a
	// burst of competing BVH builds when a large session is restored.
	if m.loader == nil {
		m.loader = make(chan struct{}, 1)
	}
	go func() {
		defer file.Close()
		select {
		case m.loader <- struct{}{}:
			defer func() { <-m.loader }()
		case <-ctx.Done():
			v.result <- loadResult{err: ctx.Err()}
			return
		}
		result := loadResult{state: initial}
		if initial.DocumentOnly {
			doc, err := readDocument(file)
			if err != nil {
				result.err = err
				v.result <- result
				return
			}
			source, err := resourcepath.OpenFile(doc.Source)
			if err != nil {
				result.err = err
				v.result <- result
				return
			}
			defer source.Close()
			result.state.Source, result.state.Document, result.state.View = doc.Source, path, doc.View
			result.state.DocumentOnly = false
			if err := result.state.Validate(); err != nil {
				result.err = err
				v.result <- result
				return
			}
			result.model, result.err = readModel(ctx, source, doc.Source)
		} else {
			result.model, result.err = readModel(ctx, file, name)
		}
		v.result <- result
	}()
	return key, nil
}
func (m *Manager) Poll() error {
	m.collectRetiring(false)
	for _, v := range m.slots {
		if v == nil {
			continue
		}
		if v.loading {
			select {
			case r := <-v.result:
				v.loading = false
				if r.err == nil && m.triangles+r.model.Triangles > maxCollectionTriangles {
					r.err = fmt.Errorf("combined model budget is 800000 triangles; close a model first")
				}
				if r.err != nil {
					v.message = "Could not open model: " + r.err.Error()
				} else {
					v.model, v.state = r.model, r.state
					m.triangles += r.model.Triangles
					if v.state.View.Selected >= len(v.model.Components) {
						v.state.View.Selected = 0
					}
					v.message = "Model loaded"
				}
				v.dirty = true
			default:
			}
		}
		if v.saving {
			select {
			case err := <-v.saveResult:
				v.saving = false
				if err != nil {
					v.message = "Save failed: " + err.Error()
				} else {
					v.message = "Saved " + filepath.Base(v.state.Document)
				}
				v.dirty = true
			default:
			}
		}
		if v.dirty {
			if err := v.refresh(); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) Surfaces() []experience.ApplicationSurface {
	m.surfaces = m.surfaces[:0]
	for _, v := range m.slots {
		if v != nil {
			m.surfaces = append(m.surfaces, experience.ApplicationSurface{ID: v.id, Key: v.state.Key, AppID: "worldr.model-inspector", Title: "Inspect / " + filepath.Base(v.state.ResourcePath()), Texture: v.renderer.texture, Spatial: &v.spatial, FrameStyle: experience.FrameCinematic})
		}
	}
	return m.surfaces
}
func (m *Manager) Focus(id uint64) {
	for _, v := range m.slots {
		if v != nil {
			if v.focused != (v.id == id) {
				v.textEpoch++
			}
			v.focused = v.id == id
			v.bindClipboard()
			if !v.focused {
				v.dragging = false
				v.controls.Blur()
				v.note.Handle(experience.Event{Kind: experience.KeyboardCancel}, "")
				v.input.Handle(experience.Event{Kind: experience.KeyboardCancel})
			}
		}
	}
}

func (m *Manager) Seat(e experience.Event) {
	switch e.Kind {
	case experience.KeymapChanged:
		m.keymap = e
	case experience.KeyboardModifiers:
		m.modifiers = e
	case experience.KeyboardCancel:
		m.modifiers = experience.Event{}
	default:
		return
	}
	for _, v := range m.slots {
		if v != nil {
			v.input.Handle(e)
		}
	}
}
func (m *Manager) Send(id uint64, e experience.Event) {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			if e.Kind == experience.TextCommit || e.Kind == experience.TextPreedit {
				state := m.TextInput(id)
				if !state.Enabled || state.ContextID != e.TextContext {
					return
				}
			}
			v.handle(e)
			return
		}
	}
}
func (m *Manager) Resize(id uint64, width, height int) {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			if width < 640 || height < 360 || width > 4096 || height > 4096 {
				return
			}
			if err := v.renderer.resize(width, height); err == nil {
				v.dirty = true
			}
			return
		}
	}
}
func (m *Manager) closeViewer(slot int) {
	v := m.slots[slot]
	if v == nil {
		return
	}
	v.cancel()
	if v.model != nil {
		m.triangles -= v.model.Triangles
	}
	// Closing a view cannot wait for a BVH build or filesystem sync. Workers
	// own immutable input and buffered results; drain them from later polls.
	if v.loading || v.saving {
		m.retiring = append(m.retiring, v)
	}
	m.retiredTextures = append(m.retiredTextures, v.renderer.texture.ID())
	if v.model != nil {
		for _, c := range v.model.Components {
			m.retiredGeometry = append(m.retiredGeometry, c.Mesh.Geometry().ID())
		}
	}
	v.input.Close()
	v.renderer.close()
	v.closed = true
	m.slots[slot] = nil
}
func (m *Manager) collectRetiring(wait bool) {
	remaining := m.retiring[:0]
	for _, v := range m.retiring {
		if v.loading {
			if wait {
				<-v.result
				v.loading = false
			} else {
				select {
				case <-v.result:
					v.loading = false
				default:
				}
			}
		}
		if v.saving {
			if wait {
				<-v.saveResult
				v.saving = false
			} else {
				select {
				case <-v.saveResult:
					v.saving = false
				default:
				}
			}
		}
		if v.loading || v.saving {
			remaining = append(remaining, v)
		}
	}
	clear(m.retiring[len(remaining):])
	m.retiring = remaining
}
func (m *Manager) CloseApplication(id uint64) {
	for i, v := range m.slots {
		if v != nil && v.id == id {
			m.closeViewer(i)
			return
		}
	}
}
func (m *Manager) Close() error {
	if !m.closed {
		m.closed = true
		for i := range m.slots {
			m.closeViewer(i)
		}
		m.collectRetiring(true)
	}
	return nil
}
func (m *Manager) RetiredTextures() []uint64 {
	ids := m.retiredTextures
	m.retiredTextures = nil
	return ids
}
func (m *Manager) RetiredGeometryIDs() []uint64 {
	ids := m.retiredGeometry
	m.retiredGeometry = nil
	return ids
}
func (m *Manager) SessionStates() []SessionState {
	var states []SessionState
	for _, v := range m.slots {
		if v != nil && v.state.Validate() == nil {
			s := v.state
			s.View = cloneView(s.View)
			states = append(states, s)
		}
	}
	return states
}
func (v *viewer) transform() scene.Mat4 {
	if v.model == nil {
		return scene.Identity()
	}
	center := v.model.Minimum.Add(v.model.Maximum).Mul(.5)
	size := v.model.Maximum.Sub(v.model.Minimum).Length()
	scale := .35 / size * v.state.View.Zoom
	return scene.Translate(.06, 0, .025+.175*v.state.View.Zoom).Mul(scene.RotateY(v.state.View.Yaw)).Mul(scene.RotateX(v.state.View.Pitch)).Mul(scene.Scale(scale, scale, scale)).Mul(scene.Translate(-center.X, -center.Y, -center.Z))
}
func (v *viewer) refresh() error {
	v.spatial.Objects = v.spatial.Objects[:0]
	v.spatial.Labels = v.spatial.Labels[:0]
	if v.model != nil {
		transform := v.transform()
		v.spatial.Objects = append(v.spatial.Objects, experience.SpatialObject{ID: 1, Node: scene.Node{Transform: transform}})
		for i, c := range v.model.Components {
			color := scene.ColorHex(0x4d738b, 1)
			wire := scene.Color{}
			if i == v.state.View.Selected {
				color = scene.ColorHex(0xb6cad4, 1)
				wire = scene.ColorHex(0x61dfff, .65)
			}
			v.spatial.Objects = append(v.spatial.Objects, experience.SpatialObject{ID: uint64(i + 2), Parent: 1, Node: scene.Node{Mesh: c.Mesh, Color: color, WireColor: wire, WireWidth: .7, CastShadow: true, ReceiveShadow: true, Material: render.Material{Specular: .45, Roughness: .36, Metallic: .65, RimStrength: .15, RimColor: [3]float32{.38, .82, 1}}}})
		}
		for _, a := range v.state.View.Annotations {
			v.spatial.Labels = append(v.spatial.Labels, experience.SpatialLabel{Text: a.Text, Position: transform.TransformPoint(a.Point), Color: scene.ColorHex(0xffd18b, 1)})
		}
		for _, m := range v.state.View.Measurements {
			v.spatial.Labels = append(v.spatial.Labels, experience.SpatialLabel{Text: fmt.Sprintf("%.4g %s", m.Distance, v.state.View.Units), Position: transform.TransformPoint(m.A.Add(m.B).Mul(.5)), Color: scene.ColorHex(0x8cf3ff, 1)})
		}
	}
	v.updateControls()
	v.dirty = false
	return v.renderer.draw(v)
}
func (v *viewer) save() {
	if v.model == nil || v.saving {
		return
	}
	v.saving = true
	v.saveResult = make(chan error, 1)
	state := v.state
	state.View = cloneView(state.View)
	v.message = "Saving tool document…"
	go func() { v.saveResult <- writeDocument(state) }()
	v.dirty = true
}
func (v *viewer) action(kind string) {
	switch kind {
	case "save":
		v.save()
	case "reset":
		v.state.View.Yaw, v.state.View.Pitch, v.state.View.Zoom = -.5, .5, 1
	case "units":
		units := []string{"units", "mm", "cm", "m", "in"}
		for i, u := range units {
			if u == v.state.View.Units {
				v.state.View.Units = units[(i+1)%len(units)]
				break
			}
		}
	case "inspect", "measure", "annotate":
		v.mode = kind
		v.pending = nil
	case "clear":
		v.state.View.Measurements = nil
		v.state.View.Annotations = nil
		v.pending = nil
	}
	v.dirty = true
}
func (v *viewer) handle(e experience.Event) {
	text := v.input.Handle(e)
	v.bindClipboard()
	if v.clipboard.Handle(e) {
		v.dirty = true
		return
	}
	if v.editing && v.focused && (e.Kind == experience.TextCommit || e.Kind == experience.TextPreedit) {
		v.note.Handle(e, e.Text)
		v.dirty = true
		return
	}
	if !v.editing && (e.SpatialObject == 0 || e.Kind == experience.KeyInput || e.Kind == experience.KeyboardCancel || e.Kind == experience.PointerCancel) {
		action := v.controls.Handle(e)
		if action.Consumed {
			if action.Activated {
				v.action(action.ID)
			}
			v.dirty = true
			return
		}
	} else if e.SpatialObject == 0 && e.Kind == experience.PointerDown && image.Pt(int(e.X), int(e.Y)).In(v.renderer.bounds(204, 550, 730, 38)) {
		_ = v.renderer.painter.PlaceCaret(v.note, v.renderer.bounds(204, 550, 730, 38), image.Pt(int(e.X), int(e.Y)), e.Modifiers.Has(experience.ModShift))
		v.dirty = true
		return
	}
	if e.Kind == experience.KeyboardCancel || e.Kind == experience.PointerCancel {
		v.dragging = false
		return
	}
	if e.Kind == experience.KeyInput && e.Pressed && v.focused {
		if v.editing {
			switch e.Keycode {
			case 1:
				v.editing = false
			case 28:
				if strings.TrimSpace(v.note.Text()) != "" && len(v.state.View.Annotations) < 128 {
					v.state.View.Annotations = append(v.state.View.Annotations, Annotation{v.annotationPoint, v.note.Text()})
					v.editing = false
				}
			default:
				v.note.Handle(e, text)
			}
			v.dirty = true
			return
		}
		if e.Modifiers == experience.ModControl && e.Keycode == 31 {
			v.save()
			return
		}
		if e.Modifiers != 0 {
			return
		}
		switch e.Keycode {
		case 50:
			v.action("measure")
		case 30:
			v.action("annotate")
		case 23:
			v.action("inspect")
		case 19:
			v.action("reset")
		case 22:
			v.action("units")
		case 104, 109:
			if v.model != nil {
				delta := 1
				if e.Keycode == 104 {
					delta = -1
				}
				v.state.View.Selected = max(0, min(len(v.model.Components)-1, v.state.View.Selected+delta))
				v.revealComponent()
			}
		case 105:
			v.state.View.Yaw -= .12
		case 106:
			v.state.View.Yaw += .12
		case 103:
			v.state.View.Pitch -= .12
		case 108:
			v.state.View.Pitch += .12
		}
		v.dirty = true
		return
	}
	if e.Kind == experience.PointerDown && e.Button == experience.ButtonPrimary {
		width, height := v.renderer.texture.Size()
		x, y := e.X*960/float32(width), e.Y*600/float32(height)
		if v.model == nil {
			return
		}
		if e.SpatialObject == 0 && x >= 19 && x < 180 && y >= 135 && y < 473 {
			index := v.componentTop + int((y-135)/26)
			if index >= 0 && index < len(v.model.Components) {
				v.state.View.Selected = index
				v.dirty = true
				return
			}
		}
		if e.SpatialObject >= 2 && int(e.SpatialObject-2) < len(v.model.Components) {
			v.state.View.Selected = int(e.SpatialObject - 2)
			v.revealComponent()
			p := scene.Vec3{X: e.SpatialPoint[0], Y: e.SpatialPoint[1], Z: e.SpatialPoint[2]}
			inverse, ok := v.transform().Inverse()
			if ok {
				p = inverse.TransformPoint(p)
				switch v.mode {
				case "measure":
					if v.pending == nil {
						v.pending = &p
						v.message = "Choose the second point"
					} else if len(v.state.View.Measurements) < 128 {
						a := *v.pending
						v.state.View.Measurements = append(v.state.View.Measurements, Measurement{a, p, p.Sub(a).Length()})
						v.pending = nil
						v.message = "Measurement added"
					}
					v.dirty = true
					return
				case "annotate":
					v.annotationPoint = p
					v.editing = true
					v.note.Set("")
					v.textEpoch++
					v.dirty = true
					return
				}
			}
		}
		v.dragging = true
		v.downX, v.downY, v.lastX, v.lastY = e.X, e.Y, e.X, e.Y
		v.dirty = true
	}
	if e.Kind == experience.PointerMove && v.dragging {
		dx, dy := e.X-v.lastX, e.Y-v.lastY
		if math.Abs(float64(e.X-v.downX))+math.Abs(float64(e.Y-v.downY)) > 3 {
			v.state.View.Yaw += dx * .008
			v.state.View.Pitch += dy * .008
			v.dirty = true
		}
		v.lastX, v.lastY = e.X, e.Y
	}
	if e.Kind == experience.PointerUp {
		v.dragging = false
	}
	if e.Kind == experience.PointerScroll && v.model != nil {
		if e.SpatialObject == 0 && e.X < 180*v.renderer.scaleX && e.Y >= 105*v.renderer.scaleY && e.Y < 492*v.renderer.scaleY {
			delta := 1
			if e.ScrollY < 0 {
				delta = -1
			}
			v.componentTop = max(0, min(max(0, len(v.model.Components)-13), v.componentTop+delta))
			v.dirty = true
			return
		}
		v.state.View.Zoom = max(.2, min(2, v.state.View.Zoom*float32(math.Exp(float64(-e.ScrollY*.025)))))
		v.dirty = true
	}
}

func (v *viewer) revealComponent() {
	v.componentTop = max(0, min(v.componentTop, v.state.View.Selected))
	if v.state.View.Selected >= v.componentTop+13 {
		v.componentTop = v.state.View.Selected - 12
	}
}
func (v *viewer) updateControls() {
	names := []string{"Inspect", "Measure", "Annotate", "Save", "Reset", "Units", "Clear marks"}
	ids := []string{"inspect", "measure", "annotate", "save", "reset", "units", "clear"}
	nodes := make([]nativeui.Node, 0, 8)
	for i, name := range names {
		nodes = append(nodes, nativeui.Node{ID: ids[i], Role: nativeui.RoleButton, Label: name, Bounds: v.renderer.bounds(18+i*127, 48, 118, 34), Selected: ids[i] == v.mode, Disabled: v.model == nil})
	}
	if v.editing {
		nodes = append(nodes, nativeui.Node{ID: "note", Role: nativeui.RoleTextField, Label: "Annotation", Value: v.note.Text(), Bounds: v.renderer.bounds(204, 550, 730, 38)})
	}
	v.controls.SetNodes(nodes)
}
func (m *Manager) Semantics(id uint64) nativeui.SemanticTree {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			return v.controls.Semantics()
		}
	}
	return nativeui.SemanticTree{}
}
func (m *Manager) TextInput(id uint64) experience.TextInputState {
	for _, v := range m.slots {
		if v != nil && v.id == id && v.focused && v.editing {
			return v.note.TextInput(fmt.Sprintf("model:%d:note:%d", v.id, v.textEpoch), v.renderer.bounds(204, 550, 730, 38))
		}
	}
	return experience.TextInputState{}
}

func (m *Manager) count() int {
	n := 0
	for _, v := range m.slots {
		if v != nil {
			n++
		}
	}
	return n
}
