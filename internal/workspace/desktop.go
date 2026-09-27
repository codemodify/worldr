package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/codemodify/worldr/internal/presentation"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// The desktop has its own persistence contract and experience identity. Shared
// camera/application reducers still use Document internally, but no synthetic
// study timeline, selection, geometry or instrument is part of desktop state.
type environmentSettings struct {
	DNA           bool `json:"dna"`
	Cat           bool `json:"cat"`
	CatCollisions bool `json:"cat_collisions"`
	Eyes          bool `json:"eyes"`
}

func defaultEnvironmentSettings() environmentSettings {
	return environmentSettings{DNA: true, Cat: true, CatCollisions: true, Eyes: true}
}

type desktopDocument struct {
	Version       int                    `json:"version"`
	Camera        CameraState            `json:"camera"`
	Presentation  presentation.Mode      `json:"presentation"`
	ReducedMotion bool                   `json:"reduced_motion,omitempty"`
	Applications  ApplicationViewState   `json:"applications"`
	Environment   environmentSettings    `json:"environment"`
	Windows       windowSettings         `json:"windows"`
	Themes        controlThemeSettings   `json:"themes"`
	Skin          *skin.Skin             `json:"skin,omitempty"`
	Navigation    *navigationPreferences `json:"navigation,omitempty"`
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
	return marshalDesktopState(w.Document(), w.environment, w.windows, w.controlTheme, w.activeSkin, w.savedNavigationPreferences())
}

func marshalDesktopState(d Document, environment environmentSettings, windows windowSettings, themes controlThemeSettings, selected *skin.Skin, navigation *navigationPreferences) ([]byte, error) {
	if selected != nil {
		if err := selected.Validate(); err != nil {
			return nil, err
		}
	}
	if err := navigation.validate(); err != nil {
		return nil, err
	}
	d.View.Presentation = presentation.Cinematic
	d.View.ReducedMotion = false
	windows.Border = windows.Border.normalized()
	themes = themes.normalized()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := windows.validate(); err != nil {
		return nil, err
	}
	if err := themes.validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(desktopDocument{
		Version:       1,
		Camera:        d.View.Camera,
		Presentation:  d.View.Presentation,
		ReducedMotion: d.View.ReducedMotion,
		Applications:  d.View.Application,
		Environment:   environment,
		Windows:       windows,
		Themes:        themes,
		Skin:          selected,
		Navigation:    navigation,
	}, "", "  ")
}

func (w *Workspace) loadDesktopState(data []byte) error {
	base := initialModel().document()
	base.Timeline.Playing = false
	d := desktopDocument{Camera: base.View.Camera, Presentation: presentation.Cinematic, Environment: defaultEnvironmentSettings(), Windows: defaultWindowSettings(), Themes: defaultControlThemeSettings()}
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
	if err := d.Navigation.validate(); err != nil {
		return err
	}
	if err := d.Windows.validate(); err != nil {
		return fmt.Errorf("window settings: %w", err)
	}
	if err := d.Themes.validate(); err != nil {
		return fmt.Errorf("native control themes: %w", err)
	}
	if d.Skin != nil {
		if err := d.Skin.Validate(); err != nil {
			return fmt.Errorf("workspace skin: %w", err)
		}
	}
	base.View.Camera, base.View.Presentation = d.Camera, presentation.Cinematic
	base.View.ReducedMotion, base.View.Application = false, d.Applications
	// Reject malformed documents against the historical contract before the
	// compatibility migration adjusts otherwise-valid placements for the wall.
	if err := base.Validate(); err != nil {
		return err
	}
	constrainEnergyWallPlacements(&base.View.Application)
	if err := base.Validate(); err != nil {
		return err
	}
	w.installLoadedDocument(base)
	w.installNavigationPreferences(d.Navigation)
	w.environment = d.Environment
	w.windows = d.Windows
	w.controlTheme = d.Themes.normalized()
	w.activeSkin = d.Skin
	w.skinPreview.dirty = true
	w.publishControlTheme()
	w.publishSkin()
	return nil
}
