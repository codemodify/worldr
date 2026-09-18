//go:build linux && cgo

package native

import (
	"bytes"
	"testing"

	"github.com/codemodify/worldr/internal/scene"
)

func gradientCanvasGPU(t *testing.T) (*VK, *scene.Canvas) {
	t.Helper()
	vk := openTextureGPU(t)
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = canvas.Close() })
	if err := vk.SetSceneAtlas(canvas.Atlas()); err != nil {
		t.Fatal(err)
	}
	canvas.Reset(64, 64)
	return vk, canvas
}

func TestRadialGradientInterpolatesColorAndDiscCoverageGPU(t *testing.T) {
	vk, canvas := gradientCanvasGPU(t)
	canvas.RadialGradient(32, 32, 24, scene.Color{R: 1, G: .4, B: .2, A: 1}, scene.Color{G: .2, B: 1, A: 1})
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(canvas.Frame(), [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	center, middle, edge := (32*64+32)*4, (32*64+44)*4, (32*64+54)*4
	if pixels[center+2] < 240 || pixels[center] > 65 || pixels[middle+2] < 115 || pixels[middle+2] > 130 || pixels[middle] < 150 || pixels[middle] > 165 || pixels[edge+2] > 25 || pixels[edge] < 235 {
		t.Fatalf("center-to-perimeter colors did not interpolate: center=%v middle=%v edge=%v", pixels[center:][:4], pixels[middle:][:4], pixels[edge:][:4])
	}
	for _, point := range [][2]int{{0, 0}, {12, 12}, {8, 8}, {56, 32}, {32, 56}} {
		texturePixel(t, pixels, 64, point[0], point[1], [4]byte{0, 0, 0, 255})
	}
	if pixels[(32*64+12)*4] == 0 || pixels[(12*64+32)*4] == 0 {
		t.Fatal("disc coverage failed along an interior axis")
	}
}

func TestRadialGradientAlphaIsSingleLayerAndStableGPU(t *testing.T) {
	vk, canvas := gradientCanvasGPU(t)
	canvas.RadialGradient(32, 32, 24, scene.Color{R: 1, G: 1, B: 1, A: .2}, scene.Color{R: 1, G: 1, B: 1})
	frame := canvas.Frame()
	baseline, pixels := make([]byte, 64*64*4), make([]byte, 64*64*4)
	for i := 0; i < 64; i++ {
		if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			copy(baseline, pixels)
			center, middle := pixels[(32*64+32)*4], pixels[(32*64+44)*4]
			if center < 47 || center > 51 || middle < 23 || middle > 26 {
				t.Fatalf("halo did not blend as one linear-alpha layer: center=%d middle=%d", center, middle)
			}
			for pixel := 0; pixel < len(pixels); pixel += 4 {
				if pixels[pixel] > 51 || pixels[pixel] != pixels[pixel+1] || pixels[pixel] != pixels[pixel+2] || pixels[pixel+3] != 255 {
					t.Fatalf("fan seams accumulated coverage or altered opaque destination alpha at byte %d: %v", pixel, pixels[pixel:pixel+4])
				}
			}
		} else if !bytes.Equal(baseline, pixels) {
			t.Fatalf("unchanged radial gradient flickered on frame %d", i)
		}
	}
}

func TestRadialGradientOutsideCanvasClipsOnGPU(t *testing.T) {
	vk, canvas := gradientCanvasGPU(t)
	white := scene.Color{R: 1, G: 1, B: 1, A: 1}
	canvas.RadialGradient(-4, 32, 10, white, white)
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(canvas.Frame(), [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 0, 32, [4]byte{255, 255, 255, 255})
	texturePixel(t, pixels, 64, 4, 32, [4]byte{255, 255, 255, 255})
	texturePixel(t, pixels, 64, 7, 32, [4]byte{0, 0, 0, 255})
	texturePixel(t, pixels, 64, 0, 20, [4]byte{0, 0, 0, 255})
}
