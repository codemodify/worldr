package nativeui

import (
	"image"
	"image/draw"
)

// PanelContent reserves the same title rail and padding as DrawPanel. Surface
// content remains application-owned; these controls do not prescribe a window
// rectangle or prevent their use on a spatial application surface.
func (p *Painter) PanelContent(bounds image.Rectangle) image.Rectangle {
	return image.Rect(bounds.Min.X+p.Theme.Padding, bounds.Min.Y+p.Theme.ControlHeight+2*p.Theme.Padding, bounds.Max.X-p.Theme.Padding, bounds.Max.Y-p.Theme.Padding).Intersect(bounds)
}
func (p *Painter) DrawPanel(dst *image.RGBA, bounds image.Rectangle, title string, focused bool) error {
	if dst == nil || bounds.Empty() {
		return nil
	}
	draw.Draw(dst, bounds, image.NewUniform(p.Theme.Background), image.Point{}, draw.Src)
	accent := p.Theme.Border
	if focused {
		accent = p.Theme.Accent
	}
	cut := p.Theme.CornerCut
	draw.Draw(dst, image.Rect(bounds.Min.X+cut, bounds.Min.Y, bounds.Max.X-cut, bounds.Min.Y+1), image.NewUniform(accent), image.Point{}, draw.Src)
	header := image.Rect(bounds.Min.X+p.Theme.Padding, bounds.Min.Y+p.Theme.Padding, bounds.Max.X-p.Theme.Padding, bounds.Min.Y+p.Theme.Padding+p.Theme.ControlHeight)
	if err := p.DrawLabel(dst, header, title, p.Theme.Text); err != nil {
		return err
	}
	y := header.Max.Y + p.Theme.Padding/2
	draw.Draw(dst, image.Rect(header.Min.X, y, header.Max.X, y+1), image.NewUniform(p.Theme.Border), image.Point{}, draw.Src)
	return nil
}
func (p *Painter) DrawMenu(dst *image.RGBA, nodes []Node, focusedID string) error {
	for _, node := range nodes {
		if err := p.DrawButton(dst, node, node.ID == focusedID, node.Selected); err != nil {
			return err
		}
	}
	return nil
}
