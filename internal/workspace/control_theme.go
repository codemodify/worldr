package workspace

import "fmt"

// controlThemeSettings selects the visual language used by native controls.
// It lives beside the window-border preference rather than in Document because
// changing appearance is not a workspace edit and must never enter Undo.
type controlThemeSettings struct {
	Family controlThemeFamily `json:"family"`
	Shape  controlShape       `json:"shape"`
}

type controlThemeFamily string

const (
	controlThemeInstrument controlThemeFamily = "instrument"
	controlThemeAperture   controlThemeFamily = "aperture"
	controlThemeGlass      controlThemeFamily = "glass"
	controlThemeTelemetry  controlThemeFamily = "telemetry"
)

type controlShape string

const (
	controlShapeChamfered controlShape = "chamfered"
	controlShapeBracketed controlShape = "bracketed"
	controlShapeSlab      controlShape = "slab"
	controlShapeNotched   controlShape = "notched"
)

func defaultControlThemeSettings() controlThemeSettings {
	return controlThemeSettings{Family: controlThemeInstrument, Shape: controlShapeChamfered}
}

func (s controlThemeSettings) normalized() controlThemeSettings {
	defaults := defaultControlThemeSettings()
	if s.Family == "" {
		s.Family = defaults.Family
	}
	if s.Shape == "" {
		s.Shape = defaults.Shape
	}
	return s
}

func (s controlThemeSettings) validate() error {
	s = s.normalized()
	if !s.Family.valid() {
		return fmt.Errorf("unknown native control theme %q", s.Family)
	}
	if !s.Shape.valid() {
		return fmt.Errorf("unknown native control shape %q", s.Shape)
	}
	return nil
}

func (f controlThemeFamily) valid() bool {
	switch f {
	case controlThemeInstrument, controlThemeAperture, controlThemeGlass, controlThemeTelemetry:
		return true
	}
	return false
}

func (s controlShape) valid() bool {
	switch s {
	case controlShapeChamfered, controlShapeBracketed, controlShapeSlab, controlShapeNotched:
		return true
	}
	return false
}

type controlThemeChoice struct {
	family             controlThemeFamily
	title, description string
}

var controlThemeChoices = [...]controlThemeChoice{
	{controlThemeInstrument, "INSTRUMENT", "LAYERED / ENGINEERING"},
	{controlThemeAperture, "APERTURE", "OPEN / LIGHTWEIGHT"},
	{controlThemeGlass, "GLASS", "LUMINOUS / QUIET"},
	{controlThemeTelemetry, "TELEMETRY", "DENSE / ASYMMETRIC"},
}

type controlShapeChoice struct {
	shape              controlShape
	title, description string
}

var controlShapeChoices = [...]controlShapeChoice{
	{controlShapeChamfered, "CHAMFERED", "CUT CORNERS"},
	{controlShapeBracketed, "BRACKETED", "OPEN RAILS"},
	{controlShapeSlab, "SLAB", "SOLID PLANES"},
	{controlShapeNotched, "NOTCHED", "DATA TABS"},
}
