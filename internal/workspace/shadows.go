package workspace

import (
	"math"
	"reflect"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// One light volume encloses the visible native casters. Content textures keep
// their original readable appearance; only explicitly authored meshes receive
// shadows. Closed/hidden spaces cannot enlarge the map or cast unseen shadows.
func (w *Workspace) fitApplicationShadow() {
	w.fitCinematicLighting()
	minimum, maximum := scene.Vec3{}, scene.Vec3{}
	found := false
	translucentLayers := 0
	var panelTiles [panelLayerColumns * panelLayerRows]uint8
	panelChrome := w.panelFieldVisible() && knownPanelLayerRecipes(w.activeSkin)
	var visit func(scene.NodeID, scene.Mat4)
	visit = func(id scene.NodeID, parent scene.Mat4) {
		n := w.scene.Node(id)
		if n == nil || n.Hidden {
			return
		}
		transform := parent.Mul(n.Transform)
		if n.Translucent && (n.Surface != nil || n.Mesh != nil) {
			if !panelChrome || n.Mesh == nil || !w.addPanelChromeLayers(id, n.Mesh, transform, &panelTiles) {
				translucentLayers++
				// Arbitrary meshes retain the existing self-intersection budget.
				if n.Mesh != nil {
					translucentLayers += 7
				}
			}
		}
		if n.CastShadow && n.Mesh != nil {
			low, high := n.Mesh.Bounds()
			for i := 0; i < 8; i++ {
				p := low
				if i&1 != 0 {
					p.X = high.X
				}
				if i&2 != 0 {
					p.Y = high.Y
				}
				if i&4 != 0 {
					p.Z = high.Z
				}
				p = transform.TransformPoint(p)
				if !found {
					minimum, maximum = p, p
					found = true
				} else {
					minimum = scene.Vec3{X: min(minimum.X, p.X), Y: min(minimum.Y, p.Y), Z: min(minimum.Z, p.Z)}
					maximum = scene.Vec3{X: max(maximum.X, p.X), Y: max(maximum.Y, p.Y), Z: max(maximum.Z, p.Z)}
				}
			}
		}
		for _, child := range w.scene.Children(id) {
			visit(child, transform)
		}
	}
	for _, root := range w.scene.Children(0) {
		visit(root, scene.Identity())
	}
	panelPeak := 0
	for _, layers := range panelTiles {
		panelPeak = max(panelPeak, int(layers))
	}
	translucentLayers += panelPeak
	// Sum visible surfaces across windows, including below-parent layers.
	// The renderer has a documented 32-fragment transparency bound.
	w.scene.TransparencyLayers = max(1, min(32, translucentLayers))
	w.scene.Shadow = render.Shadow{}
	if found {
		light := scene.Vec3{X: .4, Y: .6, Z: 1}
		center := minimum.Add(maximum).Mul(.5)
		extent := max(.1, maximum.Sub(minimum).Length()*1.2)
		shadow, err := scene.DirectionalShadow(center, light, extent, extent)
		if err == nil {
			shadow.Strength = .62
			w.scene.Light = light
			w.scene.Shadow = shadow
		}
	}
}

const panelLayerColumns, panelLayerRows = 32, 24

var panelLayerReference = func() skin.Window {
	s, _ := skin.Builtin("future-panels")
	return s.Window
}()

func knownPanelLayerRecipes(s *skin.Skin) bool {
	// Colors and custom package IDs may change freely. Geometry, per-state
	// offsets and the clipped convex rear wall must retain the proven recipe.
	return s != nil && reflect.DeepEqual(s.Window.Frame, panelLayerReference.Frame) &&
		reflect.DeepEqual(s.Window.Titlebar, panelLayerReference.Titlebar) &&
		reflect.DeepEqual(s.Window.Grip, panelLayerReference.Grip)
}

// addPanelChromeLayers bounds coincident fragments, not the number of scene
// nodes. Each tile conservatively covers every pixel in a small screen region;
// rounding and near-plane ambiguity can only increase its layer allowance.
// Each authored layer is a plane. Convex rear walls contribute at most two
// intersections; each grip has one additional coplanar input-coverage plane.
func (w *Workspace) addPanelChromeLayers(id scene.NodeID, mesh *scene.Mesh, transform scene.Mat4, tiles *[panelLayerColumns * panelLayerRows]uint8) bool {
	for _, surface := range w.applicationSurfaces {
		frame, grip := w.applicationFrames[surface.ID] == id, w.applicationDragHandles[surface.ID] == id
		if !frame && !grip {
			continue
		}
		meshes := w.skinWindowMeshes(surface)
		if meshes == nil || frame && meshes.frame.mesh != mesh || grip && meshes.grip.mesh != mesh {
			return false
		}
		low, high := mesh.Bounds()
		add := func(layers int, boxes ...box) {
			var covered [panelLayerColumns * panelLayerRows]bool
			for _, bounds := range boxes {
				w.markPanelLayerBox(bounds, low.Z, high.Z, transform, &covered)
			}
			for i, hit := range covered {
				if hit {
					tiles[i] = uint8(min(32, int(tiles[i])+layers))
				}
			}
		}
		if frame {
			// The compiler clips every front triangle to the client aperture;
			// convex rear walls also remain in these exterior bands.
			top := float32(.5)
			reading := w.m.applicationState.Reading && !w.m.applicationState.Overview
			if !reading && !meshes.layout.verticalTitle {
				top += meshes.layout.topbar.h
			}
			bands := [...]box{{low.X, low.Y, -.5 - low.X, high.Y - low.Y}, {.5, low.Y, high.X - .5, high.Y - low.Y}, {low.X, low.Y, high.X - low.X, -.5 - low.Y}, {low.X, top, high.X - low.X, high.Y - top}}
			add(3, bands[:]...) // first plane, with stroke, and both convex wall intersections
			for _, layer := range w.activeSkin.Window.Frame.Layers[1:] {
				bounds := windowSkinLayerBounds(meshes.layout.frame, layer.Bounds)
				var pieces [4]box
				for i, band := range bands {
					x, y := max(bounds.x, band.x), max(bounds.y, band.y)
					pieces[i] = box{x, y, min(bounds.x+bounds.w, band.x+band.w) - x, min(bounds.y+bounds.h, band.y+band.h) - y}
				}
				add(1, pieces[:]...)
			}
		} else {
			add(1, meshes.layout.titlebar, meshes.layout.topbar) // coplanar input plates
			for _, layer := range w.activeSkin.Window.Titlebar.Layers {
				add(1, windowSkinLayerBounds(meshes.layout.titlebar, layer.Bounds))
			}
			for _, layer := range w.activeSkin.Window.Grip.Layers {
				add(1, windowSkinLayerBounds(meshes.layout.grip, layer.Bounds))
			}
		}
		return true
	}
	return false
}

func (w *Workspace) markPanelLayerBox(b box, near, far float32, transform scene.Mat4, covered *[panelLayerColumns * panelLayerRows]bool) {
	if b.w <= 0 || b.h <= 0 || w.viewport.Width <= 0 || w.viewport.Height <= 0 {
		return
	}
	left, top := float32(math.Inf(1)), float32(math.Inf(1))
	right, bottom := float32(math.Inf(-1)), float32(math.Inf(-1))
	for corner := 0; corner < 8; corner++ {
		p := scene.Vec3{X: b.x, Y: b.y, Z: near}
		if corner&1 != 0 {
			p.X += b.w
		}
		if corner&2 != 0 {
			p.Y += b.h
		}
		if corner&4 != 0 {
			p.Z = far
		}
		x, y, depth, _ := w.camera.Project(transform.TransformPoint(p), w.viewport)
		if !finite(float64(x)) || !finite(float64(y)) || !finite(float64(depth)) || depth <= 0 || depth >= 1 {
			// A volume crossing/behind a clip plane has no bounded projected
			// rectangle from its corners. Reserve this layer over the viewport.
			for i := range covered {
				covered[i] = true
			}
			return
		}
		left, top, right, bottom = min(left, x), min(top, y), max(right, x), max(bottom, y)
	}
	vp := w.viewport
	left, top, right, bottom = left-1, top-1, right+1, bottom+1
	if right < vp.X || bottom < vp.Y || left > vp.X+vp.Width || top > vp.Y+vp.Height {
		return
	}
	x0 := max(0, min(panelLayerColumns-1, int(math.Floor(float64((left-vp.X)/vp.Width*panelLayerColumns)))))
	x1 := max(0, min(panelLayerColumns-1, int(math.Floor(float64((right-vp.X)/vp.Width*panelLayerColumns)))))
	y0 := max(0, min(panelLayerRows-1, int(math.Floor(float64((top-vp.Y)/vp.Height*panelLayerRows)))))
	y1 := max(0, min(panelLayerRows-1, int(math.Floor(float64((bottom-vp.Y)/vp.Height*panelLayerRows)))))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			covered[y*panelLayerColumns+x] = true
		}
	}
}

// fitCinematicLighting keeps presentation effects in the frame/view contracts:
// no geometry, texture, picking bound, or app pixel is rebuilt as the lights
// move.
func (w *Workspace) fitCinematicLighting() {
	// The workspace appends host-owned cursors after Draw and mixes exact-color
	// client/UI pixels with scene geometry. A frame-wide finish would grade those
	// pixels and make cursor bloom escape its logical bounds. Keep the compositor
	// output neutral; OutputTransform remains available to experiences that own
	// the complete frame until the renderer exposes a pre-overlay effect boundary.
	w.canvas.SetOutputTransform(render.OutputTransform{})
	phase := 2 * math.Pi * float64(w.backgroundPhase) / float64(dnaRotationPeriod)
	target := w.camera.Target
	w.scene.PointLights = []render.PointLight{
		{
			Position:  [3]float32{target.X + 4.2*float32(math.Cos(phase)), target.Y + 2.4, target.Z + 4.2*float32(math.Sin(phase))},
			Color:     [3]float32{.18, .78, 1},
			Intensity: .72,
			Radius:    11,
		},
		{
			Position:  [3]float32{target.X - 3.3, target.Y - 1.1 + .5*float32(math.Sin(phase*.7)), target.Z + 2.2},
			Color:     [3]float32{1, .48, .16},
			Intensity: .34,
			Radius:    8,
		},
	}
}
