package researchapp

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/scene"
)

type loadResult struct {
	dataset        *Dataset
	modified, size int64
	err            error
}

type viewer struct {
	id                                      uint64
	state                                   SessionState
	dataset                                 *Dataset
	renderer                                *renderer
	spatial                                 experience.SpatialContent
	controls                                nativeui.Controller
	result                                  chan loadResult
	cancel                                  context.CancelFunc
	nextRefresh                             time.Time
	modified, size                          int64
	loading, dirty, focused, dragging, dead bool
	forceRefresh                            bool
	lastX, lastY                            float32
	message                                 string
}

type Manager struct {
	loader                           chan struct{}
	slots                            [MaxDashboards]*viewer
	retiring                         []*viewer
	pointMesh                        *scene.Mesh
	next                             uint64
	surfaces                         []experience.ApplicationSurface
	retiredTextures, retiredGeometry []uint64
	closed                           bool
	now                              func() time.Time
}

var _ experience.Applications = (*Manager)(nil)
var _ experience.ApplicationPoller = (*Manager)(nil)
var _ experience.ApplicationProviderCloser = (*Manager)(nil)
var _ experience.ApplicationCloser = (*Manager)(nil)
var _ experience.ApplicationTextureRetirer = (*Manager)(nil)
var _ experience.ApplicationGeometryRetirer = (*Manager)(nil)

func NewManager() *Manager {
	return &Manager{loader: make(chan struct{}, 1), now: time.Now}
}

func (m *Manager) count() int {
	count := 0
	for _, v := range m.slots {
		if v != nil {
			count++
		}
	}
	return count
}

func (m *Manager) NextKey() (string, error) {
	m.collectRetiring(false)
	if m.closed {
		return "", fmt.Errorf("research workbench is closed")
	}
	if m.count()+len(m.retiring) >= MaxDashboards {
		return "", fmt.Errorf("research dashboard limit reached (%d)", MaxDashboards)
	}
	for slot, v := range m.slots {
		if v == nil {
			return slotKey(slot), nil
		}
	}
	return "", fmt.Errorf("research dashboard slots are exhausted")
}

func (m *Manager) ensureMesh() error {
	if m.pointMesh != nil {
		return nil
	}
	vertices := []scene.Vec3{{X: -.5, Y: -.5, Z: -.5}, {X: .5, Y: -.5, Z: -.5}, {X: .5, Y: .5, Z: -.5}, {X: -.5, Y: .5, Z: -.5}, {X: -.5, Y: -.5, Z: .5}, {X: .5, Y: -.5, Z: .5}, {X: .5, Y: .5, Z: .5}, {X: -.5, Y: .5, Z: .5}}
	indices := []uint32{0, 2, 1, 0, 3, 2, 4, 5, 6, 4, 6, 7, 0, 1, 5, 0, 5, 4, 3, 7, 6, 3, 6, 2, 0, 4, 7, 0, 7, 3, 1, 2, 6, 1, 6, 5}
	mesh, err := scene.NewMesh(vertices, indices, nil)
	if err != nil {
		return err
	}
	m.pointMesh = mesh
	return nil
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
	file, err := resourcepath.OpenFile(state.Source)
	if err != nil {
		return "", err
	}
	key, err := m.open(file, filepath.Base(state.Source), state.Key, &state)
	if err != nil {
		file.Close()
	}
	return key, err
}

func (m *Manager) open(file *os.File, name, key string, saved *SessionState) (string, error) {
	if m.closed || file == nil {
		return "", fmt.Errorf("research workbench is unavailable")
	}
	slot, err := keySlot(key)
	if err != nil {
		return "", err
	}
	if m.slots[slot] != nil {
		return "", fmt.Errorf("research dashboard slot is already open")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxDatasetBytes {
		return "", fmt.Errorf("dataset must be a regular file between 1 byte and 16 MiB")
	}
	path, err := resourcepath.FromFile(file)
	if err != nil {
		return "", err
	}
	if err := m.ensureMesh(); err != nil {
		return "", err
	}
	r, err := newRenderer(baseWidth, baseHeight)
	if err != nil {
		return "", err
	}
	state := SessionState{Key: key, Source: path, View: defaultView()}
	if saved != nil {
		state = *saved
	}
	m.next++
	ctx, cancel := context.WithCancel(context.Background())
	v := &viewer{id: m.next, state: state, renderer: r, result: make(chan loadResult, 1), cancel: cancel, loading: true, dirty: true, nextRefresh: m.now().Add(2 * time.Second), message: "Loading " + filepath.Base(name)}
	m.slots[slot] = v
	if err := v.refresh(m.pointMesh); err != nil {
		m.slots[slot] = nil
		cancel()
		r.close()
		return "", err
	}
	// Publish a complete loading surface before transferring file ownership to
	// the worker. If the initial texture cannot be built, the caller still owns
	// the descriptor and no background parser can race its cleanup.
	m.load(ctx, v, file, name, info)
	return key, nil
}

func (m *Manager) load(ctx context.Context, v *viewer, file *os.File, name string, info os.FileInfo) {
	go func() {
		defer file.Close()
		select {
		case m.loader <- struct{}{}:
			defer func() { <-m.loader }()
		case <-ctx.Done():
			v.result <- loadResult{err: ctx.Err()}
			return
		}
		result := loadResult{modified: info.ModTime().UnixNano(), size: info.Size()}
		result.dataset, result.err = readDataset(file, name)
		// Always complete the buffered result channel. Close waits on this
		// handoff so a canceled parse cannot strand provider shutdown.
		v.result <- result
	}()
}

func chooseAxes(view *ViewState, d *Dataset) {
	numeric := d.NumericColumns()
	if len(numeric) == 0 {
		return
	}
	valid := func(column int) bool { return column >= 0 && column < len(d.Numeric) && d.Numeric[column] }
	if !valid(view.X) {
		view.X = numeric[0]
	}
	if !valid(view.Y) {
		view.Y = numeric[min(1, len(numeric)-1)]
	}
	if !valid(view.Z) {
		view.Z = numeric[min(2, len(numeric)-1)]
	}
	view.Selected = max(0, min(len(d.Rows)-1, view.Selected))
	if view.Zoom < .2 || view.Zoom > 3 {
		view.Zoom = 1
	}
	if view.Mode != "line" && view.Mode != "scatter" {
		view.Mode = "line"
	}
}

func (m *Manager) Poll() error {
	m.collectRetiring(false)
	now := m.now()
	for _, v := range m.slots {
		if v == nil {
			continue
		}
		if v.loading {
			select {
			case result := <-v.result:
				v.loading = false
				if result.err != nil {
					if result.err != context.Canceled {
						v.message = "Dataset load failed: " + result.err.Error()
					}
				} else {
					v.dataset, v.modified, v.size = result.dataset, result.modified, result.size
					chooseAxes(&v.state.View, v.dataset)
					v.message = fmt.Sprintf("Live dataset ready · %d observations", len(v.dataset.Rows))
				}
				v.dirty = true
			default:
			}
		}
		if !v.loading && !now.Before(v.nextRefresh) {
			v.nextRefresh = now.Add(2 * time.Second)
			file, err := resourcepath.OpenFile(v.state.Source)
			if err == nil {
				info, statErr := file.Stat()
				if statErr != nil {
					err = statErr
				}
				if err == nil && (v.forceRefresh || info.ModTime().UnixNano() != v.modified || info.Size() != v.size) {
					ctx, cancel := context.WithCancel(context.Background())
					v.cancel()
					v.cancel, v.loading, v.message = cancel, true, "Dataset changed · refreshing"
					v.forceRefresh = false
					m.load(ctx, v, file, filepath.Base(v.state.Source), info)
					file = nil
				}
				if file != nil {
					file.Close()
				}
			}
			if err != nil {
				v.message = "Live refresh unavailable: " + err.Error()
				v.dirty = true
			}
		}
		if v.dirty {
			if err := v.refresh(m.pointMesh); err != nil {
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
			m.surfaces = append(m.surfaces, experience.ApplicationSurface{ID: v.id, Key: v.state.Key, AppID: "worldr.research-workbench", Title: "Research / " + filepath.Base(v.state.Source), Texture: v.renderer.texture, Spatial: &v.spatial, FrameStyle: experience.FrameCinematic})
		}
	}
	return m.surfaces
}

func (m *Manager) Focus(id uint64) {
	for _, v := range m.slots {
		if v == nil {
			continue
		}
		focused := v.id == id
		if focused != v.focused {
			v.focused, v.dirty = focused, true
		}
		if !focused {
			v.controls.Blur()
			v.dragging = false
		}
	}
}

func (m *Manager) Send(id uint64, event experience.Event) {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			v.handle(event)
			return
		}
	}
}

func (m *Manager) Resize(id uint64, width, height int) {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			if err := v.renderer.resize(width, height); err != nil {
				v.message = err.Error()
			}
			v.dirty = true
			return
		}
	}
}

func (m *Manager) Seat(event experience.Event) {
	if event.Kind != experience.KeyboardCancel && event.Kind != experience.PointerCancel {
		return
	}
	for _, v := range m.slots {
		if v != nil {
			v.dragging = false
			v.controls.Handle(event)
		}
	}
}

func (m *Manager) closeViewer(slot int) {
	v := m.slots[slot]
	if v == nil {
		return
	}
	v.cancel()
	if v.loading {
		v.dead = true
		m.retiring = append(m.retiring, v)
	}
	m.retiredTextures = append(m.retiredTextures, v.renderer.texture.ID())
	v.renderer.close()
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
		if v.loading {
			remaining = append(remaining, v)
		}
	}
	m.retiring = remaining
}

func (m *Manager) CloseApplication(id uint64) {
	for slot, v := range m.slots {
		if v != nil && v.id == id {
			m.closeViewer(slot)
			return
		}
	}
}

func (m *Manager) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	for slot := range m.slots {
		m.closeViewer(slot)
	}
	m.collectRetiring(true)
	if m.pointMesh != nil {
		m.retiredGeometry = append(m.retiredGeometry, m.pointMesh.Geometry().ID())
		m.pointMesh = nil
	}
	return nil
}

func (m *Manager) RetiredTextures() []uint64 {
	result := m.retiredTextures
	m.retiredTextures = nil
	return result
}

func (m *Manager) RetiredGeometryIDs() []uint64 {
	result := m.retiredGeometry
	m.retiredGeometry = nil
	return result
}

func (m *Manager) SessionStates() []SessionState {
	var result []SessionState
	for _, v := range m.slots {
		if v != nil && v.state.Validate() == nil {
			result = append(result, v.state)
		}
	}
	return result
}

func (m *Manager) Semantics(id uint64) nativeui.SemanticTree {
	for _, v := range m.slots {
		if v != nil && v.id == id {
			return v.controls.Semantics()
		}
	}
	return nativeui.SemanticTree{}
}

func (v *viewer) updateControls() {
	labels := []string{"X axis", "Y axis", "Z axis", "Line / Points", "Reload", "Reset view"}
	ids := []string{"x", "y", "z", "mode", "reload", "reset"}
	nodes := make([]nativeui.Node, len(ids), len(ids)+2)
	for i := range ids {
		nodes[i] = nativeui.Node{ID: ids[i], Role: nativeui.RoleButton, Label: labels[i], Bounds: v.renderer.bounds(18+i*180, 55, 168, 38), Disabled: v.dataset == nil}
	}
	if v.dataset != nil {
		d := v.dataset
		nodes = append(nodes,
			nativeui.Node{ID: "dataset-summary", Role: nativeui.RoleLabel, Label: "Dataset summary", Value: fmt.Sprintf("%d observations, %d columns; X %s, Y %s, Z %s", len(d.Rows), len(d.Columns), d.Columns[v.state.View.X], d.Columns[v.state.View.Y], d.Columns[v.state.View.Z]), Bounds: v.renderer.bounds(30, 510, 1060, 42)},
			nativeui.Node{ID: "selected-observation", Role: nativeui.RoleLabel, Label: fmt.Sprintf("Selected observation %d", v.state.View.Selected+1), Value: strings.Join(d.Rows[v.state.View.Selected], ", "), Bounds: v.renderer.bounds(24, 128, 252, 351)},
		)
	}
	v.controls.SetNodes(nodes)
}

func (v *viewer) cycleAxis(axis *int) {
	numeric := v.dataset.NumericColumns()
	for index, column := range numeric {
		if column == *axis {
			*axis = numeric[(index+1)%len(numeric)]
			return
		}
	}
	*axis = numeric[0]
}

func (v *viewer) action(id string) {
	if v.dataset == nil {
		return
	}
	switch id {
	case "x":
		v.cycleAxis(&v.state.View.X)
	case "y":
		v.cycleAxis(&v.state.View.Y)
	case "z":
		v.cycleAxis(&v.state.View.Z)
	case "mode":
		if v.state.View.Mode == "line" {
			v.state.View.Mode = "scatter"
		} else {
			v.state.View.Mode = "line"
		}
	case "reload":
		v.nextRefresh = time.Time{}
		v.forceRefresh = true
		v.message = "Checking source for changes"
	case "reset":
		v.state.View.Yaw, v.state.View.Pitch, v.state.View.Zoom = -.45, .38, 1
	}
	v.dirty = true
}

func (v *viewer) handle(event experience.Event) {
	if event.SpatialObject == 0 {
		action := v.controls.Handle(event)
		if action.Consumed {
			if action.Activated {
				v.action(action.ID)
			}
			v.dirty = true
			return
		}
	}
	if event.Kind == experience.KeyboardCancel || event.Kind == experience.PointerCancel {
		v.dragging = false
		return
	}
	if event.Kind == experience.KeyInput && event.Pressed && v.focused && v.dataset != nil && event.Modifiers == 0 {
		switch event.Keycode {
		case 103:
			v.state.View.Selected = max(0, v.state.View.Selected-1)
		case 108:
			v.state.View.Selected = min(len(v.dataset.Rows)-1, v.state.View.Selected+1)
		case 105:
			v.state.View.Yaw -= .12
		case 106:
			v.state.View.Yaw += .12
		case 19:
			v.action("reset")
		default:
			return
		}
		v.dirty = true
		return
	}
	if event.Kind == experience.PointerDown && event.Button == experience.ButtonPrimary && v.dataset != nil {
		startDrag := true
		if event.SpatialObject >= 2 {
			row := int(event.SpatialObject - 2)
			if row >= 0 && row < len(v.dataset.Rows) {
				v.state.View.Selected = row
				v.message = fmt.Sprintf("Observation %d selected in spatial view", row+1)
			}
		} else {
			width, height := v.renderer.texture.Size()
			x, y := event.X*baseWidth/float32(width), event.Y*baseHeight/float32(height)
			if x >= 24 && x < 276 && y >= 128 && y < 479 {
				startDrag = false
				start := max(0, min(len(v.dataset.Rows)-1, v.state.View.Selected-6))
				row := start + int((y-128)/27)
				if row >= 0 && row < len(v.dataset.Rows) {
					v.state.View.Selected = row
					v.message = fmt.Sprintf("Observation %d selected in table", row+1)
				}
			}
		}
		v.dragging, v.lastX, v.lastY, v.dirty = startDrag, event.X, event.Y, true
	}
	if event.Kind == experience.PointerMove && v.dragging {
		v.state.View.Yaw += (event.X - v.lastX) * .007
		v.state.View.Pitch = max(-1.2, min(1.2, v.state.View.Pitch+(event.Y-v.lastY)*.007))
		v.lastX, v.lastY, v.dirty = event.X, event.Y, true
	}
	if event.Kind == experience.PointerUp {
		v.dragging = false
	}
	if event.Kind == experience.PointerScroll && v.dataset != nil {
		v.state.View.Zoom = max(.2, min(3, v.state.View.Zoom*float32(math.Exp(float64(-event.ScrollY*.025)))))
		v.dirty = true
	}
}

func sampleRows(d *Dataset, view ViewState) []int {
	if d == nil || len(d.Rows) == 0 {
		return nil
	}
	const limit = 256
	step := max(1, (len(d.Rows)+limit-1)/limit)
	rows := make([]int, 0, min(limit+1, len(d.Rows)))
	seenSelected := false
	for row := 0; row < len(d.Rows) && len(rows) < limit; row += step {
		if _, ok := chartValue(d, view.X, row); !ok {
			continue
		}
		if _, ok := chartValue(d, view.Y, row); !ok {
			continue
		}
		if _, ok := chartValue(d, view.Z, row); !ok {
			continue
		}
		rows = append(rows, row)
		seenSelected = seenSelected || row == view.Selected
	}
	if !seenSelected {
		if _, x := chartValue(d, view.X, view.Selected); x {
			if _, y := chartValue(d, view.Y, view.Selected); y {
				if _, z := chartValue(d, view.Z, view.Selected); z {
					rows = append(rows, view.Selected)
				}
			}
		}
	}
	return rows
}

func (v *viewer) refresh(mesh *scene.Mesh) error {
	v.spatial.Objects = v.spatial.Objects[:0]
	v.spatial.Labels = v.spatial.Labels[:0]
	if v.dataset != nil && mesh != nil {
		view, d := v.state.View, v.dataset
		root := scene.Translate(.10, .01, .10).Mul(scene.RotateY(view.Yaw)).Mul(scene.RotateX(view.Pitch)).Mul(scene.Scale(view.Zoom, view.Zoom, view.Zoom))
		v.spatial.Objects = append(v.spatial.Objects, experience.SpatialObject{ID: 1, Node: scene.Node{Transform: root}})
		axes := []struct {
			id       uint64
			position scene.Vec3
			scale    scene.Vec3
			color    uint32
			name     string
			labelAt  scene.Vec3
		}{
			{1 << 40, scene.Vec3{}, scene.Vec3{X: .70, Y: .003, Z: .003}, 0x52d5e5, "X / " + d.Columns[view.X], scene.Vec3{X: .38}},
			{1<<40 + 1, scene.Vec3{}, scene.Vec3{X: .003, Y: .54, Z: .003}, 0xffbd69, "Y / " + d.Columns[view.Y], scene.Vec3{Y: .30}},
			{1<<40 + 2, scene.Vec3{}, scene.Vec3{X: .003, Y: .003, Z: .40}, 0x668fea, "Z / " + d.Columns[view.Z], scene.Vec3{Z: .23}},
		}
		for _, axis := range axes {
			node := scene.Node{Transform: scene.Translate(axis.position.X, axis.position.Y, axis.position.Z).Mul(scene.Scale(axis.scale.X, axis.scale.Y, axis.scale.Z)), Mesh: mesh, Color: scene.ColorHex(axis.color, .78), Glow: [3]float32{.05, .09, .12}, Material: render.Material{Specular: .35, Roughness: .4, RimStrength: .15, RimColor: [3]float32{.3, .9, 1}}}
			v.spatial.Objects = append(v.spatial.Objects, experience.SpatialObject{ID: axis.id, Parent: 1, Node: node})
			v.spatial.Labels = append(v.spatial.Labels, experience.SpatialLabel{Text: axis.name, Position: root.TransformPoint(axis.labelAt), Color: scene.ColorHex(axis.color, 1)})
		}
		for _, row := range sampleRows(d, view) {
			x, _ := chartValue(d, view.X, row)
			y, _ := chartValue(d, view.Y, row)
			z, _ := chartValue(d, view.Z, row)
			position := scene.Vec3{X: float32(normalize(x, d.Minimum[view.X], d.Maximum[view.X])-.5) * .68, Y: float32(normalize(y, d.Minimum[view.Y], d.Maximum[view.Y])-.5) * .52, Z: float32(normalize(z, d.Minimum[view.Z], d.Maximum[view.Z])-.5) * .38}
			scale, colorValue, glow := float32(.011), uint32(0x4fc1d4), [3]float32{.03, .16, .19}
			if row == view.Selected {
				scale, colorValue, glow = .022, 0xffc86a, [3]float32{.45, .23, .04}
			}
			node := scene.Node{Transform: scene.Translate(position.X, position.Y, position.Z).Mul(scene.Scale(scale, scale, scale)), Mesh: mesh, Color: scene.ColorHex(colorValue, 1), WireColor: scene.ColorHex(0xbaf8ff, .4), WireWidth: .5, Glow: glow, CastShadow: true, Material: render.Material{Specular: .55, Roughness: .28, Metallic: .5, RimStrength: .25, RimColor: [3]float32{.3, .9, 1}}}
			v.spatial.Objects = append(v.spatial.Objects, experience.SpatialObject{ID: uint64(row + 2), Parent: 1, Node: node})
			if row == view.Selected {
				label := fmt.Sprintf("#%d  %s=%s", row+1, d.Columns[view.Y], d.Rows[row][view.Y])
				v.spatial.Labels = append(v.spatial.Labels, experience.SpatialLabel{Text: label, Position: root.TransformPoint(position.Add(scene.Vec3{Y: .035})), Color: scene.ColorHex(0xffd38a, 1)})
			}
		}
	}
	v.updateControls()
	v.dirty = false
	return v.renderer.draw(v)
}

func IsDatasetPath(path string) bool {
	lower := strings.ToLower(path)
	return filepath.Ext(lower) == ".csv" || filepath.Ext(lower) == ".tsv" || strings.HasSuffix(lower, ".worldr-data.json")
}
