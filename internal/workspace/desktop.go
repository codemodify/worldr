package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/codemodify/worldr/internal/presentation"
)

// The desktop has its own persistence contract and experience identity. Shared
// camera/application reducers still use Document internally, but no synthetic
// study timeline, selection, geometry or instrument is part of desktop state.
type desktopDocument struct {
	Version       int                  `json:"version"`
	Camera        CameraState          `json:"camera"`
	Presentation  presentation.Mode    `json:"presentation"`
	ReducedMotion bool                 `json:"reduced_motion,omitempty"`
	Applications  ApplicationViewState `json:"applications"`
}

func studyAction(kind ActionKind) bool {
	switch kind {
	case TogglePlayback, SetPlayback, ToggleExplode, SetExploded, ToggleFocus, SetFocused,
		TogglePanelDepth, SelectComponent, SeekTime, StepTime, ResetStudy:
		return true
	}
	return false
}

func (w *Workspace) saveDesktopState() ([]byte, error) {
	return marshalDesktopState(w.Document())
}

func marshalDesktopState(d Document) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(desktopDocument{1, d.View.Camera, d.View.Presentation, d.View.ReducedMotion, d.View.Application}, "", "  ")
}

func (w *Workspace) loadDesktopState(data []byte) error {
	base := initialModel().document()
	base.Timeline.Playing = false
	d := desktopDocument{Camera: base.View.Camera, Presentation: presentation.Cinematic}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return fmt.Errorf("decode workspace document: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("workspace document must contain exactly one JSON value")
	}
	if d.Version != 1 {
		return fmt.Errorf("unsupported workspace document version %d", d.Version)
	}
	base.View.Camera, base.View.Presentation = d.Camera, d.Presentation
	base.View.ReducedMotion, base.View.Application = d.ReducedMotion, d.Applications
	if err := base.Validate(); err != nil {
		return err
	}
	w.installLoadedDocument(base)
	return nil
}

func (w *Workspace) drawDesktop() {
	view := w.m.applicationState
	label := "SPACE / YOUR TOOLS"
	switch {
	case view.Overview:
		label = "OVERVIEW / FIND A WINDOW"
	case view.Placing:
		label = "ARRANGE / DRAG TO PLACE"
	case view.Reading:
		label = "READ / FOCUSED WORK"
	}
	w.text(275, 134, 12, label, muted, 1)
	w.text(1210, 134, 12, fmt.Sprintf("%02d WINDOWS", len(w.visibleApplications())), teal, .9)
	if w.application.ID != 0 {
		w.drawApplicationControls()
		w.text(275, 810, 12, shortApplicationTitle(w.application.Title), ink, 1)
		if index := view.index(w.application.Key); index >= 0 && !view.Reading && !view.Overview {
			p := view.Layouts[index]
			w.text(905, 810, 11, fmt.Sprintf("POSITION  %+.1f / %+.1f    DEPTH  %+.1f", p.X, p.Y, p.Depth), muted, .9)
		}
	} else {
		w.text(42, 134, 12, "WORKSPACE", muted, 1)
		w.text(42, 170, 21, "Ready when you are", ink, 1)
		w.text(42, 214, 12, "Open a native tool from the APPS rail on the right.", muted, 1)
		w.text(42, 238, 12, "Your tools share this space.", muted, 1)
		w.text(410, 318, 36, "A place for your next idea.", ink, 1)
		w.text(412, 372, 16, "Bring your tools together. Keep the whole picture in view.", muted, 1)
		w.text(412, 407, 13, "Choose Files, Terminal, media, models, research or notes to begin.", teal, .9)
	}
	w.line(275, 792, 1388, 792, 1, muted, .22)
	dragHint := "Move: top grip / Super+primary · Resize: corner grip / Super+secondary · Depth: Super+wheel"
	if w.application.DragContent {
		dragHint = "Move the photo by dragging it · Super+wheel changes depth"
	}
	w.text(275, 840, 12, dragHint, muted, 1)
	w.drawHelpButton()
	if w.applicationKeyboard {
		w.text(42, 878, 11, "Keyboard sent to application", teal, .9)
	} else {
		w.text(42, 878, 11, "Ctrl+Alt+Enter  new terminal", muted, .9)
	}
	w.text(307, 878, 11, "Ctrl+Z  undo     Ctrl+Alt+O  overview", muted, .9)
	w.drawPortalButton()
	w.text(1180, 878, 11, "Ctrl+Alt+Q  quit", muted, .9)
}
