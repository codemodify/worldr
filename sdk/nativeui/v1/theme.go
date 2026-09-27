package nativeui

import (
	"fmt"
	"image/color"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// Family identifies a built-in semantic palette. Family names are stable SDK
// values; applications should persist the value rather than a slice index.
type Family string

const (
	Instrument Family = "instrument"
	Aperture   Family = "aperture"
	Glass      Family = "glass"
	Telemetry  Family = "telemetry"
)

// ShapeGrammar selects the silhouette used by controls independently of color.
type ShapeGrammar string

const (
	Chamfered ShapeGrammar = "chamfered"
	Bracketed ShapeGrammar = "bracketed"
	Slab      ShapeGrammar = "slab"
	Notched   ShapeGrammar = "notched"
	Rounded   ShapeGrammar = "rounded"
)

var families = [...]Family{Instrument, Aperture, Glass, Telemetry}
var grammars = [...]ShapeGrammar{Chamfered, Bracketed, Slab, Notched}

// Families returns an owned list in the recommended presentation order.
func Families() []Family { return append([]Family(nil), families[:]...) }

// ShapeGrammars returns an owned list in the recommended presentation order.
func ShapeGrammars() []ShapeGrammar { return append([]ShapeGrammar(nil), grammars[:]...) }

func (f Family) valid() bool {
	for _, candidate := range families {
		if f == candidate {
			return true
		}
	}
	return false
}

func (g ShapeGrammar) valid() bool {
	if g == Rounded {
		return true
	}
	for _, candidate := range grammars {
		if g == candidate {
			return true
		}
	}
	return false
}

// Palette gives controls semantic colors. Applications can derive a Theme and
// adjust these values without changing control behavior or geometry.
type Palette struct {
	Background color.RGBA
	Surface    color.RGBA
	Raised     color.RGBA
	Hover      color.RGBA
	Pressed    color.RGBA
	Accent     color.RGBA
	AccentAlt  color.RGBA
	Border     color.RGBA
	Text       color.RGBA
	Muted      color.RGBA
	Selection  color.RGBA
	Disabled   color.RGBA
	Success    color.RGBA
	Warning    color.RGBA
	Danger     color.RGBA
}

// Metrics controls density and silhouette scale in framebuffer pixels.
type Metrics struct {
	Padding       int
	Gap           int
	ControlHeight int
	Stroke        int
	Corner        int
	Notch         int
	Icon          int
}

// Typography selects scalable embedded Sans or Monospace fonts. Other family
// names fall back to Sans; richer system shaping remains a backend capability.
type Typography struct {
	Font       string
	Size       int
	LineHeight int
	Weight     int
}

// Theme is a complete, copyable control presentation. Shape can be changed
// independently with WithShape.
type Theme struct {
	Family     Family
	Shape      ShapeGrammar
	Palette    Palette
	Metrics    Metrics
	Typography Typography
	// Skin supplies extensible component recipes. Nil retains the legacy painter.
	Skin *skin.Skin
}

// RGB constructs an opaque sRGB color from 0xRRGGBB.
func RGB(value uint32) color.RGBA {
	return color.RGBA{R: byte(value >> 16), G: byte(value >> 8), B: byte(value), A: 255}
}

// RGBA constructs an sRGB color from 0xRRGGBBAA.
func RGBA(value uint32) color.RGBA {
	return color.RGBAModel.Convert(color.NRGBA{R: byte(value >> 24), G: byte(value >> 16), B: byte(value >> 8), A: byte(value)}).(color.RGBA)
}

// Builtin returns one validated built-in theme. Palette and shape are separate
// parameters so a user can compare geometry without changing color language.
func Builtin(family Family, shape ShapeGrammar) (Theme, error) {
	if !family.valid() {
		return Theme{}, fmt.Errorf("native UI: unknown theme family %q", family)
	}
	if !shape.valid() {
		return Theme{}, fmt.Errorf("native UI: unknown shape grammar %q", shape)
	}
	metrics := Metrics{Padding: 8, Gap: 7, ControlHeight: 30, Stroke: 1, Corner: 5, Notch: 8, Icon: 16}
	typography := Typography{Font: "Worldr Portable", Size: 13, LineHeight: 17}
	var palette Palette
	switch family {
	case Aperture:
		palette = Palette{
			Background: RGB(0x050d16), Surface: RGB(0x0b1c29), Raised: RGB(0x112c3b), Hover: RGB(0x173c4d), Pressed: RGB(0x0d5969),
			Accent: RGB(0xb9f7ff), AccentAlt: RGB(0x5cb6d1), Border: RGB(0x407a8c), Text: RGB(0xe4f4f7), Muted: RGB(0x83a8b3),
			Selection: RGB(0x174f61), Disabled: RGB(0x4a5e65), Success: RGB(0x79e0b4), Warning: RGB(0xf1be68), Danger: RGB(0xff7184),
		}
		metrics.Corner, metrics.Notch = 2, 5
	case Glass:
		palette = Palette{
			Background: RGB(0x07111d), Surface: RGBA(0x17394bda), Raised: RGBA(0x28546add), Hover: RGBA(0x376f83e8), Pressed: RGBA(0x275e79f0),
			Accent: RGB(0xd4fbff), AccentAlt: RGB(0xb398ff), Border: RGBA(0x8bdceaac), Text: RGB(0xf0fbff), Muted: RGB(0xa6c0cc),
			Selection: RGBA(0x5aa6c5a8), Disabled: RGB(0x64747d), Success: RGB(0x8ae5bd), Warning: RGB(0xf5cd84), Danger: RGB(0xff8495),
		}
		metrics.Corner, metrics.Stroke = 3, 1
	case Telemetry:
		palette = Palette{
			Background: RGB(0x071016), Surface: RGB(0x10242c), Raised: RGB(0x18343c), Hover: RGB(0x204953), Pressed: RGB(0x155e67),
			Accent: RGB(0x58eff3), AccentAlt: RGB(0xffa63d), Border: RGB(0x3e7780), Text: RGB(0xe7f1ef), Muted: RGB(0x86aaa8),
			Selection: RGB(0x245d63), Disabled: RGB(0x566663), Success: RGB(0x6fe1aa), Warning: RGB(0xffb247), Danger: RGB(0xff596d),
		}
		metrics.Notch, metrics.Gap = 10, 6
	default: // Instrument
		palette = Palette{
			Background: RGB(0x091823), Surface: RGB(0x153847), Raised: RGB(0x1a4455), Hover: RGB(0x245366), Pressed: RGB(0x17677a),
			Accent: RGB(0x8bebf3), AccentAlt: RGB(0x35a9c6), Border: RGB(0x34788b), Text: RGB(0xd7e9ef), Muted: RGB(0x86a8b5),
			Selection: RGB(0x285d71), Disabled: RGB(0x51636d), Success: RGB(0x6bd9a8), Warning: RGB(0xf2b95d), Danger: RGB(0xff667d),
		}
	}
	theme := Theme{Family: family, Shape: shape, Palette: palette, Metrics: metrics, Typography: typography}
	return theme, theme.Validate()
}

// FromControlTheme resolves a host preference delivered through native-app v1.
func FromControlTheme(preference nativeapp.ControlTheme) (Theme, error) {
	if err := preference.Validate(); err != nil {
		return Theme{}, err
	}
	return Builtin(Family(preference.Family), ShapeGrammar(preference.Shape))
}

// ControlTheme returns the portable preference represented by this theme.
func (t Theme) ControlTheme() nativeapp.ControlTheme {
	return nativeapp.ControlTheme{Family: string(t.Family), Shape: string(t.Shape)}
}

// DefaultTheme returns the established Instrument palette and Chamfered shape.
func DefaultTheme() Theme {
	theme, _ := Builtin(Instrument, Chamfered)
	return theme
}

// WithShape returns a validated copy using another built-in shape grammar.
func (t Theme) WithShape(shape ShapeGrammar) (Theme, error) {
	t.Shape = shape
	return t, t.Validate()
}

// Validate rejects values that would create ambiguous or unbounded drawing.
func (t Theme) Validate() error {
	if t.Skin != nil {
		if err := t.Skin.Validate(); err != nil {
			return err
		}
	} else if !t.Family.valid() {
		return fmt.Errorf("native UI: unknown theme family %q", t.Family)
	}
	if !t.Shape.valid() {
		return fmt.Errorf("native UI: unknown shape grammar %q", t.Shape)
	}
	m := t.Metrics
	if m.Padding < 0 || m.Padding > 64 || m.Gap < 0 || m.Gap > 64 || m.ControlHeight < 12 || m.ControlHeight > 256 || m.Stroke < 1 || m.Stroke > 16 || m.Corner < 0 || m.Corner > 64 || m.Notch < 0 || m.Notch > 64 || m.Icon < 6 || m.Icon > 128 {
		return fmt.Errorf("native UI: theme metrics are outside supported bounds")
	}
	ty := t.Typography
	if ty.Font == "" || len(ty.Font) > 128 || ty.Size < 6 || ty.Size > 128 || ty.LineHeight < ty.Size || ty.LineHeight > 256 {
		return fmt.Errorf("native UI: typography is outside supported bounds")
	}
	colors := [...]color.RGBA{t.Palette.Background, t.Palette.Surface, t.Palette.Raised, t.Palette.Hover, t.Palette.Pressed, t.Palette.Accent, t.Palette.AccentAlt, t.Palette.Border, t.Palette.Text, t.Palette.Muted, t.Palette.Selection, t.Palette.Disabled, t.Palette.Success, t.Palette.Warning, t.Palette.Danger}
	for _, c := range colors {
		if c.A == 0 && t.Skin == nil {
			return fmt.Errorf("native UI: semantic colors must not be fully transparent")
		}
		if c.R > c.A || c.G > c.A || c.B > c.A {
			return fmt.Errorf("native UI: semantic colors must use premultiplied alpha")
		}
	}
	return nil
}
