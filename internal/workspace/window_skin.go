package workspace

import (
	"crypto/sha256"
	"encoding/json"
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// A workspace retains only the selected immutable skin. Aspect buckets bound
// resize allocation, and retired geometry is released by the host after drawing.
type windowSkinCache struct {
	source      *skin.Skin
	fingerprint [32]byte
	meshes      map[windowSkinMeshKey]*windowSkinMeshes
	retired     []uint64
	clock       uint64
	titles      map[uint64]*windowSkinTitle
}

type windowSkinMeshKey struct {
	aspect uint16
	width  uint16
	state  string
	read   bool
}

type windowSkinPart struct {
	mesh        *scene.Mesh
	material    render.Material
	glow        [3]float32
	translucent bool
}

type windowSkinMeshes struct {
	frame, grip, controls, resize windowSkinPart
	layout                        windowSkinLayout
	used                          uint64
}

type windowSkinControl struct {
	kind applicationWindowControl
	name string
	box  box
}

type windowSkinLayout struct {
	outer, frame, topbar, titlebar, grip, resize box
	controls                                     []windowSkinControl
	logicalWidth                                 float32
	verticalTitle                                bool
}

func (w *Workspace) syncWindowSkinCache() {
	c := &w.windowSkinCache
	for id, title := range c.titles {
		if w.scene.Node(title.node) == nil {
			c.retired = append(c.retired, title.mesh.Geometry().ID())
			delete(c.titles, id)
		}
	}
	if c.source == w.activeSkin {
		return
	}
	var fingerprint [32]byte
	if w.activeSkin != nil {
		data, _ := json.Marshal(w.activeSkin) // SetSkin accepts validated finite recipes.
		fingerprint = sha256.Sum256(data)
	}
	if c.source != nil && w.activeSkin != nil && fingerprint == c.fingerprint {
		c.source = w.activeSkin
		return
	}
	for _, meshes := range c.meshes {
		for _, part := range []windowSkinPart{meshes.frame, meshes.grip, meshes.controls, meshes.resize} {
			if part.mesh != nil {
				c.retired = append(c.retired, part.mesh.Geometry().ID())
			}
		}
	}
	for id := range c.titles {
		w.retireWindowSkinTitle(id)
	}
	c.source, c.fingerprint = w.activeSkin, fingerprint
	c.meshes = nil
}

// RetiredGeometryIDs drains geometry belonging to a replaced skin. It shares
// the application's retirement contract, so the GPU does not retain every
// skin the user previews during a session.
func (w *Workspace) RetiredGeometryIDs() []uint64 {
	w.syncWindowSkinCache()
	retired := w.windowSkinCache.retired
	w.windowSkinCache.retired = nil
	if w.panelField != nil {
		retired = append(retired, w.panelField.retiredGeometry...)
		w.panelField.retiredGeometry = nil
	}
	return retired
}

func skinWindowLayout(s *skin.Skin, aspect float32) windowSkinLayout {
	return skinWindowLayoutForWidth(s, aspect, 960)
}

func skinWindowLayoutForWidth(s *skin.Skin, aspect, width float32) windowSkinLayout {
	// Client-pixel packages retain the authored chrome density on small tools
	// as well as wide documents. Empty ScaleMode preserves canonical 960 units.
	if s.Window.Layout.ScaleMode != "client-pixels" {
		width = 960
	}
	width = max(96, width)
	height := width / aspect
	l := s.Window.Layout
	minButtonW, minButtonH, padding, minResize := float32(44), float32(34), float32(8), float32(28)
	if l.ScaleMode == "client-pixels" {
		minButtonW, minButtonH, padding, minResize = 24, 20, 4, 20
	}
	buttonW, buttonH := max(minButtonW, float32(l.ButtonWidth)), max(minButtonH, float32(l.ButtonHeight))
	titleH := max(buttonH+padding, float32(l.TitlebarHeight))
	border := max(float32(2), float32(l.BorderWidth))
	bottom := float32(l.BottomHeight)
	gap := max(float32(3), float32(l.ButtonGap))
	if l.ScaleMode == "client-pixels" {
		// Keep all three controls reachable even when a custom recipe authors
		// larger buttons than a small utility's rail can contain.
		gap = min(gap, max(3, (width-3*minButtonW)/4))
		buttonW = min(buttonW, max(minButtonW, (width-4*gap)/3))
	}
	resize := max(minResize, float32(l.ResizeSize))
	result := windowSkinLayout{
		frame:         box{-.5 - border/width, -.5 - (bottom+border)/height, 1 + 2*border/width, 1 + (titleH+bottom+2*border)/height},
		topbar:        box{-.5, .5, 1, titleH / height},
		resize:        box{.5 - resize*.40/width, -.5 - resize/height, resize / width, resize / height},
		logicalWidth:  width,
		verticalTitle: l.TitleSide == "left",
	}
	result.outer, result.titlebar = result.frame, result.topbar
	if bottom >= resize {
		result.resize = box{.5 - resize/width, -.5 - bottom/height, resize / width, resize / height}
	}
	order := l.ButtonOrder
	if len(order) == 0 {
		order = []string{"minimize", "maximize", "close"}
	}
	controlWidth := float32(len(order))*buttonW + float32(len(order)-1)*gap
	x := .5 - (controlWidth+gap)/width
	if l.ButtonsSide == "left" {
		x = -.5 + gap/width
	}
	for _, name := range order {
		kind := windowControlNone
		switch name {
		case "minimize":
			kind = windowControlMinimize
		case "maximize":
			kind = windowControlRead
		case "close":
			kind = windowControlClose
		}
		if kind != windowControlNone {
			result.controls = append(result.controls, windowSkinControl{kind, name, box{x, .5 + (titleH-buttonH)/(2*height), buttonW / width, buttonH / height}})
		}
		x += (buttonW + gap) / width
	}
	gripWidth := min(float32(.24), max(float32(.10), (width-controlWidth-5*gap)/width*.42))
	if l.GripWidth > 0 {
		gripWidth = min(float32(l.GripWidth)/width, max(0, (width-controlWidth-5*gap)/width))
	}
	gripX := -.5 + gap*2/width
	if l.ButtonsSide == "left" {
		gripX = .5 - gripWidth - gap*2/width
	}
	result.grip = box{gripX, .5 + titleH*.23/height, gripWidth, titleH * .54 / height}
	if result.verticalTitle {
		titleWidth := float32(l.TitleWidth)
		if titleWidth == 0 {
			titleWidth = 32
		}
		// Leave enough ink length for common tool names at the authored font
		// size, including short clients such as launchers and message panels.
		tabHeight := min(result.frame.h*height, min(float32(148), max(float32(120), height*.70)))
		result.titlebar = box{result.frame.x - titleWidth/width, result.frame.y + result.frame.h - tabHeight/height, titleWidth / width, tabHeight / height}
		result.outer.x -= titleWidth / width
		result.outer.w += titleWidth / width
		result.grip = box{result.titlebar.x + 8/width, result.titlebar.y + 14/height, max(8, titleWidth-13) / width, 9 / height}
	}
	return result
}

// Width is rounded to four client pixels and ratios to 1/160 only for packages
// that opt into pixel density. The 64-entry LRU still bounds retained geometry.
func (w *Workspace) windowSkinDimensions(surface experience.ApplicationSurface) (uint16, float32, uint16, float32) {
	aspect := w.windowBorderAspect(surface)
	if w.activeSkin == nil || w.activeSkin.Window.Layout.ScaleMode != "client-pixels" {
		bucket, ratio := quantizedWindowBorderAspect(aspect)
		return bucket, ratio, 960, 960
	}
	width := 960
	if surface.Texture != nil {
		width, _ = surface.Texture.Size()
	}
	if index := w.m.applicationState.index(surface.Key); index >= 0 && w.m.applicationState.Layouts[index].Width > 0 {
		// Match the physical client plane after provider minimum-size policy.
		// A saved 190px request for a legacy 720px app must not magnify chrome.
		var height int
		width, height = applicationSurfaceSize(surface, w.m.applicationState.Layouts[index])
		aspect = float32(width) / float32(height)
	}
	width = max(96, min(4096, (width+2)/4*4))
	if aspect <= 0 || math.IsNaN(float64(aspect)) || math.IsInf(float64(aspect), 0) {
		aspect = 1.6
	}
	bucket := max(20, min(1280, int(math.Round(float64(aspect*160)))))
	return uint16(bucket), float32(bucket) / 160, uint16(width), float32(width)
}

func (w *Workspace) windowSkinState(surface experience.ApplicationSurface) string {
	if w.pointer.windowDrag && w.pointer.surface == w.applicationNodes[surface.ID] ||
		w.pointer.kind == captureApplicationWindowControl && w.pointer.applicationID == surface.ID {
		return "pressed"
	}
	if w.applicationHoveredID == surface.ID {
		return "hovered"
	}
	if w.applicationKeyboard && w.applicationFocusedID == surface.ID {
		return "focused"
	}
	if index := w.m.applicationState.index(surface.Key); index >= 0 && w.m.applicationState.Selected&(1<<index) != 0 {
		return "selected"
	}
	return "normal"
}

func (w *Workspace) skinWindowMeshes(surface experience.ApplicationSurface) *windowSkinMeshes {
	w.syncWindowSkinCache()
	if w.activeSkin == nil {
		return nil
	}
	bucket, aspect, widthBucket, width := w.windowSkinDimensions(surface)
	key := windowSkinMeshKey{aspect: bucket, width: widthBucket, state: w.windowSkinState(surface), read: w.m.applicationState.Reading && !w.m.applicationState.Overview}
	cache := &w.windowSkinCache
	cache.clock++
	if mesh := cache.meshes[key]; mesh != nil {
		mesh.used = cache.clock
		return mesh
	}
	if cache.meshes == nil {
		cache.meshes = make(map[windowSkinMeshKey]*windowSkinMeshes)
	}
	result := buildSkinWindowMeshesForViewAndWidth(w.activeSkin, aspect, width, key.state, key.read)
	result.used = cache.clock
	// Two full 32-window workspaces fit, while continuous resizing cannot keep
	// hundreds of GPU meshes alive. Geometry leaves through the same retirement
	// queue as a changed skin, after current nodes have been synchronized.
	const maxRetainedWindowSkinMeshes = 64
	if len(cache.meshes) >= maxRetainedWindowSkinMeshes {
		var oldestKey windowSkinMeshKey
		oldest := ^uint64(0)
		for candidate, mesh := range cache.meshes {
			if mesh.used < oldest {
				oldestKey, oldest = candidate, mesh.used
			}
		}
		old := cache.meshes[oldestKey]
		for _, part := range []windowSkinPart{old.frame, old.grip, old.controls, old.resize} {
			cache.retired = append(cache.retired, part.mesh.Geometry().ID())
		}
		delete(cache.meshes, oldestKey)
	}
	cache.meshes[key] = result
	return result
}

func buildSkinWindowMeshes(s *skin.Skin, aspect float32, state string) *windowSkinMeshes {
	return buildSkinWindowMeshesForView(s, aspect, state, false)
}

func buildSkinWindowMeshesForView(s *skin.Skin, aspect float32, state string, reading bool) *windowSkinMeshes {
	return buildSkinWindowMeshesForViewAndWidth(s, aspect, 960, state, reading)
}

func buildSkinWindowMeshesForViewAndWidth(s *skin.Skin, aspect, width float32, state string, reading bool) *windowSkinMeshes {
	layout := skinWindowLayoutForWidth(s, aspect, width)
	if reading {
		layout.frame.h -= layout.topbar.h
		layout.outer = layout.frame
	}
	result := &windowSkinMeshes{layout: layout}
	// A frame surrounds both client and titlebar. Its inner exclusion removes
	// all client pixels and leaves the titlebar to the independently picked grip.
	frame := newWindowSkinBuilderForWidth(s, aspect, layout.logicalWidth, state)
	if !reading && !layout.verticalTitle {
		frame.exclusions = []box{{-.5, -.5, 1, 1 + layout.topbar.h}}
	}
	frame.recipe(s.Window.Frame, layout.frame)
	frame.addRearWalls(s.Window.Frame, layout.frame)
	result.frame = frame.part()

	grip := newWindowSkinBuilderForWidth(s, aspect, layout.logicalWidth, state)
	for _, control := range layout.controls {
		grip.exclusions = append(grip.exclusions, control.box)
	}
	grip.pickPlate(layout.titlebar)
	if layout.verticalTitle {
		grip.pickPlate(layout.topbar)
	}
	grip.recipe(s.Window.Titlebar, layout.titlebar)
	grip.recipe(s.Window.Grip, layout.grip)
	result.grip = grip.part()

	controls := newWindowSkinBuilderForWidth(s, aspect, layout.logicalWidth, state)
	for _, control := range layout.controls {
		controls.pickPlate(control.box)
		recipe := s.Window.Buttons[control.name]
		controls.recipe(recipe, control.box)
		if recipe.Icon == "" {
			controls.defaultWindowGlyph(control.kind, control.box)
		}
	}
	result.controls = controls.part()

	resize := newWindowSkinBuilderForWidth(s, aspect, layout.logicalWidth, state)
	resize.pickPlate(layout.resize)
	resize.recipe(s.Window.Resize, layout.resize)
	result.resize = resize.part()
	return result
}

func applyWindowSkinPart(node *scene.Node, part windowSkinPart) {
	if node == nil {
		return
	}
	node.Mesh = part.mesh
	node.Color = scene.Color{R: 1, G: 1, B: 1, A: 1}
	node.Material, node.Glow = part.material, part.glow
	node.Translucent = part.translucent
	node.Unlit = part.material.Transmission == 0 && part.material.Hologram == 0
	node.DepthReadOnly = false
}

func resetWindowSkinMaterial(node *scene.Node, frame bool) {
	node.Material = render.Material{}
	node.Translucent = false
	node.Unlit = true
	node.DepthReadOnly = frame
}

func (w *Workspace) windowSkinControlAtPoint(surface experience.ApplicationSurface, point scene.Vec3) applicationWindowControl {
	meshes := w.skinWindowMeshes(surface)
	if meshes == nil {
		return applicationWindowControlAtPoint(point)
	}
	for _, control := range meshes.layout.controls {
		if control.box.contains(point.X, point.Y) {
			return control.kind
		}
	}
	return windowControlNone
}

func (w *Workspace) windowSkinReadBounds(surface experience.ApplicationSurface, center scene.Vec3, width, height float32) (scene.Vec3, float32, float32, bool) {
	if w.activeSkin == nil {
		return center, width, height, false
	}
	_, aspect, _, logicalWidth := w.windowSkinDimensions(surface)
	l := skinWindowLayoutForWidth(w.activeSkin, aspect, logicalWidth)
	if w.m.applicationState.Reading && !w.m.applicationState.Overview {
		l.frame.h -= l.topbar.h
		l.outer = l.frame
	}
	right, up, _ := applicationBasis()
	center = center.Add(up.Mul((l.outer.y + l.outer.h/2) * height)).Add(right.Mul((l.outer.x + l.outer.w/2) * width))
	// Account for rearward frame depth when Read's camera looks straight on.
	return center, width * l.outer.w * 1.02, height * l.outer.h * 1.02, true
}

func skinSceneColor(s *skin.Skin, token string, opacity float32) scene.Color {
	c := s.Color(token)
	return scene.Color{R: float32(c.R) / 255, G: float32(c.G) / 255, B: float32(c.B) / 255, A: float32(c.A) / 255 * opacity}
}

func clampSkinFloat(value float64) float32 {
	return float32(math.Max(0, math.Min(1, value)))
}
