//go:build linux && cgo

package native

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
)

func fluidFixture() fluid.Field {
	return fluid.Field{Bounds: fluid.Rect{Width: 128, Height: 96}, Style: fluid.Style{Blend: 28, Opacity: 1}, Surfaces: []fluid.Surface{
		{Bounds: fluid.Rect{X: 12, Y: 18, Width: 42, Height: 62}, Radius: 12, Tint: [4]float32{1, 1, 1, 1}, Fuse: true},
		{Bounds: fluid.Rect{X: 60, Y: 18, Width: 42, Height: 62}, Radius: 12, Tint: [4]float32{1, 1, 1, 1}, Fuse: true},
	}}
}

func fluidFrame(field fluid.Field) render.Frame {
	return render.Frame{Commands: []render.Command{{Kind: render.FluidCommand, Fluid: field}}}
}

func TestFluidFusionMatchesCPUAndDisableNeverFormsBridgeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	if err := vk.Resize(128, 96); err != nil {
		t.Fatal(err)
	}
	field := fluidFixture()
	fused := glowFrame(t, vk, fluidFrame(field))
	// Gap midpoint lies in the smooth bridge, outside both rounded rectangles.
	if fused[(49*128+57)*4] < 90 {
		t.Fatal("enabled neighboring panels did not form a filled bridge")
	}
	for _, separate := range []string{"disabled-first", "disabled-second", "hard-union"} {
		t.Run(separate, func(t *testing.T) {
			f := fluidFixture()
			switch separate {
			case "disabled-first":
				f.Surfaces[0].Fuse = false
			case "disabled-second":
				f.Surfaces[1].Fuse = false
			case "hard-union":
				f.Style.Blend = 0
			}
			pixels := glowFrame(t, vk, fluidFrame(f))
			if pixels[(49*128+57)*4] > 20 {
				t.Fatal("disabled fusion retained a bridge")
			}
		})
	}
	// Sample well away from anti-aliased boundaries; CPU hit testing and native
	// rendering must classify the same concave bridge and rounded outer contour.
	for y := 2; y < 94; y += 3 {
		for x := 2; x < 126; x += 3 {
			d := field.Distance(fluid.Point{X: float32(x) + .5, Y: float32(y) + .5})
			v := fused[(y*128+x)*4]
			if d < -2 && v < 90 || d > 2 && v > 20 {
				t.Fatalf("CPU/GPU distance disagree at (%d,%d): distance %g channel %d", x, y, d, v)
			}
		}
	}
	stats := vk.MemoryStats()
	for i := range 20 {
		field.Surfaces[1].Bounds.X = 60 + float32(i%4)
		glowFrame(t, vk, fluidFrame(field))
	}
	if next := vk.MemoryStats(); stats.AllocatedBytes != next.AllocatedBytes || stats.Images != next.Images || stats.Buffers != next.Buffers || len(vk.frame.uploaded) != 0 || len(vk.frame.textures) != 0 {
		t.Fatal("moving fluid panels rebuilt GPU geometry, textures, or storage")
	}
	field.Surfaces = nil
	field.Style.Blend = 0
	for i := range fluid.MaxSurfaces {
		field.Surfaces = append(field.Surfaces, fluid.Surface{Bounds: fluid.Rect{X: float32(4 + i%4*30), Y: float32(4 + i/4*22), Width: 20, Height: 14}, Radius: 3, Tint: [4]float32{1, 1, 1, 1}, Fuse: true})
	}
	maximum := glowFrame(t, vk, fluidFrame(field))
	if maximum[(76*128+104)*4] < 90 {
		t.Fatal("sixteenth surface did not reach the shader's bounded uniform array")
	}
}

func TestFluidOrderedImagesOverlaysBoundsAndEmptyBackdropGPU(t *testing.T) {
	vk := openTextureGPU(t)
	field := fluid.Field{Bounds: fluid.Rect{X: 8, Y: 8, Width: 48, Height: 48}, Style: fluid.Style{Background: [3][4]float32{{.12, .25, .4, 1}, {.12, .25, .4, 1}, {.12, .25, .4, 1}}}}
	texture, err := render.NewTexture(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	image := render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{12, 12, 16, 16}}}
	frame := fluidFrame(field)
	frame.Vertices = []render.Vertex{{X: 32, Y: 32, G: 1, A: 1}, {X: 48, Y: 32, G: 1, A: 1}, {X: 48, Y: 48, G: 1, A: 1}, {X: 32, Y: 32, G: 1, A: 1}, {X: 48, Y: 48, G: 1, A: 1}, {X: 32, Y: 48, G: 1, A: 1}}
	frame.Commands = append(frame.Commands, image, render.Command{Kind: render.OverlayCommand, Count: 6})
	for _, linear := range []bool{false, true} {
		frame.LinearColor = linear
		pixels := glowFrame(t, vk, frame)
		texturePixel(t, pixels, 64, 20, 20, [4]byte{255, 0, 0, 255})
		texturePixel(t, pixels, 64, 40, 40, [4]byte{0, 255, 0, 255})
		texturePixel(t, pixels, 64, 4, 4, [4]byte{0, 0, 0, 255})
		if pixels[(50*64+10)*4] < 80 {
			t.Fatal("zero-panel field did not paint an opaque backdrop")
		}
	}
	frame.Commands = []render.Command{image, {Kind: render.FluidCommand, Fluid: field}}
	pixels := glowFrame(t, vk, frame)
	if pixels[(20*64+20)*4+2] > 100 {
		t.Fatal("fluid command ignored stream ordering over an earlier image")
	}
	// Separate commands each retain their own UBO data, even with equal counts.
	second := field
	second.Bounds = fluid.Rect{X: 40, Y: 0, Width: 24, Height: 24}
	second.Style.Background = [3][4]float32{{.8, .1, .1, 1}, {.8, .1, .1, 1}, {.8, .1, .1, 1}}
	frame.Commands = []render.Command{{Kind: render.FluidCommand, Fluid: field}, {Kind: render.FluidCommand, Fluid: second}}
	pixels = glowFrame(t, vk, frame)
	if pixels[(12*64+48)*4+2] < 190 || pixels[(32*64+20)*4] < 80 {
		t.Fatal("ordered fluid fields shared or overwrote each other's constants")
	}
}

func TestFluidResizeRecoveryOpacityAndValidationGPU(t *testing.T) {
	vk := openTextureGPU(t)
	field := fluidFixture()
	field.Bounds = fluid.Rect{Width: 64, Height: 64}
	field.Style = fluid.DefaultStyle()
	frame := fluidFrame(field)
	frame.LinearColor = true
	initial := glowFrame(t, vk, frame)
	if err := vk.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := glowFrame(t, vk, frame); !bytes.Equal(initial, got) {
		t.Fatal("recovery changed fluid output")
	}
	if err := vk.Resize(83, 57); err != nil {
		t.Fatal(err)
	}
	field.Bounds = fluid.Rect{X: -2.25, Y: 1.5, Width: 83, Height: 55}
	field.Style.Opacity = 0
	withInvisible := glowFrame(t, vk, fluidFrame(field))
	field.Surfaces = nil
	if empty := glowFrame(t, vk, fluidFrame(field)); !bytes.Equal(empty, withInvisible) {
		t.Fatal("zero opacity did not restore the exact procedural backdrop")
	}
	for _, mutation := range []func(*fluid.Field){
		func(f *fluid.Field) { f.Bounds.Width = 0 },
		func(f *fluid.Field) { f.Time = float32(math.NaN()) },
		func(f *fluid.Field) { f.Pointer.X = float32(math.Inf(1)) },
		func(f *fluid.Field) { f.Style.Rim = 1.1 },
		func(f *fluid.Field) { f.Surfaces[0].Radius = 100 },
		func(f *fluid.Field) { f.Surfaces[0].Tint[0] = -1 },
		func(f *fluid.Field) { f.Surfaces = make([]fluid.Surface, 17) },
	} {
		invalid := fluidFixture()
		mutation(&invalid)
		if err := vk.RenderFrame(fluidFrame(invalid), [4]float32{}, nil); err == nil {
			t.Fatal("invalid fluid field reached native rendering")
		}
	}
}

func TestFluidExtendedOutputCropKeepsBackdropRefractionAndRimAlignedGPU(t *testing.T) {
	full := outputGPU(t, 130, 96)
	part := outputGPU(t, 65, 96)
	field := fluidFixture()
	field.Bounds.Width = 130
	field.Style = fluid.DefaultStyle()
	field.Pointer, field.PointerActive = fluid.Point{X: 67, Y: 40}, true
	frame := fluidFrame(field)
	frame.LinearColor = true
	baseline := glowFrame(t, full, frame)
	var cropped render.Frame
	cropOutputFrame(&cropped, frame, image.Pt(65, 0))
	pixels := glowFrame(t, part, cropped)
	for y := range 96 {
		for x := range 65 {
			for c := range 4 {
				delta := int(pixels[(y*65+x)*4+c]) - int(baseline[(y*130+x+65)*4+c])
				if delta < -2 || delta > 2 {
					t.Fatalf("cropped fluid changed material at (%d,%d): channel%d delta%d", x, y, c, delta)
				}
			}
		}
	}
}

func benchmarkFluidField(count int) fluid.Field {
	field := fluid.Field{Bounds: fluid.Rect{Width: 884, Height: 720}, Style: fluid.DefaultStyle()}
	for i := range count {
		field.Surfaces = append(field.Surfaces, fluid.Surface{Bounds: fluid.Rect{X: float32(30 + i%4*206), Y: float32(40 + i/4*150), Width: 190, Height: 140}, Radius: 24, Tint: [4]float32{.18, .36, .4, .7}, Fuse: true})
	}
	return field
}

func BenchmarkFluidUniformPacking(b *testing.B) {
	for _, count := range []int{5, 16} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			field := benchmarkFluidField(count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				field.Surfaces[0].Bounds.X = float32(i % 100)
				fluidPackingSink = packFluid(field)
			}
		})
	}
}

var fluidPackingSink = packFluid(fluid.Field{})

func BenchmarkFluidPanelsGPU(b *testing.B) {
	for _, count := range []int{5, 16} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			vk, err := OpenVK(false, 884, 720)
			if err != nil {
				b.Skip(err)
			}
			defer vk.Close()
			frame := fluidFrame(benchmarkFluidField(count))
			frame.LinearColor = true
			for range 3 {
				if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame.Commands[0].Fluid.Time = float32(i) / 60
				frame.Commands[0].Fluid.Surfaces[0].Bounds.X = 30 + float32(i%100)
				if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
