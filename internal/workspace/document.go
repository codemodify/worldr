package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/codemodify/worldr/internal/presentation"
)

const DocumentVersion = 1

type ComponentID string

const (
	Housing ComponentID = "housing"
	Rotor   ComponentID = "rotor"
	Shaft   ComponentID = "shaft"
)

var componentIDs = [...]ComponentID{Housing, Rotor, Shaft}

func componentIndex(id ComponentID) (int, bool) {
	for i, candidate := range componentIDs {
		if candidate == id {
			return i, true
		}
	}
	return 0, false
}

// Document contains user-owned state. GPU resources, node handles, animation
// interpolation, undo history, and in-progress gestures are deliberately absent.
// Component IDs survive changes in tree order; indices do not cross this API.
type Document struct {
	Version   int           `json:"version"`
	Selection ComponentID   `json:"selection"`
	Timeline  TimelineState `json:"timeline"`
	View      ViewState     `json:"view"`
}
type TimelineState struct {
	Seconds float64 `json:"seconds"`
	Playing bool    `json:"playing"`
}
type ViewState struct {
	Exploded      bool                 `json:"exploded"`
	Focused       bool                 `json:"focused"`
	Camera        CameraState          `json:"camera"`
	Presentation  presentation.Mode    `json:"presentation"`
	ReducedMotion bool                 `json:"reduced_motion,omitempty"`
	PanelBehind   bool                 `json:"panel_behind"`
	Application   ApplicationViewState `json:"application"`
}

// ApplicationViewState stores workspace presentation preferences, never live
// protocol IDs, client buffers, or keyboard focus.
type ApplicationViewState struct {
	Space    uint8              `json:"space,omitempty"`
	Spaces   [16]SpaceState     `json:"spaces,omitempty"`
	Behind   bool               `json:"behind"`
	Reading  bool               `json:"reading"`
	Wide     bool               `json:"wide"`
	Overview bool               `json:"overview,omitempty"`
	Placing  bool               `json:"placing,omitempty"`
	Active   string             `json:"active,omitempty"`
	Selected uint32             `json:"selected,omitempty"`
	Layouts  ApplicationLayouts `json:"layouts"`
}

const MaxApplicationLayouts = 32

// ApplicationPlacement uses a stable launch/window key, never a protocol ID.
// X/Y follow the workspace's fixed right/up basis; Depth points toward the
// initial camera. A nonzero Group moves its members together.
type ApplicationPlacement struct {
	Space uint8   `json:"space,omitempty"`
	Key   string  `json:"key,omitempty"`
	X     float32 `json:"x,omitempty"`
	Y     float32 `json:"y,omitempty"`
	Depth float32 `json:"depth,omitempty"`
	// Width and Height are the application's requested logical content size.
	// Zero keeps the Compact/Wide preset used by documents saved before free
	// resizing was introduced.
	Width  int   `json:"width,omitempty"`
	Height int   `json:"height,omitempty"`
	Wide   bool  `json:"wide,omitempty"`
	Group  uint8 `json:"group,omitempty"`
	// Minimized keeps the live surface available through Overview and portals.
	// Maximized and Restore* remain for compatibility with older saved documents;
	// current window chrome uses Read instead.
	Minimized     bool `json:"minimized,omitempty"`
	Maximized     bool `json:"maximized,omitempty"`
	RestoreWidth  int  `json:"restore_width,omitempty"`
	RestoreHeight int  `json:"restore_height,omitempty"`
	RestoreWide   bool `json:"restore_wide,omitempty"`
}
type CameraState struct {
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
	// Zoom is a logarithmic magnification; zero preserves the original camera.
	Zoom float32 `json:"zoom,omitempty"`
	// Target coordinates use the same fixed right/up/depth basis as application
	// placements. They let a camera visit a distant group without moving it.
	TargetX     float32 `json:"target_x,omitempty"`
	TargetY     float32 `json:"target_y,omitempty"`
	TargetDepth float32 `json:"target_depth,omitempty"`
}

type ApplicationLayouts [MaxApplicationLayouts]ApplicationPlacement

func (layouts *ApplicationLayouts) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("application layouts must be an array")
	}
	var values []ApplicationPlacement
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return err
	}
	if len(values) > MaxApplicationLayouts {
		return fmt.Errorf("at most %d application layouts are supported", MaxApplicationLayouts)
	}
	var next ApplicationLayouts
	copy(next[:], values)
	*layouts = next
	return nil
}

func (m model) document() Document {
	return Document{Version: DocumentVersion, Selection: componentIDs[m.selected], Timeline: TimelineState{Seconds: m.clock, Playing: m.playing}, View: ViewState{Exploded: m.exploded, Focused: m.focused, Camera: CameraState{Yaw: m.yaw, Pitch: m.pitch, Zoom: m.zoom, TargetX: m.cameraX, TargetY: m.cameraY, TargetDepth: m.cameraDepth}, Presentation: m.presentation, ReducedMotion: m.reducedMotion, PanelBehind: m.panelBehind, Application: m.applicationState}}
}

func (d Document) Validate() error {
	if d.Version != DocumentVersion {
		return fmt.Errorf("unsupported axial document version %d", d.Version)
	}
	if _, ok := componentIndex(d.Selection); !ok {
		return fmt.Errorf("unknown axial component %q", d.Selection)
	}
	if !finite(d.Timeline.Seconds) || d.Timeline.Seconds < 0 || d.Timeline.Seconds > duration {
		return fmt.Errorf("timeline must be finite and between 0 and %.0f seconds", duration)
	}
	if !finite(float64(d.View.Camera.Yaw)) || !finite(float64(d.View.Camera.Pitch)) || abs(d.View.Camera.Pitch) > 1.1 {
		return fmt.Errorf("invalid camera orientation")
	}
	if !finite(float64(d.View.Camera.Zoom)) || abs(d.View.Camera.Zoom) > .8 {
		return fmt.Errorf("camera zoom must be finite and between -0.8 and 0.8")
	}
	if !validCameraTarget(d.View.Camera) {
		return fmt.Errorf("camera target is out of bounds")
	}
	if !d.View.Presentation.Valid() {
		return fmt.Errorf("unknown presentation mode %q", d.View.Presentation)
	}
	return d.View.Application.validate()
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (w *Workspace) Document() Document { return w.m.document() }

func (w *Workspace) install(d Document, snap bool) {
	w.m.clock, w.m.playing = d.Timeline.Seconds, d.Timeline.Playing
	w.m.exploded, w.m.focused = d.View.Exploded, d.View.Focused
	w.m.presentation = d.View.Presentation
	w.m.reducedMotion = d.View.ReducedMotion
	w.m.panelBehind = d.View.PanelBehind
	previousApplications := w.m.applicationState
	w.m.applicationState = d.View.Application
	w.m.applicationBehind, w.m.applicationReading, w.m.applicationWide = d.View.Application.Behind, d.View.Application.Reading, d.View.Application.Wide
	w.installApplicationView(previousApplications)
	w.m.yaw, w.m.pitch = d.View.Camera.Yaw, d.View.Camera.Pitch
	w.m.zoom = d.View.Camera.Zoom
	w.m.cameraX, w.m.cameraY, w.m.cameraDepth = d.View.Camera.TargetX, d.View.Camera.TargetY, d.View.Camera.TargetDepth
	w.m.selected, _ = componentIndex(d.Selection)
	if snap || w.m.reducedMotion {
		w.presentationInitialized = false
		w.m.explosion = 0
		if w.m.exploded {
			w.m.explosion = 1
		}
	}
}

func (w *Workspace) SaveState() ([]byte, error) {
	w.finishWindowThrow()
	if w.desktop {
		return w.saveDesktopState()
	}
	d := w.Document()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(d, "", "  ")
}

// CheckpointState leaves active interaction and independent throws untouched.
// An unfinished drag/orbit is excluded from the snapshot, while released
// windows are captured at their current position without stopping their coast.
func (w *Workspace) CheckpointState() ([]byte, error) {
	d := w.Document()
	switch w.pointer.kind {
	case captureApplicationPlacement:
		d = windowDragBefore(w.pointer, d)
	case captureApplicationResize:
		d = windowResizeBefore(w.pointer, d)
	case captureNone:
	default:
		d = merge(d, w.pointer.start, w.pointer.mask())
	}
	if w.desktop {
		return marshalDesktopState(d)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(d, "", "  ")
}

// LoadState is transactional: invalid or trailing input leaves both the current
// document and undo history untouched. A successful load starts a new history.
func (w *Workspace) LoadState(data []byte) error {
	if w.desktop {
		return w.loadDesktopState(data)
	}
	// Older version 1 documents predate presentation and surface placement.
	// Defaults apply only to absent fields.
	d := Document{View: ViewState{Presentation: presentation.Cinematic, PanelBehind: true}}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return fmt.Errorf("decode axial document: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("axial document must contain exactly one JSON value")
	}
	if err := d.Validate(); err != nil {
		return err
	}
	w.installLoadedDocument(d)
	return nil
}

func (w *Workspace) installLoadedDocument(d Document) {
	w.windowThrow = nil
	w.resetApplicationReadClick()
	w.pointer = pointerCapture{}
	w.clearApplicationFocus()
	w.history = nil
	w.historyPosition = 0
	w.portals = portalNavigation{pressed: -1}
	w.install(d, true)
	w.applicationRestoreKey, w.applicationRestoreSelection = d.View.Application.Active, d.View.Application.Selected
	w.syncApplications()
	w.layout(w.width, w.height)
	w.syncScene()
}
