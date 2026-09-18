package workspace

import (
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"image"
	"image/color"
	"math"
)

type shapedKey struct {
	text        string
	size, width int
	color       scene.Color
}
type shapedLabel struct {
	texture       *render.Texture
	width, height int
	frame         uint64
}
type shapedLabels struct {
	painter          *nativeui.Painter
	cache            map[shapedKey]*shapedLabel
	frame            uint64
	bytes            int
	images, commands []render.Command
	retired          []uint64
}

func (w *Workspace) beginShapedLabels() {
	if w.labels == nil {
		return
	}
	w.labels.frame++
	w.labels.images = w.labels.images[:0]
}
func (w *Workspace) shapedText(x, y, size, maxWidth float32, text string, c scene.Color) {
	if w.helpOpen || maxWidth < 1 || text == "" {
		return
	}
	if w.labels == nil {
		painter, err := nativeui.NewPainter(nativeui.Cinematic())
		if err != nil {
			return
		}
		w.labels = &shapedLabels{painter: painter, cache: map[shapedKey]*shapedLabel{}, frame: 1}
	}
	labels := w.labels
	key := shapedKey{text: text, size: max(6, min(128, int(math.Round(float64(size))))), width: max(1, min(2048, int(maxWidth))), color: c}
	label := labels.cache[key]
	if label == nil {
		labels.painter.Theme.FontSize = float64(key.size)
		metrics, err := labels.painter.Measure(text, key.width)
		if err != nil {
			return
		}
		width, height := max(1, min(key.width, metrics.Width)), max(1, min(256, metrics.Height))
		needed := width * height * 4
		for len(labels.cache) >= 512 || labels.bytes+needed > 32<<20 {
			var oldestKey shapedKey
			var oldest *shapedLabel
			for k, v := range labels.cache {
				if v.frame < labels.frame && (oldest == nil || v.frame < oldest.frame) {
					oldestKey, oldest = k, v
				}
			}
			if oldest == nil {
				return
			}
			delete(labels.cache, oldestKey)
			labels.bytes -= oldest.width * oldest.height * 4
			labels.retired = append(labels.retired, oldest.texture.ID())
		}
		pixels := image.NewRGBA(image.Rect(0, 0, width, height))
		channel := func(v float32) uint8 { return uint8(max(0, min(255, v*255))) }
		rgba := color.RGBA{R: channel(c.R * c.A), G: channel(c.G * c.A), B: channel(c.B * c.A), A: channel(c.A)}
		if err = labels.painter.DrawLabel(pixels, pixels.Rect, text, rgba); err != nil {
			return
		}
		texture, err := render.NewTexture(width, height, pixels.Pix)
		if err != nil {
			return
		}
		label = &shapedLabel{texture: texture, width: width, height: height}
		labels.cache[key] = label
		labels.bytes += needed
	}
	label.frame = labels.frame
	labels.images = append(labels.images, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: label.texture, Bounds: [4]float32{x, y, float32(label.width), float32(label.height)}}})
}
func (w *Workspace) shapedFrame(frame render.Frame) render.Frame {
	if w.labels == nil || len(w.labels.images) == 0 {
		return frame
	}
	labels := w.labels
	labels.commands = append(labels.commands[:0], frame.Commands...)
	labels.commands = append(labels.commands, labels.images...)
	frame.Commands = labels.commands
	return frame
}
func (w *Workspace) RetiredTextures() []uint64 {
	if w.labels == nil {
		return nil
	}
	ids := w.labels.retired
	w.labels.retired = nil
	return ids
}
