package main

import (
	"bytes"
	"fmt"
	"image"
	"reflect"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func compareDiskCache(t *testing.T, d, fresh *diskDesktop, label string) {
	t.Helper()
	if !bytes.Equal(d.pixels.Pix, fresh.pixels.Pix) {
		for i, value := range d.pixels.Pix {
			if value != fresh.pixels.Pix[i] {
				t.Fatalf("cached pixels differ from fresh rendering after %s at (%d,%d), channel %d: %d versus %d", label, i/4%d.width, i/4/d.width, i%4, value, fresh.pixels.Pix[i])
			}
		}
	}
	if !reflect.DeepEqual(d.controller.Semantics(), fresh.controller.Semantics()) {
		t.Fatalf("cached semantics differ after %s", label)
	}
}

func TestDiskCompositionCacheMatchesFullRendering(t *testing.T) {
	d, validator, _ := startDisk(t)
	fresh, freshValidator, _ := startDisk(t)
	apply := func(label string, f func(*diskDesktop) error) {
		t.Helper()
		fresh.clearRenderCache()
		if err := f(d); err != nil {
			t.Fatal(err)
		}
		if err := f(fresh); err != nil {
			t.Fatal(err)
		}
		compareDiskCache(t, d, fresh, label)
		diskSnapshot(t, d, validator)
		diskSnapshot(t, fresh, freshValidator)
	}
	compareDiskCache(t, d, fresh, "initial paint")
	for _, id := range []string{"row-efi", "sort-capacity", "block-recovery", "view", "refresh", "grid", "filter-system", "filter-efi", "filter-recovery", "filter-system", "reset", "menu-action", "popup-refresh", "help", "help-close"} {
		node := diskNode(t, d, id)
		for _, kind := range []nativeapp.EventKind{nativeapp.PointerDown, nativeapp.PointerUp} {
			event := nativeapp.Event{Kind: kind, Button: nativeapp.ButtonPrimary, X: float32(node.Bounds.X + node.Bounds.Width/2), Y: float32(node.Bounds.Y + node.Bounds.Height/2)}
			apply(id, func(app *diskDesktop) error { return app.Handle(1, event) })
		}
	}
	for _, id := range []string{"merrick", "advanced", "plasma", "hologram"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		apply(id, func(app *diskDesktop) error { return app.SetSkin(selected) })
		for _, size := range [][2]int{{900, 650}, {1511, 1007}, {1920, 1080}} {
			apply(fmt.Sprintf("%s at %v", id, size), func(app *diskDesktop) error { return app.Resize(1, size[0], size[1]) })
			for _, control := range []string{"row-recovery", "block-system", "menu-file", "filter-efi"} {
				node := diskNode(t, d, control)
				event := nativeapp.Event{Kind: nativeapp.PointerMove, X: float32(node.Bounds.X + node.Bounds.Width/2), Y: float32(node.Bounds.Y + node.Bounds.Height/2)}
				apply(control, func(app *diskDesktop) error { return app.Handle(1, event) })
			}
			bytes := 0
			for _, layer := range d.layers {
				bytes += len(layer.pixels.Pix)
			}
			if bytes != len(d.pixels.Pix) || len(d.background.Pix) != len(d.pixels.Pix) {
				t.Fatal("composition cache is not bounded to two current framebuffers")
			}
		}
	}
}

func TestGlyphHaloMatchesOriginalBoxFilter(t *testing.T) {
	layer := image.NewRGBA(image.Rect(0, 0, 31, 23))
	for i := 3; i < len(layer.Pix); i += 4 {
		layer.Pix[i] = uint8(i * 71)
	}
	for _, radius := range []int{2, 3, 4, 9} {
		halo := glyphHalo(layer, radius)
		for y := 0; y < layer.Rect.Dy(); y++ {
			for x := 0; x < layer.Rect.Dx(); x++ {
				total := 0
				for yy := max(0, y-radius); yy <= min(layer.Rect.Dy()-1, y+radius); yy++ {
					for xx := max(0, x-radius); xx <= min(layer.Rect.Dx()-1, x+radius); xx++ {
						total += int(layer.Pix[yy*layer.Stride+xx*4+3])
					}
				}
				want := uint8(total / ((2*radius + 1) * (2*radius + 1)) / 2)
				if got := halo.AlphaAt(x, y).A; got != want {
					t.Fatalf("radius%d at(%d,%d): got%d want%d", radius, x, y, got, want)
				}
			}
		}
	}
}
