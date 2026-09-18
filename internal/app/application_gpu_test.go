package app

import (
	"io"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"github.com/codemodify/worldr/internal/render"
)

type retiredLayerServer struct {
	adapterServer
	retired []*render.Texture
}

func (s *retiredLayerServer) RetiredTextures() []*render.Texture {
	result := s.retired
	s.retired = nil
	return result
}

func TestLayeredApplicationKeepsCropOrderAndLogicalInput(t *testing.T) {
	texture, err := render.NewTexture(8, 4, make([]byte, 8*4*4))
	if err != nil {
		t.Fatal(err)
	}
	child, err := render.NewTexture(2, 2, make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	server := &retiredLayerServer{adapterServer: adapterServer{current: []apps.Surface{{ID: 1, Width: 8, Height: 4, LogicalWidth: 3, LogicalHeight: 2, Revision: 1, Layers: []apps.Layer{
		{Token: 2, Texture: child, X: 0, Y: 0, Width: 1, Height: 1, UV: [4]float32{0, 0, 1, 1}},
		{Token: 1, Texture: texture, Root: true, Width: 3, Height: 2, UV: [4]float32{.25, 0, .75, 1}},
		{Token: 3, Texture: child, X: 2, Y: 1, Width: 1, Height: 1, UV: [4]float32{.5, 0, 1, .5}},
	}}}}}
	a := &applicationController{server: server, images: map[uint64]*applicationImage{}, output: io.Discard}
	if err := a.Poll(); err != nil {
		t.Fatal(err)
	}
	surface := a.Surfaces()[0]
	if surface.Texture != texture || surface.ContentAspect != 1.5 || surface.SurfaceUV != ([4]float32{.25, 0, .5, 1}) || !surface.Translucent {
		t.Fatal("GPU root was read back or its cropped aspect/alpha changed", surface)
	}
	if len(surface.Spatial.Objects) != 2 || surface.Spatial.Objects[0].Node.Transform[14] >= 0 || surface.Spatial.Objects[1].Node.Transform[14] <= 0 {
		t.Fatal("below/above-parent ordering lost", surface.Spatial)
	}
	for _, object := range surface.Spatial.Objects {
		if !object.Node.Unpickable {
			t.Fatal("protocol child stole workspace root input")
		}
	}
	a.Send(1, experience.Event{Kind: experience.PointerMove, X: 4, Y: 2})
	if server.x != 1.5 || server.y != 1 {
		t.Fatal("fractional cropped root misrouted logical pointer", server.x, server.y)
	}
	// Returning to CPU flattening must replace the immutable borrowed texture.
	server.current[0].Layers = nil
	server.current[0].Pixels = make([]byte, 128)
	server.current[0].Revision++
	server.retired = []*render.Texture{texture, child}
	if err := a.Poll(); err != nil {
		t.Fatal(err)
	}
	if got := a.Surfaces()[0]; got.Texture == texture || got.Spatial != nil || got.Translucent || got.ContentAspect != 0 {
		t.Fatal("CPU fallback retained stale layer state")
	}
	ids := a.RetiredTextures()
	if len(ids) != 2 {
		t.Fatal("server textures did not transfer once", ids)
	}
	for _, id := range ids {
		if err := a.releaseTextureBacking(id); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.retiredBacking) != 0 || len(a.RetiredTextures()) != 0 {
		t.Fatal("retired texture retained or repeated")
	}
}
