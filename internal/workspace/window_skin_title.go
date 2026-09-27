package workspace

import (
	"image"
	"image/color"
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type windowSkinTitle struct {
	node, parent scene.NodeID
	mesh         *scene.Mesh
	text         string
	aspect       uint16
	width        uint16
}

func (w *Workspace) retireWindowSkinTitle(id uint64) {
	if title := w.windowSkinCache.titles[id]; title != nil {
		w.scene.Remove(title.node)
		w.windowSkinCache.retired = append(w.windowSkinCache.retired, title.mesh.Geometry().ID())
		delete(w.windowSkinCache.titles, id)
	}
}

func (w *Workspace) syncWindowSkinTitle(surface experience.ApplicationSurface, layout windowSkinLayout) {
	if surface.Title == "" || w.activeSkin == nil {
		w.retireWindowSkinTitle(surface.ID)
		return
	}
	bucket, aspect, widthBucket, width := w.windowSkinDimensions(surface)
	parent := w.applicationDragHandles[surface.ID]
	if previous := w.windowSkinCache.titles[surface.ID]; previous != nil && previous.parent == parent && previous.text == surface.Title && previous.aspect == bucket && previous.width == widthBucket {
		return
	}
	w.retireWindowSkinTitle(surface.ID)
	bounds := windowSkinTitleBounds(w.activeSkin, layout, aspect)
	if bounds.w <= 0 || bounds.h <= 0 {
		return
	}
	mesh := windowSkinTitleMeshForWidth(w.activeSkin, surface.Title, bounds, aspect, width, layout.verticalTitle)
	if mesh == nil {
		return
	}
	if w.windowSkinCache.titles == nil {
		w.windowSkinCache.titles = make(map[uint64]*windowSkinTitle)
	}
	node := w.scene.Add(parent, scene.Node{Mesh: mesh, Color: scene.Color{R: 1, G: 1, B: 1, A: 1}, Unlit: true, Unpickable: true})
	w.windowSkinCache.titles[surface.ID] = &windowSkinTitle{node: node, parent: parent, mesh: mesh, text: surface.Title, aspect: bucket, width: widthBucket}
}

func windowSkinTitleBounds(selected *skin.Skin, layout windowSkinLayout, aspect float32) box {
	if layout.verticalTitle {
		width, height := layout.logicalWidth, layout.logicalWidth/aspect
		return box{layout.titlebar.x + 4/width, layout.grip.y + layout.grip.h + 5/height, layout.titlebar.w - 8/width, layout.titlebar.y + layout.titlebar.h - layout.grip.y - layout.grip.h - 10/height}
	}
	left, right := layout.grip.x+layout.grip.w+.014, float32(.49)
	if selected.Window.Layout.ButtonsSide == "left" {
		left, right = -.49, layout.grip.x-.014
		for _, control := range layout.controls {
			left = max(left, control.box.x+control.box.w+.014)
		}
	} else {
		for _, control := range layout.controls {
			right = min(right, control.box.x-.014)
		}
	}
	if right-left < .04 {
		return box{}
	}
	return box{left, layout.titlebar.y, right - left, layout.titlebar.h}
}

// Titles are retained scene geometry, not a screen overlay. Rasterizing the
// authored font once into small coverage runs keeps shaping, ellipsis and
// custom fonts consistent with native controls while respecting depth/rotation.
func windowSkinTitleMesh(selected *skin.Skin, title string, bounds box, aspect float32) *scene.Mesh {
	return windowSkinTitleMeshForWidth(selected, title, bounds, aspect, 960, false)
}

func windowSkinTitleMeshForWidth(selected *skin.Skin, title string, bounds box, aspect, logicalWidth float32, vertical bool) *scene.Mesh {
	theme, err := nativeui.ThemeFromSkin(*selected)
	if err != nil {
		return nil
	}
	if vertical {
		// Honor the package font size at the same physical density on narrow
		// tools and wide documents; only constrain it to the authored tab width.
		theme.Typography.Size = min(theme.Typography.Size, max(8, int(bounds.w*logicalWidth)))
		theme.Typography.LineHeight = theme.Typography.Size + 4
	}
	painter, err := nativeui.NewPainter(theme)
	if err != nil {
		return nil
	}
	defer painter.Close()
	pixelWidth, pixelHeight := bounds.w*logicalWidth, bounds.h/aspect*logicalWidth
	if vertical {
		pixelWidth, pixelHeight = pixelHeight, pixelWidth
	}
	width := max(1, min(2048, int(math.Ceil(float64(pixelWidth)))))
	height := max(1, min(128, int(math.Ceil(float64(pixelHeight)))))
	pixels := image.NewRGBA(image.Rect(0, 0, width, height))
	token := selected.Window.Titlebar.TextColor
	if token == "" {
		token = "text"
	}
	foreground := color.RGBAModel.Convert(selected.Color(token)).(color.RGBA)
	if err := painter.DrawLabel(pixels, pixels.Bounds(), title, nativeui.LabelStyle{Color: foreground}); err != nil {
		return nil
	}
	var geometry frameGeometry
	for y := 0; y < height; y++ {
		for x := 0; x < width; {
			value := color.NRGBAModel.Convert(pixels.At(x, y)).(color.NRGBA)
			alpha := (int(value.A) + 8) / 17
			if alpha == 0 {
				x++
				continue
			}
			end := x + 1
			for end < width && (int(pixels.RGBAAt(end, y).A)+8)/17 == alpha {
				end++
			}
			left := bounds.x + float32(x)/float32(width)*bounds.w
			right := bounds.x + float32(end)/float32(width)*bounds.w
			top := bounds.y + bounds.h - float32(y)/float32(height)*bounds.h
			bottom := bounds.y + bounds.h - float32(y+1)/float32(height)*bounds.h
			if vertical {
				left = bounds.x + bounds.w - float32(y+1)/float32(height)*bounds.w
				right = bounds.x + bounds.w - float32(y)/float32(height)*bounds.w
				top = bounds.y + bounds.h - float32(x)/float32(width)*bounds.h
				bottom = bounds.y + bounds.h - float32(end)/float32(width)*bounds.h
			}
			geometry.rect(left, bottom, right, top, scene.Color{R: float32(value.R) / 255, G: float32(value.G) / 255, B: float32(value.B) / 255, A: float32(alpha) / 15})
			x = end
		}
	}
	mesh, err := geometry.mesh()
	if err != nil {
		return nil
	}
	return mesh
}
