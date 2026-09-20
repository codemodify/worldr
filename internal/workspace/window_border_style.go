package workspace

import "fmt"

// windowBorderStyle is a desktop appearance preference. It deliberately stays
// outside Document: changing chrome must not create an undoable workspace edit.
type windowBorderStyle string

const (
	windowBorderAperture   windowBorderStyle = "aperture"
	windowBorderInstrument windowBorderStyle = "instrument"
	windowBorderGlass      windowBorderStyle = "glass"
	windowBorderTelemetry  windowBorderStyle = "telemetry"
)

type windowSettings struct {
	Border windowBorderStyle `json:"border"`
}

func defaultWindowSettings() windowSettings {
	return windowSettings{Border: windowBorderInstrument}
}

func (s windowBorderStyle) valid() bool {
	switch s {
	case windowBorderAperture, windowBorderInstrument, windowBorderGlass, windowBorderTelemetry:
		return true
	}
	return false
}

// normalized gives directly constructed test/workspace values the same default
// as decoded legacy state. Persisted values are still validated strictly.
func (s windowBorderStyle) normalized() windowBorderStyle {
	if s == "" {
		return windowBorderInstrument
	}
	return s
}

func (s windowSettings) validate() error {
	if !s.Border.valid() {
		return fmt.Errorf("unknown window border style %q", s.Border)
	}
	return nil
}

type windowBorderChoice struct {
	style              windowBorderStyle
	title, description string
}

var windowBorderChoices = [...]windowBorderChoice{
	{windowBorderAperture, "APERTURE", "OPEN CORNERS / LIGHT RAILS"},
	{windowBorderInstrument, "INSTRUMENT", "LAYERED CHASSIS / CURRENT"},
	{windowBorderGlass, "GLASS", "TRANSLUCENT EDGE / QUIET"},
	{windowBorderTelemetry, "TELEMETRY", "ASYMMETRIC DATA RAILS"},
}
