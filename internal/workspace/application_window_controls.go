package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

type applicationWindowControl uint8

const (
	windowControlNone applicationWindowControl = iota
	windowControlMinimize
	windowControlMaximize
	windowControlClose
)

var applicationWindowControlBoxes = [...]struct {
	kind applicationWindowControl
	box  box
}{
	{windowControlMinimize, box{.246, .521, .068, .045}},
	{windowControlMaximize, box{.326, .521, .068, .045}},
	{windowControlClose, box{.406, .521, .080, .045}},
}

// applicationWindowControlMesh keeps the three conventional window actions in
// world space. It is parented to the drag grip so Read, Overview and frameless
// surfaces inherit the grip's established visibility policy.
func buildApplicationWindowControlMesh() (*scene.Mesh, error) {
	var g frameGeometry
	plate := scene.ColorHex(0x123b48, .96)
	edge := scene.ColorHex(0x5ed5e8, .90)
	glyph := scene.ColorHex(0xc4f8ff, 1)
	for _, control := range applicationWindowControlBoxes {
		b := control.box
		left, bottom, right, top := b.x, b.y, b.x+b.w, b.y+b.h
		cut := float32(.008)
		g.polygon(plate,
			framePoint{left + cut, bottom}, framePoint{right, bottom},
			framePoint{right, top - cut}, framePoint{right - cut, top},
			framePoint{left, top}, framePoint{left, bottom + cut},
		)
		g.stroke(.0015, edge,
			framePoint{left + cut, bottom + .002}, framePoint{right - .002, bottom + .002},
			framePoint{right - .002, top - cut}, framePoint{right - cut, top - .002},
		)
		cx, cy := left+b.w/2, bottom+b.h/2
		switch control.kind {
		case windowControlMinimize:
			g.rect(cx-.014, cy-.010, cx+.014, cy-.006, glyph)
		case windowControlMaximize:
			g.stroke(.003, glyph,
				framePoint{cx - .013, cy - .011}, framePoint{cx + .013, cy - .011},
				framePoint{cx + .013, cy + .011}, framePoint{cx - .013, cy + .011},
				framePoint{cx - .013, cy - .011},
			)
		case windowControlClose:
			g.stroke(.003, glyph, framePoint{cx - .012, cy - .012}, framePoint{cx + .012, cy + .012})
			g.stroke(.003, glyph, framePoint{cx - .012, cy + .012}, framePoint{cx + .012, cy - .012})
		}
	}
	return g.mesh()
}

func buildApplicationMinimizedBarMesh() (*scene.Mesh, error) {
	var g frameGeometry
	plate := scene.ColorHex(0x092d39, .98)
	edge := scene.ColorHex(0x34c6df, .92)
	detail := scene.ColorHex(0x7ee9f7, .72)
	// A collapsed window remains as a perspective-correct spatial strip at its
	// saved position. The right side is left open for the action controls.
	g.polygon(plate,
		framePoint{-.49, .521}, framePoint{.226, .521},
		framePoint{.226, .566}, framePoint{-.472, .566}, framePoint{-.49, .548},
	)
	g.stroke(.0025, edge,
		framePoint{-.488, .523}, framePoint{.224, .523},
		framePoint{.224, .564}, framePoint{-.471, .564}, framePoint{-.488, .547},
	)
	g.rect(-.445, .541, -.165, .546, detail)
	for _, x := range []float32{-.13, -.106, -.082} {
		g.rect(x, .536, x+.011, .551, detail)
	}
	return g.mesh()
}

func buildApplicationResizeHandleMesh() (*scene.Mesh, error) {
	var g frameGeometry
	plate := scene.ColorHex(0x103744, .96)
	edge := scene.ColorHex(0x75e9f7, .96)
	detail := scene.ColorHex(0x3faabd, .78)
	// The clipped corner is visibly different from the top drag grip. Its full
	// plate remains pickable, while the inset diagonals communicate resize.
	g.polygon(plate,
		framePoint{.466, -.505}, framePoint{.505, -.466},
		framePoint{.548, -.509}, framePoint{.548, -.548}, framePoint{.509, -.548},
	)
	g.stroke(.003, edge, framePoint{.486, -.539}, framePoint{.539, -.486})
	g.stroke(.002, detail, framePoint{.506, -.539}, framePoint{.539, -.506})
	return g.mesh()
}

func (w *Workspace) attachApplicationWindowControls(id uint64, parent scene.NodeID) {
	if w.applicationWindowControlMesh == nil {
		mesh, err := buildApplicationWindowControlMesh()
		if err != nil {
			panic(err) // Invalid constant geometry is a programming error.
		}
		w.applicationWindowControlMesh = mesh
	}
	if w.applicationWindowControls == nil {
		w.applicationWindowControls = make(map[uint64]scene.NodeID)
	}
	w.applicationWindowControls[id] = w.scene.Add(parent, scene.Node{
		Mesh: w.applicationWindowControlMesh, Color: scene.Color{R: 1, G: 1, B: 1, A: 1}, Unlit: true,
	})
}

func (w *Workspace) attachApplicationResizeHandle(id uint64, parent scene.NodeID) {
	if w.applicationResizeHandleMesh == nil {
		mesh, err := buildApplicationResizeHandleMesh()
		if err != nil {
			panic(err) // Invalid constant geometry is a programming error.
		}
		w.applicationResizeHandleMesh = mesh
	}
	if w.applicationResizeHandles == nil {
		w.applicationResizeHandles = make(map[uint64]scene.NodeID)
	}
	w.applicationResizeHandles[id] = w.scene.Add(parent, scene.Node{
		Mesh: w.applicationResizeHandleMesh, Color: scene.Color{R: 1, G: 1, B: 1, A: 1}, Unlit: true,
	})
}

func (w *Workspace) syncApplicationWindowControls(surface experience.ApplicationSurface) {
	control := w.scene.Node(w.applicationWindowControls[surface.ID])
	if control == nil {
		return
	}
	i := w.m.applicationState.index(surface.Key)
	minimized := i >= 0 && w.m.applicationState.Layouts[i].Minimized
	if minimized && !w.m.applicationState.Overview {
		if w.applicationMinimizedBarMesh == nil {
			mesh, err := buildApplicationMinimizedBarMesh()
			if err != nil {
				panic(err) // Invalid constant geometry is a programming error.
			}
			w.applicationMinimizedBarMesh = mesh
		}
		if handle := w.scene.Node(w.applicationDragHandles[surface.ID]); handle != nil {
			handle.Mesh = w.applicationMinimizedBarMesh
			handle.Hidden = surface.Frameless || surface.DragContent
			handle.Color = scene.ColorHex(0x78e9f8, .96)
		}
	}
	control.Mesh = w.applicationWindowControlMesh
	control.Hidden = surface.Frameless || surface.DragContent
	control.Color = scene.Color{R: 1, G: 1, B: 1, A: 1}
	if minimized {
		control.Glow = [3]float32{.08, .36, .44}
	} else if surface.Key == w.m.applicationState.Active {
		control.Glow = [3]float32{.04, .18, .22}
	} else {
		control.Glow = [3]float32{}
	}
}

func (w *Workspace) syncApplicationResizeHandle(surface experience.ApplicationSurface) {
	handle := w.scene.Node(w.applicationResizeHandles[surface.ID])
	if handle == nil {
		return
	}
	i := w.m.applicationState.index(surface.Key)
	minimized := i >= 0 && w.m.applicationState.Layouts[i].Minimized
	view := w.m.applicationState
	handle.Hidden = surface.Frameless || surface.DragContent || minimized || view.Overview || view.Reading
	handle.Mesh = w.applicationResizeHandleMesh
	handle.Color = scene.Color{R: 1, G: 1, B: 1, A: 1}
	if surface.Key == view.Active {
		handle.Glow = [3]float32{.04, .18, .22}
	} else {
		handle.Glow = [3]float32{}
	}
}

func applicationWindowControlAtPoint(point scene.Vec3) applicationWindowControl {
	for _, control := range applicationWindowControlBoxes {
		if control.box.contains(point.X, point.Y) {
			return control.kind
		}
	}
	return windowControlNone
}

func (w *Workspace) applicationWindowControlAt(x, y float32) (experience.ApplicationSurface, applicationWindowControl, scene.NodeID, bool) {
	w.layout(w.width, w.height)
	w.syncScene()
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	if !ok {
		return experience.ApplicationSurface{}, windowControlNone, 0, false
	}
	for _, surface := range w.applicationSurfaces {
		node := w.applicationWindowControls[surface.ID]
		if node == 0 || node != hit.Node {
			continue
		}
		root := w.scene.Node(w.applicationNodes[surface.ID])
		control := w.scene.Node(node)
		if root == nil || control == nil {
			break
		}
		inverse, valid := root.Transform.Mul(w.scene.Node(w.applicationDragHandles[surface.ID]).Transform).Mul(control.Transform).Inverse()
		if !valid {
			break
		}
		kind := applicationWindowControlAtPoint(inverse.TransformPoint(hit.Point))
		return surface, kind, node, kind != windowControlNone
	}
	return experience.ApplicationSurface{}, windowControlNone, 0, false
}

func (w *Workspace) handleApplicationWindowControl(event experience.Event) bool {
	if w.pointer.kind == captureApplicationWindowControl {
		switch event.Kind {
		case experience.PointerMove:
			w.movePointer(event.X, event.Y)
			return true
		case experience.PointerDown, experience.PointerScroll:
			return true
		case experience.PointerUp:
			if applicationButton(event) != 272 {
				return true
			}
			w.movePointer(event.X, event.Y)
			p := w.pointer
			w.pointer = pointerCapture{}
			surface, kind, node, hit := w.applicationWindowControlAt(event.X, event.Y)
			if p.dragged || !hit || node != p.surface || surface.ID != p.applicationID || kind != applicationWindowControl(p.dockIndex) || w.applicationWindowControls[p.applicationID] != p.surface {
				return true
			}
			switch kind {
			case windowControlMinimize:
				if w.applicationFocusedID == surface.ID || w.applicationCapturedID == surface.ID {
					w.clearApplicationFocus()
				}
				_ = w.Dispatch(Action{Kind: ToggleApplicationMinimized, ApplicationKey: surface.Key})
			case windowControlMaximize:
				_ = w.Dispatch(Action{Kind: ToggleApplicationMaximized, ApplicationKey: surface.Key})
			case windowControlClose:
				closer, ok := w.applications.(experience.ApplicationCloser)
				if !ok {
					return true
				}
				if w.applicationFocusedID == surface.ID || w.applicationCapturedID == surface.ID {
					w.clearApplicationFocus()
				} else if w.applicationHoveredID == surface.ID {
					w.clearApplicationHover()
				}
				closer.CloseApplication(surface.ID)
			}
			return true
		case experience.PointerCancel, experience.KeyboardCancel:
			w.pointer = pointerCapture{}
			return event.Kind == experience.PointerCancel
		}
	}
	if len(w.applicationButtons) != 0 || w.pointer.kind != captureNone {
		return false
	}
	if event.Kind != experience.PointerDown && event.Kind != experience.PointerMove && event.Kind != experience.PointerScroll {
		return false
	}
	surface, kind, node, hit := w.applicationWindowControlAt(event.X, event.Y)
	if !hit {
		return false
	}
	w.clearApplicationHover()
	if event.Kind != experience.PointerDown || applicationButton(event) != 272 {
		return true
	}
	w.stopWindowThrowForKey(surface.Key)
	w.cancelPointer()
	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	w.pointer = pointerCapture{
		kind: captureApplicationWindowControl, start: w.Document(), surface: node,
		applicationID: surface.ID, dockIndex: int(kind), pressX: x, pressY: y,
		lastX: x, lastY: y, scale: w.scale, ox: w.ox, oy: w.oy,
	}
	return true
}
