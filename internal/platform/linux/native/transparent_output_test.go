//go:build linux && cgo

package native

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/scene"
)

func TestLowAlphaLinearOutputIsValidPremultipliedSRGBGPU(t *testing.T) {
	vk := openTextureGPU(t)
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	canvas.SetLinearColor(true)
	if err := vk.SetSceneAtlas(canvas.Atlas()); err != nil {
		t.Fatal(err)
	}
	for _, rgb := range []uint32{0x76deec, 0xffffff} {
		for _, alpha := range []float32{.008, .025, .06, .2} {
			t.Run(fmt.Sprintf("%06x-alpha-%.3f", rgb, alpha), func(t *testing.T) {
				canvas.Reset(64, 64)
				col := scene.ColorHex(rgb, alpha)
				canvas.Rect(4, 4, 24, 24, col)
				// Also cover antialiased low-coverage stroke fringes, where PNG
				// unassociation can expose bright RGB despite almost zero alpha.
				canvas.Line(4, 42, 58, 42, 3.1, col)
				pixels := make([]byte, 64*64*4)
				if err := vk.RenderFrame(canvas.Frame(), [4]float32{}, pixels); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < len(pixels); i += 4 {
					for c := 0; c < 3; c++ {
						if pixels[i+c] > pixels[i+3] {
							t.Fatalf("invalid associated BGRA at %d,%d: %v", i/4%64, i/4/64, pixels[i:i+4])
						}
					}
				}
				want := [4]byte{byte(math.Round(float64(col.R * alpha * 255))), byte(math.Round(float64(col.G * alpha * 255))), byte(math.Round(float64(col.B * alpha * 255))), byte(math.Round(float64(alpha * 255)))}
				texturePixel(t, pixels, 64, 16, 16, want)
				source := image.NewRGBA(image.Rect(0, 0, 64, 64))
				for i := 0; i < len(pixels); i += 4 {
					source.Pix[i], source.Pix[i+1], source.Pix[i+2], source.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]
				}
				for _, background := range []byte{16, 232} {
					// This is the conventional encoded source-over operation used
					// by consumers of premultiplied RGBA and transparent PNGs.
					destination := image.NewRGBA(source.Rect)
					draw.Draw(destination, destination.Rect, image.NewUniform(color.RGBA{background, background, background, 255}), image.Point{}, draw.Src)
					draw.Draw(destination, destination.Rect, source, image.Point{}, draw.Over)
					pixel := destination.RGBAAt(16, 16)
					for channel, value := range []byte{pixel.R, pixel.G, pixel.B} {
						sourceChannel := []float32{col.R, col.G, col.B}[channel]
						expect := math.Round(float64(sourceChannel*alpha*255 + float32(background)*(1-alpha)))
						if math.Abs(float64(value)-expect) > 2 {
							t.Fatalf("source-over background=%d channel=%d got=%d expected=%.0f", background, channel, value, expect)
						}
					}
					if pixel.A != 255 {
						t.Fatal("opaque destination lost alpha")
					}
					// Rendering the same foreground over an opaque GPU clear uses
					// linear-light blending before encoding, with the same coverage.
					level := float32(background) / 255
					if err := vk.RenderFrame(canvas.Frame(), [4]float32{level, level, level, 1}, pixels); err != nil {
						t.Fatal(err)
					}
					decode := func(v float64) float64 {
						if v <= .04045 {
							return v / 12.92
						}
						return math.Pow((v+.055)/1.055, 2.4)
					}
					var expected [4]byte
					for channel, value := range []float32{col.R, col.G, col.B} {
						expected[channel] = linearByte(decode(float64(value))*float64(alpha) + decode(float64(level))*float64(1-alpha))
					}
					expected[3] = 255
					texturePixel(t, pixels, 64, 16, 16, expected)
				}
			})
		}
	}
}
