package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

// Application frames are scene content, not part of the client's image or hit
// region. Each style shares retained geometry across its application instances
// and inherits the parent's placement, aspect ratio and visibility.
func applicationFrameMesh() (*scene.Mesh, error) {
	const inner, outer, depth = float32(.502), float32(.505), float32(.12)
	var g frameGeometry
	front := scene.Color{R: 1, G: 1, B: 1, A: 1}
	outerEdge := scene.ColorHex(0x355864, .96)
	innerEdge := scene.ColorHex(0x67909a, .72)

	// Keep the face in the application plane and wholly outside its pixels.
	// The side walls extend only behind that plane, so an oblique workspace view
	// exposes real depth without changing the client's image or hit rectangle.
	g.polygon(front, framePoint{-outer, -outer}, framePoint{outer, -outer}, framePoint{inner, -inner}, framePoint{-inner, -inner})
	g.polygon(front, framePoint{outer, -outer}, framePoint{outer, outer}, framePoint{inner, inner}, framePoint{inner, -inner})
	g.polygon(front, framePoint{outer, outer}, framePoint{-outer, outer}, framePoint{-inner, inner}, framePoint{inner, inner})
	g.polygon(front, framePoint{-outer, outer}, framePoint{-outer, -outer}, framePoint{-inner, -inner}, framePoint{-inner, inner})
	g.wall(depth, outerEdge, true,
		framePoint{-outer, -outer}, framePoint{outer, -outer}, framePoint{outer, outer}, framePoint{-outer, outer})
	g.wall(depth, innerEdge, true,
		framePoint{-inner, -inner}, framePoint{-inner, inner}, framePoint{inner, inner}, framePoint{inner, -inner})
	return g.mesh()
}

func (w *Workspace) attachApplicationFrame(id uint64, parent scene.NodeID) {
	w.applicationFrames[id] = w.scene.Add(parent, scene.Node{
		Mesh: w.applicationFrameMesh, Color: scene.ColorHex(0x6a9dab, .10),
		Unlit: true, Unpickable: true, DepthReadOnly: true,
	})
	w.attachApplicationDragHandle(id, parent)
	w.attachApplicationWindowControls(id, w.applicationDragHandles[id])
	w.attachApplicationResizeHandle(id, parent)
}

func (w *Workspace) syncApplicationFrame(surface experience.ApplicationSurface) {
	w.syncApplicationDragHandle(surface)
	w.syncApplicationWindowControls(surface)
	w.syncApplicationResizeHandle(surface)
	frame := w.scene.Node(w.applicationFrames[surface.ID])
	if frame == nil {
		return
	}
	i := w.m.applicationState.index(surface.Key)
	minimized := i >= 0 && w.m.applicationState.Layouts[i].Minimized
	frame.Hidden = surface.Frameless || minimized && !w.m.applicationState.Overview
	if frame.Hidden {
		frame.Glow = [3]float32{}
		return
	}
	frame.Mesh = w.frameMeshFor(surface)
	color, alpha := uint32(0x6a9dab), float32(.10)
	glow := float32(0)
	index := i
	switch {
	case w.applicationKeyboard && w.applicationFocusedID == surface.ID:
		color, alpha = 0x82eaf5, .90
		glow = 1
	case index >= 0 && w.m.applicationState.Selected&(1<<index) != 0:
		color, alpha = 0x62cedf, .50
		glow = .65
	case w.applicationHoveredID == surface.ID:
		color, alpha = 0xb4e9f1, .32
		glow = .35
	}
	if photoFrameSurface(surface) {
		// An always-visible left bracket identifies the photo while keeping
		// its other edges open. A small selection lift avoids a bright halo.
		color = 0xc3f4ff
		alpha = .76 + alpha*.20
		glow *= .18
	} else {
		// Border meshes author their own material channels, so the node tint is
		// theme-owned rather than forcing every reference direction back to cyan.
		visual := borderVisualFor(w.selectedWindowBorderStyle())
		color = visual.tint
		switch {
		case w.applicationKeyboard && w.applicationFocusedID == surface.ID:
			alpha = .98
		case index >= 0 && w.m.applicationState.Selected&(1<<index) != 0:
			alpha = .82
		case w.applicationHoveredID == surface.ID:
			alpha = .72
		default:
			alpha = visual.idleAlpha
		}
	}
	frame.Color = scene.ColorHex(color, alpha)
	// Idle windows never emit, even while a different window has focus.
	frame.Glow = [3]float32{frame.Color.R * glow, frame.Color.G * glow, frame.Color.B * glow}
}
