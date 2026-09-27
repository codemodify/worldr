package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestPortraitCachePreservesAlphaAndInvalidatesWithSkinAndSize(t *testing.T) {
	// The optional user portrait can contain alpha, unlike the embedded RGB
	// photo. Cached pixels must preserve the original compositing arithmetic.
	portrait := image.NewNRGBA(image.Rect(0, 0, 81, 123))
	for y := 0; y < portrait.Rect.Dy(); y++ {
		for x := 0; x < portrait.Rect.Dx(); x++ {
			portrait.SetNRGBA(x, y, color.NRGBA{uint8(x * 3), uint8(y * 2), 97, uint8((x*5 + y*7) % 256)})
		}
	}
	d := &desktop{portrait: portrait}
	defer d.Close()
	if err := d.Start(nativeapp.Host{MaxSurfaces: 8, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}); err != nil {
		t.Fatal(err)
	}
	v := d.view(2)
	for _, id := range []string{"merrick", "hologram", "plasma"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{890, 320}, {713, 257}, {96, 64}, {1920, 1080}} {
			if err := d.Resize(2, size[0], size[1]); err != nil {
				t.Fatal(err)
			}
			d.state.Nutrients[0] = 19
			if err := d.paint(v); err != nil {
				t.Fatal(err)
			}
			cached := append([]byte(nil), v.pixels.Pix...)
			d.portraitCache = nil
			if err := d.paint(v); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(cached, v.pixels.Pix) {
				t.Fatalf("cached portrait changed alpha/panel pixels for %s at %v", id, size)
			}
			cache := d.portraitCache
			if err := d.paint(v); err != nil {
				t.Fatal(err)
			}
			if cache != d.portraitCache || len(cache.Pix) > len(v.pixels.Pix) {
				t.Fatal("portrait cache was regenerated or exceeds its current surface")
			}
		}
	}
}

func TestUnchangedResizeDoesNotRepaintMerrick(t *testing.T) {
	d, validator, _ := testDesktop(t, 8)
	v := d.view(2)
	if err := d.Resize(2, v.width, v.height); err != nil {
		t.Fatal(err)
	}
	if len(checkedSnapshot(t, d, validator).Textures) != 0 {
		t.Fatal("unchanged resize republished a surface")
	}
}
