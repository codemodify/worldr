// Package nativeui supplies shared native controls, shaped labels, editable
// fields and a semantic focus tree. It is independent of a display protocol;
// an application retains its own pixels and exposes the semantic snapshot to
// the host. Controller, Painter and Field calls belong to one host goroutine.
package nativeui

import "image/color"

type Theme struct {
	Background, Surface, Hover, Accent, Border color.RGBA
	Text, Muted, Selection, Disabled           color.RGBA
	Font                                       string
	FontSize                                   float64
	Padding, ControlHeight, CornerCut          int
}

func Color(rgb uint32) color.RGBA {
	return color.RGBA{R: byte(rgb >> 16), G: byte(rgb >> 8), B: byte(rgb), A: 255}
}

// Cinematic returns a value, so applications can adapt scale without mutating
// a global palette or drifting their shared focus/selection language.
func Cinematic() Theme {
	return Theme{Background: Color(0x091823), Surface: Color(0x153847), Hover: Color(0x245366), Accent: Color(0x8bebf3), Border: Color(0x34788b), Text: Color(0xd7e9ef), Muted: Color(0x86a8b5), Selection: Color(0x285d71), Disabled: Color(0x51636d), Font: "Sans", FontSize: 14, Padding: 7, ControlHeight: 28, CornerCut: 4}
}
