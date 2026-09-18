package app

import (
	"fmt"
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// Register only formats this Vulkan device can import. Clients keep the shared
// memory fallback; exporting a supported format never requires CPU readback.
func configureApplicationRenderer(p *presenter) func(*applicationController) error {
	return func(a *applicationController) error {
		server, ok := a.server.(*apps.Server)
		if !ok || p == nil || p.vk == nil {
			return nil
		}
		if err := server.SetDMABufImporterForDevice(p.vk.DMABufFormats(), p.vk.RenderNode(), func(descriptor dmabuf.Descriptor) (*render.Texture, error) {
			if p.vk == nil {
				return nil, native.ErrNotReady
			}
			return p.vk.ImportDMABuf(descriptor)
		}); err != nil {
			return err
		}
		return a.updateOutputScale(p)
	}
}
func (a *applicationController) updateOutputScale(p *presenter) error {
	server, ok := a.server.(interface{ SetScale120(uint32) error })
	if !ok {
		return nil
	}
	scale := float64(1)
	if p != nil && p.win != nil {
		scale = p.win.Scale()
	}
	return server.SetScale120(uint32(math.Round(float64(scale) * 120)))
}
func (a *applicationController) updateApplicationImage(entry *applicationImage, s apps.Surface) error {
	if len(s.Layers) == 0 {
		if entry.surface.Texture == nil || entry.surface.Spatial != nil {
			texture, err := render.NewTexture(s.Width, s.Height, s.Pixels)
			if err != nil {
				return err
			}
			entry.surface.Texture = texture
		} else if entry.revision != s.Revision {
			if err := entry.surface.Texture.Replace(s.Width, s.Height, s.Pixels); err != nil {
				return err
			}
		}
		entry.surface.Spatial, entry.surface.SurfaceUV, entry.surface.Translucent = nil, [4]float32{}, false
		entry.surface.ContentAspect = 0
	} else {
		root := -1
		for i, layer := range s.Layers {
			if layer.Root {
				root = i
				break
			}
		}
		if root < 0 || s.LogicalWidth <= 0 || s.LogicalHeight <= 0 {
			return fmt.Errorf("invalid layered surface dimensions/root")
		}
		if entry.surface.Spatial == nil && entry.surface.Texture != nil {
			a.retired = append(a.retired, entry.surface.Texture.ID())
		}
		layer := s.Layers[root]
		entry.surface.Texture = layer.Texture
		entry.surface.SurfaceUV = layerUV(layer)
		entry.surface.ContentAspect = float32(s.LogicalWidth) / float32(s.LogicalHeight)
		entry.surface.Translucent = !layer.Opaque
		spatial := entry.surface.Spatial
		if spatial == nil {
			spatial = &experience.SpatialContent{}
		}
		spatial.Objects = spatial.Objects[:0]
		width, height := float32(s.LogicalWidth), float32(s.LogicalHeight)
		for i, layer := range s.Layers {
			if i == root {
				continue
			}
			// All layers use the root coordinate plane; the protocol server performs
			// child hit testing. Small depth offsets preserve below/above-parent order.
			x := (layer.X+layer.Width/2)/width - .5
			y := (height/2 - layer.Y - layer.Height/2) / width
			z := float32(i-root) * .00001
			spatial.Objects = append(spatial.Objects, experience.SpatialObject{ID: uint64(i + 1), Node: scene.Node{
				Surface: layer.Texture, SurfaceUV: layerUV(layer),
				Transform: scene.Translate(x, y, z).Mul(scene.Scale(layer.Width/width, layer.Height/width, 1)),
				Unlit:     true, Unpickable: true, Translucent: !layer.Opaque,
			}})
		}
		entry.surface.Spatial = spatial
	}
	entry.revision = s.Revision
	return nil
}
func layerUV(layer apps.Layer) [4]float32 {
	return [4]float32{layer.UV[0], layer.UV[1], layer.UV[2] - layer.UV[0], layer.UV[3] - layer.UV[1]}
}
func (a *applicationController) retireApplicationImage(entry *applicationImage) {
	if entry.surface.Spatial == nil && entry.surface.Texture != nil {
		a.retired = append(a.retired, entry.surface.Texture.ID())
	}
}
func (a *applicationController) collectServerTextures() {
	server, ok := a.server.(interface{ RetiredTextures() []*render.Texture })
	if !ok {
		return
	}
	for _, texture := range server.RetiredTextures() {
		if texture == nil {
			continue
		}
		if a.retiredBacking == nil {
			a.retiredBacking = map[uint64]*render.Texture{}
		}
		a.retired = append(a.retired, texture.ID())
		a.retiredBacking[texture.ID()] = texture
	}
}
func (a *applicationController) releaseTextureBacking(id uint64) error {
	texture := a.retiredBacking[id]
	delete(a.retiredBacking, id)
	if texture != nil {
		return texture.Close()
	}
	return nil
}
