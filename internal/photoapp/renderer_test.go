package photoapp

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func testPhotoRenderer(t *testing.T, w, h int) *photoRenderer {
	t.Helper()
	r, err := newPhotoRenderer(w, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	return r
}

func TestPhotoRendererImageFillsTightAspectTextureWithoutChrome(t *testing.T) {
	r := testPhotoRenderer(t, 1120, 760)
	for _, item := range []struct{ source, want image.Point }{
		{image.Pt(200, 100), image.Pt(1120, 560)},
		{image.Pt(100, 200), image.Pt(380, 760)},
		{image.Pt(2, 2), image.Pt(760, 760)},
		{image.Pt(16000, 100), image.Pt(1120, 7)},
		{image.Pt(100, 16000), image.Pt(5, 760)},
		{image.Pt(16000, 1), image.Pt(1120, 1)},
	} {
		photo := photoSolid(item.source.X, item.source.Y, 183)
		if err := r.paint("", false, photo); err != nil {
			t.Fatal(err)
		}
		if got := r.image.Rect.Size(); got != item.want {
			t.Fatalf("source %v: texture %v, want %v", item.source, got, item.want)
		}
		for i := 0; i < len(r.image.Pix); i += 4 {
			if !bytes.Equal(r.image.Pix[i:i+4], []byte{183, 40, 80, 255}) {
				t.Fatalf("source %v: chrome, letterbox or altered color at texture pixel %d", item.source, i/4)
			}
		}
		if r.requested != image.Pt(1120, 760) {
			t.Fatal("fitting image changed the requested display box")
		}
	}
}

func TestPhotoRendererPreservesAllEdgesAndSourceOrigin(t *testing.T) {
	r := testPhotoRenderer(t, 1120, 760)
	photo := image.NewRGBA(image.Rect(7, 11, 107, 61))
	for y := photo.Rect.Min.Y; y < photo.Rect.Max.Y; y++ {
		for x := photo.Rect.Min.X; x < photo.Rect.Max.X; x++ {
			photo.SetRGBA(x, y, color.RGBA{byte(x), byte(y), 123, 255})
		}
	}
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ from, to image.Point }{
		{photo.Rect.Min, r.image.Rect.Min},
		{image.Pt(photo.Rect.Max.X-1, photo.Rect.Min.Y), image.Pt(r.image.Rect.Max.X-1, 0)},
		{image.Pt(photo.Rect.Min.X, photo.Rect.Max.Y-1), image.Pt(0, r.image.Rect.Max.Y-1)},
		{photo.Rect.Max.Sub(image.Pt(1, 1)), r.image.Rect.Max.Sub(image.Pt(1, 1))},
	} {
		if got, want := r.image.RGBAAt(pair.to.X, pair.to.Y), photo.RGBAAt(pair.from.X, pair.from.Y); got != want {
			t.Fatalf("cropped or changed source edge %v → %v: %v, want %v", pair.from, pair.to, got, want)
		}
	}
	// A photo that exactly fills its display box preserves every source pixel.
	if err := r.resize(640, 400); err != nil {
		t.Fatal(err)
	}
	photo = image.NewRGBA(image.Rect(7, 11, 647, 411))
	for i := 0; i < len(photo.Pix); i += 4 {
		photo.Pix[i], photo.Pix[i+1], photo.Pix[i+2], photo.Pix[i+3] = byte(i), byte(i/17), byte(i/23), 255
	}
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(photo.Pix, r.image.Pix) {
		t.Fatal("same-size image altered source pixels")
	}
}

func TestPhotoRendererTransparencyOnlyUsesImageArea(t *testing.T) {
	r := testPhotoRenderer(t, 640, 400)
	photo := image.NewNRGBA(image.Rect(0, 0, 640, 320))
	photo.SetNRGBA(20, 20, color.NRGBA{200, 80, 20, 128})
	photo.SetNRGBA(30, 30, color.NRGBA{230, 120, 50, 255})
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	if r.image.Rect != photo.Rect {
		t.Fatal("transparent photo gained a letterbox")
	}
	if a, b := r.image.RGBAAt(0, 0), r.image.RGBAAt(8, 0); a != (color.RGBA{34, 39, 43, 255}) || b != (color.RGBA{44, 49, 53, 255}) {
		t.Fatalf("unexpected transparency checker: %v %v", a, b)
	}
	if got := r.image.RGBAAt(20, 20); got.R <= 44 || got.R >= 200 || got.A != 255 {
		t.Fatalf("partial alpha was not composited: %v", got)
	}
	if got := r.image.RGBAAt(30, 30); got != (color.RGBA{230, 120, 50, 255}) {
		t.Fatalf("checker changed opaque image pixel: %v", got)
	}
	for i := 3; i < len(r.image.Pix); i += 4 {
		if r.image.Pix[i] != 255 {
			t.Fatal("surface contains uncomposited alpha")
		}
	}
}

func TestPhotoRendererResizeRetainsTextureAndRequestedBox(t *testing.T) {
	r := testPhotoRenderer(t, 1120, 760)
	photo := photoSolid(20, 10, 181)
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	texture, pixels := r.texture, r.image
	if err := r.paint("", false, photo); err != nil || r.image != pixels {
		t.Fatal("same-size repaint reallocated frame pixels")
	}
	for _, item := range []struct{ box, want image.Point }{
		{image.Pt(640, 400), image.Pt(640, 320)},
		{image.Pt(1920, 1080), image.Pt(1920, 960)},
	} {
		if err := r.resize(item.box.X, item.box.Y); err != nil {
			t.Fatal(err)
		}
		if err := r.paint("", false, photo); err != nil {
			t.Fatal(err)
		}
		if r.texture != texture || r.requested != item.box || r.image.Rect.Size() != item.want {
			t.Fatal("resize lost texture identity, requested box, or photo aspect")
		}
		update, ok := texture.Snapshot(0)
		if !ok || update.Width != item.want.X || update.Height != item.want.Y || !bytes.Equal(update.Pixels, r.image.Pix) {
			t.Fatal("texture does not contain the resized full photo")
		}
	}
	// A portrait replacement uses the requested box, not the preceding
	// landscape image's smaller actual texture height.
	if err := r.paint("", false, photoSolid(10, 40, 111)); err != nil {
		t.Fatal(err)
	}
	if r.image.Rect.Size() != image.Pt(270, 1080) || r.texture != texture {
		t.Fatal("portrait replacement inherited old image aspect or changed renderer texture")
	}
	before := r.requested
	if err := r.resize(639, 400); err == nil || r.requested != before {
		t.Fatal("invalid display box changed requested dimensions")
	}
	r.close()
	r.close()
	if err := r.paint("", false, photo); err == nil {
		t.Fatal("closed renderer accepted paint")
	}
}

func TestPhotoRendererOnlyShowsNecessaryLoadingAndErrors(t *testing.T) {
	r := testPhotoRenderer(t, 640, 400)
	loading := append([]byte(nil), r.image.Pix...)
	if err := r.paint("Unable to open fixture: invalid header", false, nil); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(loading, r.image.Pix) {
		t.Fatal("initial failure did not change loading message")
	}
	photo := photoSolid(100, 50, 173)
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	clean := append([]byte(nil), r.image.Pix...)
	if err := r.paint("", true, photo); err != nil || !bytes.Equal(clean, r.image.Pix) {
		t.Fatal("replacement loading decorated the current photograph")
	}
	if err := r.paint("Unable to open next.png", false, photo); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(clean, r.image.Pix) || !bytes.Equal(clean[:r.image.Stride*200], r.image.Pix[:r.image.Stride*200]) {
		t.Fatal("failure message was missing or obscured more than the bottom status line")
	}
	if err := r.paint("", false, photo); err != nil || !bytes.Equal(clean, r.image.Pix) {
		t.Fatal("successful repaint did not restore the unadorned photo")
	}
}

type opacityScanPhoto struct {
	*image.RGBA
	opacityScans int
}

func (p *opacityScanPhoto) Opaque() bool {
	p.opacityScans++
	return false
}

func TestPhotoRendererDoesNotScanWholeSourceOpacity(t *testing.T) {
	r := testPhotoRenderer(t, 640, 400)
	photo := &opacityScanPhoto{RGBA: image.NewRGBA(image.Rect(0, 0, 200, 100))}
	if err := r.paint("", false, photo); err != nil {
		t.Fatal(err)
	}
	if photo.opacityScans != 0 {
		t.Fatal("photo scaling scanned the complete source for opacity")
	}
}

func BenchmarkPhotoRendererPaint(b *testing.B) {
	r, err := newPhotoRenderer(1120, 760)
	if err != nil {
		b.Fatal(err)
	}
	defer r.close()
	photo := photoSolid(1600, 1000, 180)
	if err := r.paint("", false, photo); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.paint("", false, photo); err != nil {
			b.Fatal(err)
		}
	}
}
