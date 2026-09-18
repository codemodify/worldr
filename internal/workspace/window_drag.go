package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

// The grip is a separate scene mesh above the content plane. Its hit region
// follows perspective and occlusion without taking pixels from the client.
func applicationDragHandleMesh() (*scene.Mesh, error) {
	var vertices []scene.Vec3
	var indices []uint32
	var colors []scene.Color
	quad := func(left, bottom, right, top float32, color scene.Color) {
		base := uint32(len(vertices))
		vertices = append(vertices, scene.Vec3{X: left, Y: bottom}, scene.Vec3{X: right, Y: bottom}, scene.Vec3{X: right, Y: top}, scene.Vec3{X: left, Y: top})
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
		colors = append(colors, color, color)
	}
	// Segments leave two dark notches in a broad, readily grabbed plate.
	quad(-.16, .522, .16, .548, scene.Color{R: .25, G: .35, B: .4, A: 1})
	for _, left := range []float32{-.105, -.025, .055} {
		quad(left, .548, left+.05, .58, scene.Color{R: 1, G: 1, B: 1, A: 1})
	}
	return scene.NewMesh(vertices, indices, colors)
}

func (w *Workspace) attachApplicationDragHandle(id uint64, parent scene.NodeID) {
	if w.applicationDragHandleMesh == nil {
		mesh, err := applicationDragHandleMesh()
		if err != nil {
			panic(err) // Invalid constant geometry is a programming error.
		}
		w.applicationDragHandleMesh = mesh
	}
	if w.applicationDragHandles == nil {
		w.applicationDragHandles = make(map[uint64]scene.NodeID)
	}
	w.applicationDragHandles[id] = w.scene.Add(parent, scene.Node{
		Mesh: w.applicationDragHandleMesh, Color: scene.ColorHex(0x82cdda, .85), Unlit: true,
	})
}

func (w *Workspace) syncApplicationDragHandle(surface experience.ApplicationSurface) {
	handle := w.scene.Node(w.applicationDragHandles[surface.ID])
	if handle == nil {
		return
	}
	handle.Mesh = w.applicationDragHandleMesh
	if cinematicFrameSurface(surface) {
		if w.terminalDragHandleMesh == nil {
			mesh, err := terminalDragHandleMesh()
			if err != nil {
				panic(err)
			}
			w.terminalDragHandleMesh = mesh
		}
		handle.Mesh = w.terminalDragHandleMesh
	}
	view := w.m.applicationState
	handle.Hidden = surface.Frameless || surface.DragContent || view.Overview || view.Reading
	handle.Color = scene.ColorHex(0x6ca7b7, .78)
	if w.pointer.windowDrag && w.pointer.surface == w.applicationNodes[surface.ID] {
		handle.Color = scene.ColorHex(0xb2f5ff, 1)
	} else if surface.Key == view.Active {
		handle.Color = scene.ColorHex(0x82dbe9, .95)
	}
}

func (w *Workspace) applicationDragTarget(x, y float32) (experience.ApplicationSurface, bool) {
	w.layout(w.width, w.height)
	w.syncScene()
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	if ok {
		for _, surface := range w.applicationSurfaces {
			if w.applicationDragHandles[surface.ID] == hit.Node || surface.DragContent && w.applicationNodes[surface.ID] == hit.Node {
				return surface, true
			}
		}
	}
	return experience.ApplicationSurface{}, false
}

// Help can consume a release before drag routing sees it. Clear this owner too
// so closing the guide cannot leave a completed physical stroke suppressed.
func (w *Workspace) releaseWindowDragButton(event experience.Event) {
	if event.Kind == experience.PointerUp {
		delete(w.windowDragButtons, applicationButton(event))
	}
}

func (w *Workspace) ownWindowDragButton(button uint32) {
	if button == 0 {
		return
	}
	if w.windowDragButtons == nil {
		w.windowDragButtons = make(map[uint32]bool)
	}
	w.windowDragButtons[button] = true
}

func (w *Workspace) handleWindowDrag(event experience.Event) bool {
	active := w.pointer.kind == captureApplicationPlacement && w.pointer.windowDrag
	if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel {
		owned := len(w.windowDragButtons) != 0
		w.windowDragButtons = nil
		if active {
			w.clearApplicationFocus()
			w.cancelPointer()
		}
		// Keyboard cancellation must continue to the other shortcut trackers.
		return event.Kind == experience.PointerCancel && (active || owned)
	}
	button := applicationButton(event)
	ownedRelease := event.Kind == experience.PointerUp && w.windowDragButtons[button]
	w.releaseWindowDragButton(event)
	if active {
		w.layout(w.width, w.height)
		if w.viewport != w.pointer.dragViewport {
			w.cancelPointer()
			active = false
		} else {
			switch event.Kind {
			case experience.PointerDown:
				w.ownWindowDragButton(button)
				return true
			case experience.PointerMove:
				if !w.prepareContentWindowDrag(event) {
					return true
				}
				w.moveApplicationPlacement(event.X, event.Y)
				w.sampleWindowDrag(event, false)
				return true
			case experience.PointerScroll:
				if w.pointer.dragFromView {
					return true
				}
				w.preview(Action{Kind: MoveApplications, DeltaDepth: -event.ScrollY * .035})
				// Rebase on the shifted plane so the next motion cannot acquire
				// lateral displacement solely from a perspective depth change.
				w.syncScene()
				if hit, _, ok := w.scene.MapCapturedSurface(w.camera, w.viewport, event.X, event.Y, w.pointer.surface); ok {
					w.pointer.lastWorld = hit.Point
				}
				w.pointer.dragSampleCount = 0
				w.sampleWindowDrag(event, false)
				return true
			case experience.PointerUp:
				if button == 272 {
					if w.prepareContentWindowDrag(event) {
						w.moveApplicationPlacement(event.X, event.Y)
						w.sampleWindowDrag(event, true)
					}
					if w.pointer.kind != captureNone {
						w.releaseWindowDrag()
					}
				}
				return true
			case experience.KeyInput:
				if event.Pressed && !event.Repeat && event.Modifiers == 0 && (event.Key == experience.KeyEscape || event.Keycode == 1) {
					w.cancelPointer()
					w.helpKeys[helpStroke(event)] = true
					return true
				}
			}
		}
	}
	if ownedRelease {
		return true
	}
	if len(w.windowDragButtons) > 0 {
		switch event.Kind {
		case experience.PointerDown:
			w.ownWindowDragButton(button)
			return true
		case experience.PointerMove, experience.PointerScroll, experience.PointerUp:
			return true
		}
	}
	view := w.m.applicationState
	if w.application.ID == 0 || len(w.applicationButtons) != 0 || w.pointer.kind != captureNone {
		return false
	}
	if event.Kind != experience.PointerDown && event.Kind != experience.PointerMove && event.Kind != experience.PointerScroll {
		return false
	}
	surface, handle := w.applicationDragTarget(event.X, event.Y)
	if !handle && !view.Reading && !view.Overview && event.Kind == experience.PointerDown && button == 272 && event.Modifiers == experience.ModSuper {
		if hit, ok := w.applicationHit(event.X, event.Y); ok {
			surface = w.applicationForNode(hit.Node)
		}
	}
	if surface.ID == 0 || (view.Reading || view.Overview) && !surface.DragContent {
		return false
	}
	if surface.DragContent && (view.Overview || view.Placing) && event.Kind == experience.PointerDown && button == 272 && event.Modifiers.Has(experience.ModShift) {
		return false // Preserve the workspace's additive selection gesture.
	}
	if event.Kind != experience.PointerDown {
		// Hovering a workspace handle releases a client's custom cursor but
		// does not change its keyboard focus. Scrolling it never zooms behind it.
		w.clearApplicationHover()
		return true
	}
	w.stopWindowThrowForKey(surface.Key)
	w.ownWindowDragButton(button)
	if button != 272 {
		return true
	}
	w.cancelPointer()
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	w.startWindowDrag(surface, event)
	return true
}

func (w *Workspace) startWindowDrag(surface experience.ApplicationSurface, event experience.Event) {
	view := w.m.applicationState
	w.beginApplicationPlacement(surface, event.X, event.Y)
	if w.pointer.kind == captureApplicationPlacement {
		index := w.m.applicationState.index(surface.Key)
		if view.Selected&(1<<index) == 0 {
			// Include target selection in the gesture's single undo record.
			w.preview(Action{Kind: SelectApplication, ApplicationKey: surface.Key})
		} else if view.Active != surface.Key {
			// Moving another selected member preserves the whole selection.
			d := w.Document()
			d.View.Application.Active = surface.Key
			d.View.Application.aliases()
			w.install(d, false)
		}
		w.pointer.windowSelection = w.m.applicationState.movementSelection()
		w.stopWindowThrows(w.pointer.windowSelection)
		if surface.DragContent && (view.Reading || view.Overview) {
			if inverse, ok := w.scene.Node(w.pointer.surface).Transform.Inverse(); ok {
				w.pointer.dragFromView = true
				w.pointer.dragGrab = inverse.TransformPoint(w.pointer.lastWorld)
			}
		}
		w.initializeWindowDrag(event)
	}
}

// A photo can be picked directly from Read or Overview. Cross into Space only
// once an actual drag starts, keeping the grabbed image point under the cursor.
// Its display size can change with the view, but the camera never follows the
// moving photo and the gesture remains one reversible placement operation.
func (w *Workspace) prepareContentWindowDrag(event experience.Event) bool {
	p := &w.pointer
	if !p.dragFromView {
		return p.kind == captureApplicationPlacement
	}
	dx, dy := (event.X-p.pressX)/p.scale, (event.Y-p.pressY)/p.scale
	if dx*dx+dy*dy < 16 {
		return false
	}
	p.dragFromView, p.dragViewChanged = false, true
	d := w.Document()
	d.View.Application.Reading, d.View.Application.Overview, d.View.Application.Placing = false, false, false
	w.install(d, false)
	w.syncScene()
	hit, _, ok := w.scene.MapCapturedSurface(w.camera, w.viewport, event.X, event.Y, p.surface)
	if !ok {
		w.cancelPointer()
		return false
	}
	grab := w.scene.Node(p.surface).Transform.TransformPoint(p.dragGrab)
	right, up, _ := applicationBasis()
	delta := hit.Point.Sub(grab)
	w.preview(Action{Kind: MoveApplications, DeltaX: delta.Dot(right), DeltaY: delta.Dot(up)})
	p.lastWorld, p.dragSampleCount = hit.Point, 0
	w.sampleWindowDrag(event, false)
	return true
}
