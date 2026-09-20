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
	d.View.Presentation = presentation.Cinematic
	d.View.ReducedMotion = false
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
	base.View.Camera, base.View.Presentation = d.Camera, presentation.Cinematic
	base.View.ReducedMotion, base.View.Application = false, d.Applications
	if err := base.Validate(); err != nil {
		return err
	}
	w.installLoadedDocument(base)
	return nil
}

func (w *Workspace) drawDesktop() {
	if w.application.ID != 0 {
		return
	}
	w.text(410, 318, 36, "A place for your next idea.", ink, 1)
	w.text(412, 372, 16, "Bring your tools together. Keep the whole picture in view.", muted, 1)
	w.text(412, 407, 13, "Choose Launcher on the right to open a tool or space.", teal, .9)
}
