package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/scene"
)

var (
	_ experience.Experience   = (*Workspace)(nil)
	_ experience.Stateful     = (*Workspace)(nil)
	_ experience.Demonstrator = (*Workspace)(nil)
)

type captureKind uint8

const (
	captureNone captureKind = iota
	captureButton
	captureOrbit
	captureOrbitPad
	captureTimeline
	captureSurface
	captureSurfaceButton
	captureSurfaceTimeline
	captureApplicationPlacement
	captureApplicationClose
	captureApplicationLaunch
	captureForgetClosedPlacements
	captureApplicationDock
	captureWorkspacePan
	captureApplicationResize
	captureApplicationWindowControl
	captureApplicationReadClick
)

type pointerCapture struct {
	kind                          captureKind
	start                         Document
	pressX, pressY, lastX, lastY  float32
	scale, ox, oy                 float32
	dragged                       bool
	buttonAction                  Action
	surface                       scene.NodeID
	lastWorld                     scene.Vec3
	applicationID                 uint64
	dockIndex                     int
	windowDrag                    bool
	windowSelection               uint32
	dragFromView, dragViewChanged bool
	dragGrab                      scene.Vec3
	dragViewport                  scene.Viewport
	dragSamples                   [32]windowDragSample
	dragSampleCount               int
	panHorizontal, panVertical    scene.Vec3
	resizeKey                     string
	resizeWidth, resizeHeight     int
	resizeRatioX, resizeRatioY    float32
	resizeWorldX, resizeWorldY    float32
	resizeAnchor                  bool
	resizeButton                  uint32
	readClick                     bool
	readDouble                    bool
	readKey                       string
	readTime                      uint32
	readX, readY                  float32
}

func (p pointerCapture) mask() fields {
	switch p.kind {
	case captureOrbit, captureOrbitPad, captureWorkspacePan:
		return fieldCamera
	case captureTimeline, captureSurfaceTimeline:
		return fieldTime | fieldPlaying
	case captureApplicationPlacement, captureApplicationResize:
		return fieldApplicationLayout
	}
	return 0
}

func (w *Workspace) Handle(event experience.Event) bool {
	if event.Kind == experience.TextCommit || event.Kind == experience.TextPreedit {
		state := w.TextInput()
		if !state.Enabled || state.ContextID != event.TextContext {
			return true
		}
	}

	w.syncApplications()
	w.observeApplicationReadInterruption(event)
	if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel ||
		event.Kind == experience.PointerDown && w.commands != nil && w.commands.open {
		w.resetApplicationReadClick()
	}
	if w.handleCommands(event) {
		return true
	}
	if event.Kind == experience.PointerCancel || event.Kind == experience.KeyboardCancel {
		w.finishWindowThrow()
	}
	if w.handleHelp(event) {
		w.releaseWindowDragButton(event)
		return true
	}
	// A client-owned data drag retains its pointer grab over every workspace
	// overlay, including the fixed app rail and scene rotation pad.
	if w.handleApplicationDrag(event) {
		return true
	}
	// A held Read click keeps its grab, while fixed overlays get first refusal
	// for new presses so an application behind the rail/pad/portal cannot steal
	// their input through scene picking.
	if w.pointer.kind == captureApplicationReadClick && w.handleApplicationReadGesture(event) {
		return true
	}
	if w.handleOrbitPad(event) {
		return true
	}
	if w.handleApplicationDock(event) {
		return true
	}
	if w.handlePortalNavigation(event) {
		return true
	}
	if w.handleApplicationReadGesture(event) {
		return true
	}
	// The Super+wheel window gesture also owns window chrome. Route it before
	// ordinary control hover so scrolling over a minimize/maximize/close plate
	// still changes the hovered window's depth.
	if w.handleWindowDepthWheel(event) {
		return true
	}
	if w.handleApplicationWindowControl(event) {
		return true
	}
	// Escape belongs to an active pointer gesture before it can stop an
	// unrelated coasting window. Otherwise a held resize or workspace pan can
	// be stranded when another window is still moving under inertia.
	if w.windowThrow != nil && !w.applicationKeyboard && w.pointer.kind == captureNone && event.Kind == experience.KeyInput && event.Pressed && !event.Repeat && event.Modifiers == 0 && (event.Key == experience.KeyEscape || event.Keycode == 1) {
		w.finishWindowThrow()
		w.helpKeys[helpStroke(event)] = true
		return true
	}
	if w.handleWindowDrag(event) {
		return true
	}
	if w.handleHeldOverviewKey(event) {
		return true
	}
	if w.handleOverviewShortcut(event) {
		return true
	}
	if w.handleTerminalShortcut(event) {
		return true
	}
	if w.handleOverviewNavigation(event) {
		return true
	}
	if w.handleApplicationControl(event) {
		return true
	}
	if w.handleApplication(event) {
		return true
	}
	if w.handleCameraScroll(event) {
		return true
	}
	switch event.Kind {
	case experience.KeyInput:
		return w.handleKey(event)
	case experience.PointerCancel, experience.KeyboardCancel:
		return w.cancelPointer()
	case experience.PointerDown:
		if event.Button != experience.ButtonPrimary {
			return false
		}
		w.cancelPointer()
		x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
		p := pointerCapture{start: w.Document(), pressX: x, pressY: y, lastX: x, lastY: y, scale: w.scale, ox: w.ox, oy: w.oy}
		if !w.desktop && (box{297, 803, 1095, 63}).contains(x, y) {
			p.kind = captureTimeline
		} else if w.desktop && event.Modifiers == experience.ModSuper && w.inViewport(x, y) {
			p.kind = captureWorkspacePan
			p.panHorizontal, p.panVertical = w.workspacePanVectors()
		} else if !w.desktop && w.inViewport(x, y) {
			p.kind = captureOrbit
			if hit, ok := w.surfaceHit(event.X, event.Y); ok {
				p.kind = captureSurface
				if panelChart.contains(hit.PixelX, hit.PixelY) {
					p.kind = captureSurfaceTimeline
				} else if action, ok := panelAction(hit.PixelX, hit.PixelY); ok {
					p.kind, p.buttonAction = captureSurfaceButton, action
				}
			}
		} else if action, ok := w.buttonAction(x, y); ok {
			p.kind = captureButton
			p.buttonAction = action
		} else {
			return false
		}
		w.pointer = p
		if p.kind == captureTimeline {
			w.preview(Action{Kind: SeekTime, Seconds: float64((x-307)/1075) * duration})
		}
		if p.kind == captureSurfaceTimeline {
			w.scrubSurface(event.X, event.Y)
		}
		return true
	case experience.PointerMove:
		if w.pointer.kind == captureNone {
			return false
		}
		w.movePointer(event.X, event.Y)
		return true
	case experience.PointerUp:
		if event.Button != experience.ButtonPrimary || w.pointer.kind == captureNone {
			return false
		}
		w.movePointer(event.X, event.Y)
		p := w.pointer
		x, y := p.lastX, p.lastY
		w.commitPointer()
		if !p.dragged {
			if p.kind == captureButton {
				if action, ok := w.buttonAction(x, y); ok && action == p.buttonAction {
					_ = w.Dispatch(action)
				}
			}
			if p.kind == captureOrbit {
				if action, ok := w.pickAction(event.X, event.Y); ok {
					_ = w.Dispatch(action)
				}
			}
			if p.kind == captureSurfaceButton {
				if hit, ok := w.surfaceHit(event.X, event.Y); ok {
					if action, ok := panelAction(hit.PixelX, hit.PixelY); ok && action == p.buttonAction {
						_ = w.Dispatch(action)
					}
				}
			}
		}
		return true
	}
	return false
}

// Overview remains reachable while an application owns the keyboard. Consume
// the complete reserved key stroke, including a release after its modifiers
// have changed or the user has clicked an application again.
func (w *Workspace) handleOverviewShortcut(event experience.Event) bool {
	if event.Kind == experience.KeyboardCancel {
		w.overviewShortcutHeld = false
	}
	if event.Kind != experience.KeyInput {
		return false
	}
	if w.overviewShortcutHeld && ((w.overviewShortcutKey != 0 && event.Keycode == w.overviewShortcutKey) ||
		(w.overviewShortcutKey == 0 && event.Key == experience.KeyO)) {
		if !event.Pressed {
			w.overviewShortcutHeld = false
		}
		return true
	}
	if event.Key != experience.KeyO || event.Modifiers != experience.ModControl|experience.ModAlt ||
		w.application.ID == 0 && len(w.visibleApplications()) == 0 {
		return false
	}
	if event.Pressed && !event.Repeat {
		w.overviewShortcutHeld, w.overviewShortcutKey = true, event.Keycode
		w.cancelPointer()
		_ = w.Dispatch(Action{Kind: ToggleApplicationOverview})
	}
	return true
}

func (w *Workspace) movePointer(px, py float32) {
	p := &w.pointer
	x, y := (px-p.ox)/p.scale, (py-p.oy)/p.scale
	if abs(x-p.pressX)+abs(y-p.pressY) > 4 {
		p.dragged = true
	}
	if p.kind == captureTimeline {
		w.preview(Action{Kind: SeekTime, Seconds: float64((x-307)/1075) * duration})
	}
	if p.kind == captureSurfaceTimeline {
		w.scrubSurface(px, py)
	}
	if (p.kind == captureOrbit || p.kind == captureOrbitPad) && p.dragged {
		w.preview(Action{Kind: OrbitCamera, DeltaX: x - p.lastX, DeltaY: y - p.lastY})
	}
	if p.kind == captureWorkspacePan && p.dragged {
		delta := p.panHorizontal.Mul(x - p.lastX).Add(p.panVertical.Mul(y - p.lastY))
		w.preview(Action{Kind: PanCamera, DeltaX: delta.X, DeltaY: delta.Y, DeltaDepth: delta.Z})
	}
	p.lastX, p.lastY = x, y
}

// workspacePanVectors map one design-space pointer unit onto the saved fixed
// application basis. Moving the camera opposite a horizontal drag and along a
// downward drag's screen-up direction makes scene content follow the pointer,
// independent of the current orbit and zoom.
func (w *Workspace) workspacePanVectors() (horizontal, vertical scene.Vec3) {
	forward := w.camera.Target.Sub(w.camera.Eye).Normalize()
	right := forward.Cross(w.camera.Up).Normalize()
	up := right.Cross(forward).Normalize()
	distance := w.camera.Eye.Sub(w.camera.Target).Length()
	fov := w.camera.FOV
	if fov <= 0 || fov >= math.Pi {
		fov = math.Pi / 4
	}
	perDesignUnit := float32(2*math.Tan(float64(fov)/2)) * distance * w.scale / w.viewport.Height
	worldHorizontal := right.Mul(-perDesignUnit)
	worldVertical := up.Mul(perDesignUnit)
	fixedRight, fixedUp, fixedNormal := applicationBasis()
	return scene.Vec3{X: worldHorizontal.Dot(fixedRight), Y: worldHorizontal.Dot(fixedUp), Z: worldHorizontal.Dot(fixedNormal)},
		scene.Vec3{X: worldVertical.Dot(fixedRight), Y: worldVertical.Dot(fixedUp), Z: worldVertical.Dot(fixedNormal)}
}

// A gesture applies live semantic previews, then produces one edit at release.
// Capture keeps its starting coordinate conversion through a host resize.
func (w *Workspace) preview(action Action) {
	if next, err := reduce(w.Document(), action); err == nil {
		w.install(next, false)
	}
}
func (w *Workspace) commitPointer() {
	if w.pointer.kind == captureNone {
		return
	}
	if w.pointer.kind == captureApplicationReadClick {
		w.resetApplicationReadClick()
		w.windowDragButtons = nil
	}
	before, after := w.pointer.start, w.Document()
	switch w.pointer.kind {
	case captureApplicationPlacement:
		before = windowDragBefore(w.pointer, after)
	case captureApplicationResize:
		before = windowResizeBefore(w.pointer, after)
	}
	w.record(before, after, w.pointer.mask())
	w.pointer = pointerCapture{}
}
func (w *Workspace) cancelPointer() bool {
	if w.pointer.kind == captureNone {
		return false
	}
	if w.pointer.kind == captureApplicationReadClick {
		w.resetApplicationReadClick()
		w.windowDragButtons = nil
	}
	switch w.pointer.kind {
	case captureApplicationPlacement:
		w.install(windowDragBefore(w.pointer, w.Document()), false)
	case captureApplicationResize:
		w.install(windowResizeBefore(w.pointer, w.Document()), false)
	default:
		w.install(merge(w.Document(), w.pointer.start, w.pointer.mask()), false)
	}
	w.pointer = pointerCapture{}
	return true
}

func (w *Workspace) inViewport(x, y float32) bool {
	px, py := w.ox+x*w.scale, w.oy+y*w.scale
	return px > w.viewport.X && px < w.viewport.X+w.viewport.Width && py > w.viewport.Y && py < w.viewport.Y+w.viewport.Height
}

func (w *Workspace) buttonAction(x, y float32) (Action, bool) {
	if w.application.ID != 0 {
		switch {
		case applicationOverviewButton.contains(x, y):
			return Action{Kind: ToggleApplicationOverview}, true
		case applicationPlaceButton.contains(x, y):
			return Action{Kind: ToggleApplicationPlacement}, true
		case applicationBackButton.contains(x, y):
			return Action{Kind: MoveApplications, DeltaDepth: -.5}, true
		case applicationFrontButton.contains(x, y):
			return Action{Kind: MoveApplications, DeltaDepth: .5}, true
		case applicationGroupButton.contains(x, y):
			return Action{Kind: GroupApplications}, true
		case applicationUngroupButton.contains(x, y):
			return Action{Kind: UngroupApplications}, true
		case applicationDepthButton.contains(x, y):
			return Action{Kind: ToggleApplicationDepth}, true
		case applicationReadButton.contains(x, y):
			return Action{Kind: ToggleApplicationReading}, true
		case applicationSizeButton.contains(x, y):
			return Action{Kind: ToggleApplicationSize}, true
		}
	}
	switch {
	case cinematicButton.contains(x, y):
		return Action{Kind: SetPresentation, Presentation: presentation.Cinematic}, true
	case adaptiveButton.contains(x, y):
		return Action{Kind: SetPresentation, Presentation: presentation.Adaptive}, true
	case motionButton.contains(x, y):
		return Action{Kind: ToggleReducedMotion}, true
	case (box{1110, 30, 123, 37}).contains(x, y):
		if w.desktop {
			return Action{Kind: ResetView}, true
		}
		return Action{Kind: ResetStudy}, true
	case (box{1247, 30, 151, 37}).contains(x, y):
		if w.application.ID != 0 {
			return Action{Kind: ToggleApplicationReading}, true
		}
		if w.desktop {
			return Action{}, false
		}
		return Action{Kind: ToggleFocus}, true
	case !w.desktop && (box{42, 810, 103, 43}).contains(x, y):
		return Action{Kind: TogglePlayback}, true
	case !w.desktop && w.application.ID == 0 && !w.m.focused && (box{32, 600, 208, 44}).contains(x, y):
		return Action{Kind: ToggleExplode}, true
	case !w.desktop && w.application.ID == 0 && !w.m.focused && panelDepthButton.contains(x, y):
		return Action{Kind: TogglePanelDepth}, true
	}
	if !w.desktop && !w.m.focused && w.application.ID == 0 {
		for i, id := range componentIDs {
			if (box{32, float32(307 + i*68), 208, 56}).contains(x, y) {
				return Action{Kind: SelectComponent, Component: id}, true
			}
		}
	}
	return Action{}, false
}

func (w *Workspace) surfaceHit(x, y float32) (scene.Hit, bool) {
	if w.panelNode == 0 {
		return scene.Hit{}, false
	}
	w.layout(w.width, w.height)
	w.syncScene()
	hit, ok := w.scene.Pick(w.camera, w.viewport, x, y)
	return hit, ok && hit.Surface && hit.Node == w.panelNode
}

func (w *Workspace) scrubSurface(x, y float32) {
	// Capture follows this surface even if a moving mesh temporarily covers it
	// or the pointer leaves its bounds. The initial press still requires a
	// visible hit, so hidden controls never receive a new gesture.
	w.layout(w.width, w.height)
	w.syncScene()
	if hit, _, ok := w.scene.MapSurface(w.camera, w.viewport, x, y, w.panelNode); ok {
		w.preview(Action{Kind: SeekTime, Seconds: float64((hit.PixelX-panelChart.x)/panelChart.w) * duration})
	}
}

func (w *Workspace) pickAction(x, y float32) (Action, bool) {
	w.layout(w.width, w.height)
	w.syncScene()
	if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); ok {
		for i, id := range w.nodes {
			if hit.Node == id {
				return Action{Kind: SelectComponent, Component: componentIDs[i]}, true
			}
		}
	}
	return Action{}, false
}

func (w *Workspace) handleKey(event experience.Event) bool {
	if !event.Pressed || event.Modifiers.Has(experience.ModAlt) || event.Modifiers.Has(experience.ModSuper) {
		return false
	}
	if event.Repeat && event.Key != experience.KeyLeft && event.Key != experience.KeyRight {
		return false
	}
	var action Action
	if event.Modifiers.Has(experience.ModControl) {
		switch event.Key {
		case experience.KeyZ:
			action.Kind = Undo
			if event.Modifiers.Has(experience.ModShift) {
				action.Kind = Redo
			}
		case experience.KeyY:
			action.Kind = Redo
		default:
			return false
		}
	} else {
		switch event.Key {
		case experience.KeySpace:
			action.Kind = TogglePlayback
		case experience.KeyE:
			action.Kind = ToggleExplode
		case experience.KeyF:
			action.Kind = ToggleFocus
			if w.application.ID != 0 {
				action.Kind = ToggleApplicationReading
			}
		case experience.KeyP:
			action.Kind = TogglePresentation
			if event.Modifiers == experience.ModShift {
				action.Kind = ToggleReducedMotion
			}
		case experience.KeyB:
			action.Kind = TogglePanelDepth
			if w.application.ID != 0 {
				action.Kind = ToggleApplicationDepth
			}
		case experience.KeyR:
			action.Kind = ResetStudy
			if w.desktop {
				action.Kind = ResetView
			}
		case experience.KeyO:
			// A minimized window intentionally leaves no current application,
			// but Overview is also the recovery path for that collapsed window.
			if w.application.ID == 0 && len(w.visibleApplications()) == 0 {
				return false
			}
			action.Kind = ToggleApplicationOverview
		case experience.Key1:
			action = Action{Kind: SelectComponent, Component: Housing}
		case experience.Key2:
			action = Action{Kind: SelectComponent, Component: Rotor}
		case experience.Key3:
			action = Action{Kind: SelectComponent, Component: Shaft}
		case experience.KeyLeft:
			action = Action{Kind: StepTime, Seconds: -0.5}
		case experience.KeyRight:
			action = Action{Kind: StepTime, Seconds: 0.5}
		case experience.KeyEscape:
			if w.m.applicationState.Overview {
				w.cancelPointer()
				action.Kind = ToggleApplicationOverview
				break
			}
			if w.cancelPointer() {
				return true
			}
			action.Kind = ResetView
		default:
			return false
		}
	}
	return w.Dispatch(action) == nil
}
