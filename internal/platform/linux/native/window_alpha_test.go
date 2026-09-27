//go:build linux && cgo

package native

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// Transparent standalone windows use the same linear HDR and multisampled
// render targets as the desktop. Verify that their final SDR pixels remain
// valid premultiplied values, with no opaque rectangle introduced by grading.
func TestGlassWindowAlphaSurvivesMSAAGradeAndResizeGPU(t *testing.T) {
	vk := openTextureGPU(t)
	canvas, err := scene.NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	if err := vk.SetSceneAtlas(canvas.Atlas()); err != nil {
		t.Fatal(err)
	}
	canvas.SetLinearColor(true)
	for _, extent := range []int{64, 80, 64} {
		if err := vk.Resize(uint32(extent), uint32(extent)); err != nil {
			t.Fatal(err)
		}
		canvas.Reset(extent, extent)
		canvas.FillConvex(scene.ColorHex(0x15576b, .25), [2]float32{16.3, 8.1}, [2]float32{48.2, 8.1}, [2]float32{56.2, 16.4}, [2]float32{56.2, 48.1}, [2]float32{48.3, 56.1}, [2]float32{16.2, 56.1}, [2]float32{8.3, 48.3}, [2]float32{8.3, 16.2})
		canvas.Rect(24, 24, 16, 16, scene.ColorHex(0xffffff, 1))
		frame := canvas.Frame()
		pixels := make([]byte, extent*extent*4)
		if err := vk.RenderFrame(frame, [4]float32{}, pixels); err != nil {
			t.Fatal(err)
		}
		alpha := make([]byte, extent*extent)
		for i := range alpha {
			alpha[i] = pixels[i*4+3]
		}
		if got := alpha[16*extent+32]; got < 63 || got > 64 {
			t.Fatalf("glass alpha=%d; expected quarter opacity", got)
		}
		if alpha[32*extent+32] != 255 || alpha[2*extent+2] != 0 {
			t.Fatal("opaque text or transparent exterior changed")
		}
		frame.Output = render.OutputTransform{Exposure: .6, Saturation: .12, ToneMap: render.ToneMapFilmic, BloomStrength: .4, BloomThreshold: .5, BloomRadius: 5}
		if err := vk.RenderFrame(frame, [4]float32{}, pixels); err != nil {
			t.Fatal(err)
		}
		for i, a := range alpha {
			p := pixels[i*4:][:4]
			if p[3] != a {
				t.Fatalf("grading changed alpha at pixel %d: %d -> %d", i, a, p[3])
			}
			if p[0] > a || p[1] > a || p[2] > a {
				t.Fatalf("unassociated output at pixel %d: %v", i, p)
			}
		}
	}
}
