package nativeui

import (
	"fmt"
	sdkui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
	"image/color"
)

// SetSkin shares the public SDK's recipe renderer while keeping the native
// shaped-text engine, editing state, selection, and IME composition intact.
// Control allocation remains owned by each application.
func (p *Painter) SetSkin(selected skin.Skin) error {
	if p == nil || p.closed {
		return fmt.Errorf("native UI painter is closed")
	}
	theme, err := sdkui.ThemeFromSkin(selected)
	if err != nil {
		return err
	}
	painter, err := sdkui.NewPainter(theme)
	if err != nil {
		return err
	}
	rgba := func(token string) color.RGBA { return color.RGBAModel.Convert(selected.Color(token)).(color.RGBA) }
	next := p.Theme
	next.Background, next.Surface, next.Hover = rgba("background"), rgba("surface"), rgba("hover")
	next.Accent, next.Border, next.Text = rgba("accent"), rgba("border"), rgba("text")
	next.Muted, next.Selection, next.Disabled = rgba("muted"), rgba("selection"), rgba("disabled")
	// Existing domain text sizes/fonts may encode document grids. Shared
	// controls use the skin typography within their existing hit allocations.
	if p.skinPainter != nil {
		_ = p.skinPainter.Close()
	}
	p.Theme, p.skinPainter = next, painter
	p.controlFont, p.controlFontSize = selected.Typography.Family, selected.Typography.Size
	return nil
}

// LegacySkin adapts the older independent palette/silhouette preference for
// providers that draw full skin recipes. It also resets any imported recipe.
func LegacySkin(family, shape string) (skin.Skin, error) {
	switch family {
	case "instrument", "aperture", "glass", "telemetry":
	default:
		return skin.Skin{}, fmt.Errorf("unknown legacy theme family %q", family)
	}
	geometry := shape
	switch shape {
	case "chamfered", "bracketed", "notched":
	case "slab":
		geometry = "rect"
	default:
		return skin.Skin{}, fmt.Errorf("unknown legacy theme shape %q", shape)
	}
	selected, err := skin.Builtin(family)
	if err != nil {
		return skin.Skin{}, err
	}
	legacy, err := sdkui.Builtin(sdkui.Family(family), sdkui.ShapeGrammar(shape))
	if err != nil {
		return skin.Skin{}, err
	}
	selected.Typography = skin.Typography{Family: legacy.Typography.Font, Size: float64(legacy.Typography.Size), LineHeight: float64(legacy.Typography.LineHeight), Weight: legacy.Typography.Weight}
	for token, value := range map[string]color.RGBA{
		"background": legacy.Palette.Background, "surface": legacy.Palette.Surface, "raised": legacy.Palette.Raised, "hover": legacy.Palette.Hover, "pressed": legacy.Palette.Pressed,
		"accent": legacy.Palette.Accent, "accent-alt": legacy.Palette.AccentAlt, "border": legacy.Palette.Border, "text": legacy.Palette.Text, "muted": legacy.Palette.Muted,
		"selection": legacy.Palette.Selection, "disabled": legacy.Palette.Disabled, "success": legacy.Palette.Success, "warning": legacy.Palette.Warning, "danger": legacy.Palette.Danger,
	} {
		c := color.NRGBAModel.Convert(value).(color.NRGBA)
		selected.Palette[token] = skin.Color(fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A))
	}
	for name, recipe := range selected.Controls {
		for i := range recipe.Layers {
			recipe.Layers[i].Geometry = skin.Geometry{Kind: geometry, Corner: .15, Notch: .15}
		}
		selected.Controls[name] = recipe
	}
	return selected, selected.Validate()
}
