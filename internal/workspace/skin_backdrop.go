package workspace

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

//go:embed assets/merrick-silver.png
var sculptedSilverPNG []byte

func (w *Workspace) skinBackdropVisible() bool {
	if !w.desktop || w.activeSkin == nil {
		return false
	}
	switch w.activeSkin.Desktop.Backdrop {
	case "sculpted-silver", "quiet-gradient", "panel-field":
		return true
	}
	return false
}

// drawSkinBackdrop retains one immutable texture for the life of the workspace.
// An authored backdrop replaces ambient artwork without changing the user's
// environment preferences, which resume when another backdrop is selected.
func (w *Workspace) drawSkinBackdrop() bool {
	if !w.skinBackdropVisible() {
		return false
	}
	if w.panelFieldVisible() {
		w.drawPanelField()
		return true
	}
	if w.activeSkin.Desktop.Backdrop == "quiet-gradient" {
		w.drawQuietSkinBackdrop()
		return true
	}
	if w.skinBackdrop == nil {
		decoded, err := png.Decode(bytes.NewReader(sculptedSilverPNG))
		if err != nil {
			return false
		}
		bounds := decoded.Bounds()
		pixels := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(pixels, pixels.Bounds(), decoded, bounds.Min, draw.Src)
		w.skinBackdrop, err = render.NewTexture(pixels.Rect.Dx(), pixels.Rect.Dy(), pixels.Pix)
		if err != nil {
			return false
		}
	}
	iw, ih := w.skinBackdrop.Size()
	width, height := float32(w.width), float32(w.height)
	scale := max(width/float32(iw), height/float32(ih))
	w.canvas.Image(w.skinBackdrop, (width-float32(iw)*scale)/2, (height-float32(ih)*scale)/2, float32(iw)*scale, float32(ih)*scale)
	return true
}

func (w *Workspace) drawQuietSkinBackdrop() {
	color := func(token, fallback string, alpha float32) scene.Color {
		if _, ok := w.activeSkin.Palette[token]; !ok {
			token = fallback
		}
		c := w.activeSkin.Color(token)
		return scene.Color{R: float32(c.R) / 255, G: float32(c.G) / 255, B: float32(c.B) / 255, A: alpha}
	}
	width, height := float32(w.width), float32(w.height)
	w.canvas.Rect(0, 0, width, height, color("desktop-background", "background", 1))
	glow := color("desktop-glow", "accent", .24)
	w.canvas.RadialGradient(width*.43, height*.36, max(width, height)*.74, glow, glow.WithAlpha(0))
}
