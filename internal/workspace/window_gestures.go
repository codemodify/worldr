package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

func (w *Workspace) windowGestureTarget(x, y float32) (experience.ApplicationSurface, bool) {
	if surface, ok := w.applicationResizeTarget(x, y); ok {
		return surface, true
	}
	if surface, _, _, ok := w.applicationWindowControlAt(x, y); ok {
		return surface, true
	}
	if surface, ok := w.applicationDragTarget(x, y); ok {
		return surface, true
	}
	if hit, ok := w.applicationHit(x, y); ok {
		surface := w.applicationForNode(hit.Node)
		return surface, surface.ID != 0
	}
	return experience.ApplicationSurface{}, false
}

// A resize preview owns only one placement. Build its historical starting
// point on top of the current document so independent window momentum, newly
// registered surfaces and other live changes are never rewound by cancel,
// checkpoint or undo.
func windowResizeBefore(pointer pointerCapture, current Document) Document {
	startIndex := pointer.start.View.Application.index(pointer.resizeKey)
	currentIndex := current.View.Application.index(pointer.resizeKey)
	if startIndex < 0 || currentIndex < 0 {
		return current
	}
	start := pointer.start.View.Application.Layouts[startIndex]
	target := &current.View.Application.Layouts[currentIndex]
	target.X, target.Y = start.X, start.Y
	target.Width, target.Height, target.Wide = start.Width, start.Height, start.Wide
	target.Maximized = start.Maximized
	target.RestoreWidth, target.RestoreHeight, target.RestoreWide = start.RestoreWidth, start.RestoreHeight, start.RestoreWide
	current.View.Application.aliases()
	return current
}

func (w *Workspace) applicationResizeTarget(x, y float32) (experience.ApplicationSurface, bool) {
	if len(w.applicationResizeHandles) == 0 {
		return experience.ApplicationSurface{}, false
	}
	w.layout(w.width, w.height)
	w.syncScene()
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	if !ok {
		return experience.ApplicationSurface{}, false
	}
	for _, surface := range w.applicationSurfaces {
		if w.applicationResizeHandles[surface.ID] == hit.Node {
			return surface, true
		}
	}
	return experience.ApplicationSurface{}, false
}

// Super+wheel changes the hovered window's spatial depth without moving
// keyboard focus or replacing the user's current selection. A grouped window
// carries its explicit group, while unrelated coasting windows keep moving.
func (w *Workspace) handleWindowDepthWheel(event experience.Event) bool {
	view := w.m.applicationState
	if event.Kind != experience.PointerScroll || event.Modifiers != experience.ModSuper || event.ScrollY == 0 ||
		math.IsNaN(float64(event.ScrollY)) || math.IsInf(float64(event.ScrollY), 0) || w.pointer.kind != captureNone ||
		len(w.applicationButtons) != 0 || len(w.windowDragButtons) != 0 || view.Reading || view.Overview || view.Placing {
		return false
	}
	surface, ok := w.windowGestureTarget(event.X, event.Y)
	if !ok {
		return false
	}
	_ = w.Dispatch(Action{Kind: MoveApplication, ApplicationKey: surface.Key, DeltaDepth: -event.ScrollY * .035})
	return true
}

func (w *Workspace) handleWindowResize(event experience.Event) bool {
	p := &w.pointer
	active := p.kind == captureApplicationResize
	if active {
		if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel {
			w.cancelPointer()
			if event.Kind == experience.PointerCancel {
				w.windowDragButtons = nil
				return true
			}
			return false
		}
		w.layout(w.width, w.height)
		if w.viewport != p.dragViewport {
			w.cancelPointer()
			active = false
		} else {
			button := applicationButton(event)
			switch event.Kind {
			case experience.PointerDown:
				w.ownWindowDragButton(button)
				return true
			case experience.PointerMove:
				w.previewWindowResize(event.X, event.Y)
				return true
			case experience.PointerScroll:
				return true
			case experience.PointerUp:
				w.releaseWindowDragButton(event)
				if button == p.resizeButton {
					w.previewWindowResize(event.X, event.Y)
					w.commitPointer()
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
	button := applicationButton(event)
	if active || event.Kind != experience.PointerDown ||
		w.application.ID == 0 || w.pointer.kind != captureNone || len(w.applicationButtons) != 0 || len(w.windowDragButtons) != 0 {
		return false
	}
	view := w.m.applicationState
	if view.Reading || view.Overview || view.Placing {
		return false
	}
	surface, target := experience.ApplicationSurface{}, false
	switch {
	case button == 272 && event.Modifiers == 0:
		surface, target = w.applicationResizeTarget(event.X, event.Y)
	case button == 273 && event.Modifiers == experience.ModSuper:
		surface, target = w.windowGestureTarget(event.X, event.Y)
	}
	if !target {
		return false
	}
	// Passive photo content deliberately keeps its source aspect and minimal
	// partial frame; it has no window-resize chrome or client layout to reflow.
	if surface.DragContent {
		return false
	}
	i := view.index(surface.Key)
	if i < 0 || view.Layouts[i].Minimized {
		return false
	}
	width, height := applicationLogicalSize(view.Layouts[i])
	ratioX, ratioY := w.applicationResizeRatios(surface, width, height)
	_, _, worldWidth, worldHeight := w.applicationTransformFor(surface)
	w.stopWindowThrowForKey(surface.Key)
	w.clearApplicationFocus()
	w.applicationRestoreKey = ""
	w.pointer = pointerCapture{
		kind: captureApplicationResize, start: w.Document(), surface: w.applicationNodes[surface.ID],
		pressX: event.X, pressY: event.Y, lastX: event.X, lastY: event.Y, scale: w.scale, dragViewport: w.viewport,
		resizeKey: surface.Key, resizeWidth: width, resizeHeight: height, resizeRatioX: ratioX, resizeRatioY: ratioY,
		resizeWorldX: worldWidth / float32(width), resizeWorldY: worldHeight / float32(height),
		resizeAnchor: !surface.Frameless, resizeButton: button,
	}
	w.ownWindowDragButton(button)
	return true
}

func (w *Workspace) applicationResizeRatios(surface experience.ApplicationSurface, width, height int) (float32, float32) {
	w.layout(w.width, w.height)
	w.syncScene()
	node := w.scene.Node(w.applicationNodes[surface.ID])
	if node == nil {
		return 1, 1
	}
	minX, minY := float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY := -minX, -minY
	for _, point := range []scene.Vec3{{X: -.5, Y: -.5}, {X: .5, Y: -.5}, {X: .5, Y: .5}, {X: -.5, Y: .5}} {
		x, y, _, _ := w.camera.Project(node.Transform.TransformPoint(point), w.viewport)
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	if maxX-minX < 1 || maxY-minY < 1 {
		return 1, 1
	}
	return float32(width) / (maxX - minX), float32(height) / (maxY - minY)
}

func (w *Workspace) previewWindowResize(x, y float32) {
	p := &w.pointer
	if p.kind != captureApplicationResize {
		return
	}
	dx, dy := x-p.pressX, y-p.pressY
	if dx*dx+dy*dy < 16 && !p.dragged {
		return
	}
	p.dragged = true
	width := max(minApplicationWidth, min(maxApplicationWidth, p.resizeWidth+int(math.Round(float64(dx*p.resizeRatioX)))))
	height := max(minApplicationHeight, min(maxApplicationHeight, p.resizeHeight+int(math.Round(float64(dy*p.resizeRatioY)))))
	action := Action{Kind: ResizeApplication, ApplicationKey: p.resizeKey, Width: width, Height: height}
	if p.resizeAnchor {
		i := w.m.applicationState.index(p.resizeKey)
		if i < 0 {
			return
		}
		currentWidth, currentHeight := applicationLogicalSize(w.m.applicationState.Layouts[i])
		// Move the center by half the physical size delta. The top-left stays
		// fixed, so the visible bottom-right corner follows the resize pointer.
		action.DeltaX = float32(width-currentWidth) * p.resizeWorldX / 2
		action.DeltaY = -float32(height-currentHeight) * p.resizeWorldY / 2
	}
	w.preview(action)
	p.lastX, p.lastY = x, y
}
