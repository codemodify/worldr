package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// One light volume encloses the visible native casters. Content textures keep
// their original readable appearance; only explicitly authored meshes receive
// shadows. Closed/hidden spaces cannot enlarge the map or cast unseen shadows.
func (w *Workspace) fitApplicationShadow() {
	w.fitCinematicLighting()
	minimum, maximum := scene.Vec3{}, scene.Vec3{}
	found := false
	translucentLayers := 0
	var visit func(scene.NodeID, scene.Mat4)
	visit = func(id scene.NodeID, parent scene.Mat4) {
		n := w.scene.Node(id)
		if n == nil || n.Hidden {
			return
		}
		if n.Translucent && (n.Surface != nil || n.Mesh != nil) {
			translucentLayers++
			// A mesh may intersect itself; retain at least the native default.
			if n.Mesh != nil {
				translucentLayers += 7
			}
		}
		transform := parent.Mul(n.Transform)
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

// fitCinematicLighting keeps presentation effects in the frame/view contracts:
// no geometry, texture, picking bound, or app pixel is rebuilt as the lights
// move. Adaptive focus naturally reaches the exact neutral output at blend 0.
func (w *Workspace) fitCinematicLighting() {
	strength := w.presentationBlend
	// The workspace appends host-owned cursors after Draw and mixes exact-color
	// client/UI pixels with scene geometry. A frame-wide finish would grade those
	// pixels and make cursor bloom escape its logical bounds. Keep the compositor
	// output neutral; OutputTransform remains available to experiences that own
	// the complete frame until the renderer exposes a pre-overlay effect boundary.
	w.canvas.SetOutputTransform(render.OutputTransform{})
	if strength <= 0 {
		w.scene.PointLights = nil
		return
	}
	phase := float64(w.m.clock) * .32
	if w.m.reducedMotion {
		phase = .7
	}
	target := w.camera.Target
	w.scene.PointLights = []render.PointLight{
		{
			Position:  [3]float32{target.X + 4.2*float32(math.Cos(phase)), target.Y + 2.4, target.Z + 4.2*float32(math.Sin(phase))},
			Color:     [3]float32{.18, .78, 1},
			Intensity: .72 * strength,
			Radius:    11,
		},
		{
			Position:  [3]float32{target.X - 3.3, target.Y - 1.1 + .5*float32(math.Sin(phase*.7)), target.Z + 2.2},
			Color:     [3]float32{1, .48, .16},
			Intensity: .34 * strength,
			Radius:    8,
		},
	}
}
