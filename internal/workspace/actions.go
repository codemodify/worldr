package workspace

import (
	"fmt"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/presentation"
)

type ActionKind string

const (
	CreateSpace                ActionKind = "create-space"
	RenameSpace                ActionKind = "rename-space"
	SwitchSpace                ActionKind = "switch-space"
	MoveToSpace                ActionKind = "move-to-space"
	NavigatePortal             ActionKind = "navigate-portal"
	TogglePlayback             ActionKind = "toggle-playback"
	SetPlayback                ActionKind = "set-playback"
	ToggleExplode              ActionKind = "toggle-explode"
	SetExploded                ActionKind = "set-exploded"
	ToggleFocus                ActionKind = "toggle-focus"
	SetFocused                 ActionKind = "set-focused"
	TogglePresentation         ActionKind = "toggle-presentation"
	SetPresentation            ActionKind = "set-presentation"
	ToggleReducedMotion        ActionKind = "toggle-reduced-motion"
	SetReducedMotion           ActionKind = "set-reduced-motion"
	TogglePanelDepth           ActionKind = "toggle-panel-depth"
	ToggleApplicationDepth     ActionKind = "toggle-application-depth"
	ToggleApplicationReading   ActionKind = "toggle-application-reading"
	ToggleApplicationSize      ActionKind = "toggle-application-size"
	SelectApplication          ActionKind = "select-application"
	ToggleApplicationOverview  ActionKind = "toggle-application-overview"
	ToggleApplicationPlacement ActionKind = "toggle-application-placement"
	MoveApplications           ActionKind = "move-applications"
	GroupApplications          ActionKind = "group-applications"
	UngroupApplications        ActionKind = "ungroup-applications"
	ForgetClosedPlacements     ActionKind = "forget-closed-placements"
	SelectComponent            ActionKind = "select-component"
	SeekTime                   ActionKind = "seek-time"
	StepTime                   ActionKind = "step-time"
	OrbitCamera                ActionKind = "orbit-camera"
	ZoomCamera                 ActionKind = "zoom-camera"
	ResetStudy                 ActionKind = "reset-study"
	ResetView                  ActionKind = "reset-view"
	Undo                       ActionKind = "undo"
	Redo                       ActionKind = "redo"
)

// Action is the semantic command boundary used by UI hit targets, normalized
// key bindings, automation, and demonstrations. Only the payload named by Kind
// is read: a space/name or application key for navigation, Component, Seconds
// (absolute or step), movement/orbit deltas, Enabled, or Presentation.
type Action struct {
	Space          uint8
	SpaceName      string
	Kind           ActionKind
	Component      ComponentID
	Seconds        float64
	DeltaX, DeltaY float32
	Enabled        bool
	Presentation   presentation.Mode
	ApplicationKey string
	Additive       bool
	DeltaDepth     float32
	DeltaZoom      float32
	// ClosedPlacements is derived from live providers by Dispatch.
	ClosedPlacements uint32
}

type fields uint16

const (
	fieldSelection fields = 1 << iota
	fieldTime
	fieldPlaying
	fieldExploded
	fieldFocused
	fieldCamera
	fieldPresentation
	fieldPanelDepth
	fieldApplicationDepth
	fieldApplicationReading
	fieldApplicationSize
	fieldApplicationLayout
	fieldReducedMotion
	allFields = fieldSelection | fieldTime | fieldPlaying | fieldExploded | fieldFocused | fieldCamera | fieldPresentation | fieldPanelDepth | fieldApplicationDepth | fieldApplicationReading | fieldApplicationSize | fieldApplicationLayout | fieldReducedMotion
)

type edit struct {
	id            uint64
	before, after Document
	fields        fields
	forgetClosed  bool
}

const historyLimit = 128

func difference(a, b Document) fields {
	var f fields
	if a.Selection != b.Selection {
		f |= fieldSelection
	}
	if a.Timeline.Seconds != b.Timeline.Seconds {
		f |= fieldTime
	}
	if a.Timeline.Playing != b.Timeline.Playing {
		f |= fieldPlaying
	}
	if a.View.Exploded != b.View.Exploded {
		f |= fieldExploded
	}
	if a.View.Focused != b.View.Focused {
		f |= fieldFocused
	}
	if a.View.Camera != b.View.Camera {
		f |= fieldCamera
	}
	if a.View.Presentation != b.View.Presentation {
		f |= fieldPresentation
	}
	if a.View.ReducedMotion != b.View.ReducedMotion {
		f |= fieldReducedMotion
	}
	if a.View.PanelBehind != b.View.PanelBehind {
		f |= fieldPanelDepth
	}
	if a.View.Application.Behind != b.View.Application.Behind {
		f |= fieldApplicationDepth
	}
	if a.View.Application.Reading != b.View.Application.Reading {
		f |= fieldApplicationReading
	}
	if a.View.Application.Wide != b.View.Application.Wide {
		f |= fieldApplicationSize
	}
	if a.View.Application != b.View.Application {
		f |= fieldApplicationLayout
	}
	return f
}

// Merge only the fields touched by an edit. Undoing a component selection must
// not rewind a clock that continued playing while the user was inspecting it.
func merge(current, source Document, mask fields) Document {
	if mask&fieldSelection != 0 {
		current.Selection = source.Selection
	}
	if mask&fieldTime != 0 {
		current.Timeline.Seconds = source.Timeline.Seconds
	}
	if mask&fieldPlaying != 0 {
		current.Timeline.Playing = source.Timeline.Playing
	}
	if mask&fieldExploded != 0 {
		current.View.Exploded = source.View.Exploded
	}
	if mask&fieldFocused != 0 {
		current.View.Focused = source.View.Focused
	}
	if mask&fieldCamera != 0 {
		current.View.Camera = source.View.Camera
	}
	if mask&fieldPresentation != 0 {
		current.View.Presentation = source.View.Presentation
	}
	if mask&fieldReducedMotion != 0 {
		current.View.ReducedMotion = source.View.ReducedMotion
	}
	if mask&fieldPanelDepth != 0 {
		current.View.PanelBehind = source.View.PanelBehind
	}
	if mask&fieldApplicationDepth != 0 {
		current.View.Application.Behind = source.View.Application.Behind
	}
	if mask&fieldApplicationReading != 0 {
		current.View.Application.Reading = source.View.Application.Reading
	}
	if mask&fieldApplicationSize != 0 {
		current.View.Application.Wide = source.View.Application.Wide
	}
	if mask&fieldApplicationLayout != 0 {
		// Live clients may register new stable keys between an edit and its
		// undo/cancel. Restore the edited view while retaining those new slots.
		registered := current.View.Application.Layouts
		current.View.Application = source.View.Application
		for _, placement := range registered {
			if placement.Key == "" || current.View.Application.index(placement.Key) >= 0 {
				continue
			}
			for i, slot := range current.View.Application.Layouts {
				if slot.Key == "" {
					current.View.Application.Layouts[i] = placement
					break
				}
			}
		}
	}
	return current
}

func (w *Workspace) record(before, after Document, allowed fields) uint64 {
	changed := difference(before, after) & allowed
	if changed == 0 {
		return 0
	}
	return w.appendEdit(edit{before: before, after: after, fields: changed})
}

func (w *Workspace) appendEdit(entry edit) uint64 {
	w.nextEditID++
	entry.id = w.nextEditID
	w.history = w.history[:w.historyPosition]
	w.history = append(w.history, entry)
	if len(w.history) > historyLimit {
		copy(w.history, w.history[len(w.history)-historyLimit:])
		w.history = w.history[:historyLimit]
	}
	w.historyPosition = len(w.history)
	return w.nextEditID
}

func (w *Workspace) CanUndo() bool { return w.windowThrow != nil || w.historyPosition > 0 }
func (w *Workspace) CanRedo() bool { return w.windowThrow == nil && w.historyPosition < len(w.history) }

// Dispatch validates before mutation, commits one undoable domain edit, and
// never records playback ticks or presentation interpolation as user edits.
func (w *Workspace) Dispatch(action Action) error {
	if w.desktop && studyAction(action.Kind) {
		return fmt.Errorf("action %q belongs to the AXIAL study", action.Kind)
	}
	if action.Kind == ForgetClosedPlacements {
		w.finishWindowThrow()
		return w.forgetClosedPlacements()
	}
	if action.Kind == Undo || action.Kind == Redo {
		w.finishWindowThrow()
		w.cancelPointer()
		if action.Kind == Undo && w.CanUndo() {
			entry := w.history[w.historyPosition-1]
			next, err := w.restoreEdit(entry, true)
			if err != nil {
				return err
			}
			w.historyPosition--
			w.install(next, false)
		}
		if action.Kind == Redo && w.CanRedo() {
			entry := w.history[w.historyPosition]
			next, err := w.restoreEdit(entry, false)
			if err != nil {
				return err
			}
			w.historyPosition++
			w.install(next, false)
		}
		return nil
	}
	// Validate first so an invalid command cannot even commit an active gesture.
	if _, err := reduce(w.Document(), action); err != nil {
		return err
	}
	switch action.Kind {
	case SelectApplication:
		w.stopWindowThrowForKey(action.ApplicationKey)
	case MoveApplications, ToggleApplicationDepth, GroupApplications, UngroupApplications, MoveToSpace:
		w.stopWindowThrows(w.m.applicationState.movementSelection())
	case ToggleApplicationSize:
		w.stopWindowThrowForKey(w.m.applicationState.Active)
	case OrbitCamera, ZoomCamera, TogglePresentation, SetPresentation, SwitchSpace, CreateSpace, RenameSpace, NavigatePortal:
		// Navigation and presentation do not own any window's momentum.
	default:
		w.finishWindowThrow()
	}
	switch action.Kind {
	case SelectApplication, ToggleApplicationDepth, ToggleApplicationReading, ToggleApplicationSize, ToggleApplicationOverview, ToggleApplicationPlacement, MoveApplications, GroupApplications, UngroupApplications, SwitchSpace, MoveToSpace, NavigatePortal:
		w.applicationRestoreKey = ""
	}
	w.commitPointer()
	before := w.Document()
	after, err := reduce(before, action)
	if err != nil {
		return err
	}
	w.install(after, false)
	w.record(before, after, allFields)
	return nil
}

func reduce(d Document, a Action) (Document, error) {
	switch a.Kind {
	case CreateSpace, RenameSpace, SwitchSpace, MoveToSpace:
		if err := reduceSpace(&d, a); err != nil {
			return d, err
		}
	case NavigatePortal:
		if err := reducePortalNavigation(&d, a); err != nil {
			return d, err
		}
	case TogglePlayback:
		d.Timeline.Playing = !d.Timeline.Playing
	case SetPlayback:
		d.Timeline.Playing = a.Enabled
	case ToggleExplode:
		d.View.Exploded = !d.View.Exploded
	case SetExploded:
		d.View.Exploded = a.Enabled
	case ToggleFocus:
		d.View.Focused = !d.View.Focused
	case SetFocused:
		d.View.Focused = a.Enabled
	case TogglePresentation:
		d.View.Presentation = d.View.Presentation.Next()
	case SetPresentation:
		d.View.Presentation = a.Presentation
	case ToggleReducedMotion:
		d.View.ReducedMotion = !d.View.ReducedMotion
	case SetReducedMotion:
		d.View.ReducedMotion = a.Enabled
	case TogglePanelDepth:
		d.View.PanelBehind = !d.View.PanelBehind
	case ToggleApplicationDepth, ToggleApplicationReading, ToggleApplicationSize, SelectApplication, ToggleApplicationOverview, ToggleApplicationPlacement, MoveApplications, GroupApplications, UngroupApplications, ForgetClosedPlacements:
		if err := reduceApplicationView(&d.View.Application, a); err != nil {
			return d, err
		}
	case SelectComponent:
		if _, ok := componentIndex(a.Component); !ok {
			return d, fmt.Errorf("unknown component %q", a.Component)
		}
		d.Selection = a.Component
	case SeekTime, StepTime:
		if !finite(a.Seconds) {
			return d, fmt.Errorf("timeline command must be finite")
		}
		t := a.Seconds
		if a.Kind == StepTime {
			t += d.Timeline.Seconds
		}
		d.Timeline.Seconds = math.Max(0, math.Min(duration, t))
		d.Timeline.Playing = false
	case OrbitCamera:
		if !finite(float64(a.DeltaX)) || !finite(float64(a.DeltaY)) {
			return d, fmt.Errorf("orbit command must be finite")
		}
		d.View.Camera.Yaw = float32(math.Remainder(float64(d.View.Camera.Yaw)+float64(a.DeltaX)*0.008, 2*math.Pi))
		d.View.Camera.Pitch = float32(math.Max(-1.1, math.Min(1.1, float64(d.View.Camera.Pitch)+float64(a.DeltaY)*0.006)))
	case ZoomCamera:
		if !finite(float64(a.DeltaZoom)) {
			return d, fmt.Errorf("zoom command must be finite")
		}
		d.View.Camera.Zoom = float32(math.Max(-.8, math.Min(.8, float64(d.View.Camera.Zoom)+float64(a.DeltaZoom))))
	case ResetStudy:
		mode := d.View.Presentation
		reducedMotion := d.View.ReducedMotion
		applications := d.View.Application
		d = initialModel().document()
		d.View.Presentation = mode
		d.View.ReducedMotion = reducedMotion
		d.View.Application = applications
	case ResetView:
		d.View.Focused = false
		d.View.Camera = initialModel().document().View.Camera
		d.View.Application.Overview, d.View.Application.Placing, d.View.Application.Reading = false, false, false
	default:
		return d, fmt.Errorf("unknown action %q", a.Kind)
	}
	return d, d.Validate()
}

func (w *Workspace) Demo(elapsed time.Duration) {
	if w.desktop {
		return
	}
	step := int(elapsed / (2 * time.Second))
	if step < 0 {
		return
	}
	if step < w.demoStep {
		w.demoStep = 0
	}
	sequence := [...]Action{{Kind: ToggleExplode}, {Kind: SelectComponent, Component: Housing}, {Kind: SelectComponent, Component: Shaft}, {Kind: ToggleFocus}, {Kind: ToggleFocus}, {Kind: ResetStudy}}
	for w.demoStep < step {
		_ = w.Dispatch(sequence[w.demoStep%len(sequence)])
		w.demoStep++
	}
}
