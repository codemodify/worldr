//go:build linux

package projectapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/bmp"
)

func thumbnailFixture(t *testing.T, format string) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 24, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 24; x++ {
			source.SetRGBA(x, y, color.RGBA{R: 230, A: 255})
		}
	}
	var out bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&out, source)
	case "jpeg":
		err = jpeg.Encode(&out, source, nil)
	case "gif":
		err = gif.Encode(&out, source, nil)
	case "bmp":
		err = bmp.Encode(&out, source)
	case "webp":
		data, e := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ9Y/+ByKi/wEA")
		err = e
		out.Write(data)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestThumbnailFormatsAreBoundedAndKeepAspect(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif", "bmp", "webp"} {
		t.Run(format, func(t *testing.T) {
			thumb, err := decodeThumbnail(context.Background(), thumbnailFixture(t, format))
			if err != nil {
				t.Fatal(err)
			}
			want := image.Rect(0, 0, 18, 9)
			if format == "webp" {
				want = image.Rect(0, 0, 18, 18)
			}
			if thumb.Bounds() != want {
				t.Fatalf("bounds %v, want %v", thumb.Bounds(), want)
			}
			r, g, b, a := thumb.At(3, 3).RGBA()
			if r < 180<<8 || g > 20<<8 || b > 20<<8 || a != 65535 {
				t.Fatal("thumbnail lost fixture color")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeThumbnail(ctx, thumbnailFixture(t, "png")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decode: %v", err)
	}
	if _, err := decodeThumbnail(context.Background(), []byte("not a photo")); err == nil {
		t.Fatal("accepted invalid photo")
	}
	// A valid BMP config declaring a huge image must fail before pixel allocation.
	header := make([]byte, 54)
	copy(header, "BM")
	binary.LittleEndian.PutUint32(header[10:], 54)
	binary.LittleEndian.PutUint32(header[14:], 40)
	binary.LittleEndian.PutUint32(header[18:], 8192)
	binary.LittleEndian.PutUint32(header[22:], 8192)
	binary.LittleEndian.PutUint16(header[26:], 1)
	binary.LittleEndian.PutUint16(header[28:], 24)
	if _, err := decodeThumbnail(context.Background(), header); err == nil {
		t.Fatal("unbounded header accepted")
	}
}

func TestThumbnailJPEGOrientationAndGIFFirstCanvas(t *testing.T) {
	jpegData := thumbnailFixture(t, "jpeg")
	app := make([]byte, 36)
	copy(app, []byte{0xff, 0xe1, 0, 34})
	copy(app[4:], "Exif\x00\x00II")
	binary.LittleEndian.PutUint16(app[12:], 42)
	binary.LittleEndian.PutUint32(app[14:], 8)
	binary.LittleEndian.PutUint16(app[18:], 1)
	binary.LittleEndian.PutUint16(app[20:], 0x112)
	binary.LittleEndian.PutUint16(app[22:], 3)
	binary.LittleEndian.PutUint32(app[24:], 1)
	binary.LittleEndian.PutUint16(app[28:], 6)
	data := append(append(append([]byte{}, jpegData[:2]...), app...), jpegData[2:]...)
	thumb, err := decodeThumbnail(context.Background(), data)
	if err != nil || thumb.Bounds() != image.Rect(0, 0, 9, 18) {
		t.Fatalf("portrait EXIF: %v %v", thumb, err)
	}
	palette := color.Palette{color.RGBA{}, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(2, 2, 6, 6), palette)
	second := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	for i := range first.Pix {
		first.Pix[i] = 1
	}
	for i := range second.Pix {
		second.Pix[i] = 2
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}, Config: image.Config{ColorModel: palette, Width: 8, Height: 8}}); err != nil {
		t.Fatal(err)
	}
	thumb, err = decodeThumbnail(context.Background(), encoded.Bytes())
	if err != nil || thumb.Bounds() != image.Rect(0, 0, 18, 18) {
		t.Fatalf("GIF logical canvas: %v %v", thumb, err)
	}
	if got := thumb.RGBAAt(9, 9); got.R < 200 || got.B != 0 {
		t.Fatalf("GIF used later frame: %v", got)
	}
	if got := thumb.RGBAAt(0, 0); got.A != 0 {
		t.Fatalf("GIF lost transparent canvas: %v", got)
	}
}

func TestThumbnailReadUsesAnchorAndRejectsChangedSymlinkOversizedFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := thumbnailFixture(t, "png")
	if err := os.WriteFile(filepath.Join(root, "photo.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	reader, _, err := openReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	listing := reader.read(request{ctx: context.Background(), directory: true})
	req := request{ctx: context.Background(), path: "photo.png", thumbnail: true, thumbnailStamp: listing.entries[0].stamp}
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photo.png"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := reader.read(req); got.err != nil || got.thumbnailImage == nil {
		t.Fatalf("lost anchored photo: %v", got.err)
	}
	moved := root + "-moved"
	if err := os.Rename(filepath.Join(moved, "photo.png"), filepath.Join(moved, "old.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "photo.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if got := reader.read(req); got.err == nil || got.thumbnailImage != nil {
		t.Fatal("decoded replaced file against stale listing")
	}
	if err := os.Remove(filepath.Join(moved, "photo.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("old.png", filepath.Join(moved, "photo.png")); err != nil {
		t.Fatal(err)
	}
	if got := reader.read(req); got.err == nil {
		t.Fatal("followed photo symlink")
	}
	file, err := os.Create(filepath.Join(moved, "huge.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxThumbnailBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	listing = reader.read(request{ctx: context.Background(), directory: true})
	for _, item := range listing.entries {
		if item.name == "huge.png" {
			req.path, req.thumbnailStamp = item.name, item.stamp
		}
	}
	if got := reader.read(req); got.err == nil || got.thumbnailImage != nil {
		t.Fatal("read oversized photo")
	}
}

func TestProviderThumbnailsStayBoundedIdleAndNeverOpenViewer(t *testing.T) {
	p, r := controlledProvider(t)
	p.thumbnailsEnabled = true
	clock := time.Now()
	p.now = func() time.Time { return clock }
	listing := nextCall(t, r)
	entries := []entry{{name: "photo.png", stamp: fileStamp{inode: 1}}, {name: "second.png", stamp: fileStamp{inode: 2}}}
	listing.answer <- result{entries: entries}
	pollUntil(t, p, func() bool { return p.thumbnailPending })
	first := nextCall(t, r)
	if !first.request.thumbnail || first.request.openMedia || p.loadingFile || p.message == "" {
		t.Fatal("thumbnail acted as explicit open")
	}
	first.answer <- result{thumbnailImage: image.NewRGBA(image.Rect(0, 0, 18, 9))}
	pollUntil(t, p, func() bool { return p.thumbnailFor(entries[0]) != nil })
	second := nextCall(t, r)
	second.answer <- result{err: errors.New("invalid image")}
	pollUntil(t, p, func() bool { return !p.thumbnailPending })
	generation := p.generation
	for i := 0; i < 10; i++ {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	if p.generation != generation || len(p.thumbnails) != 2 {
		t.Fatal("repeated a failed decode or lost thumbnail cache")
	}
	clock = clock.Add(autoRefreshInterval + time.Second)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	refresh := nextCall(t, r)
	if !refresh.request.refresh {
		t.Fatal("thumbnail work postponed automatic refresh")
	}
	entries[0].stamp.inode++
	refresh.answer <- result{entries: entries}
	pollUntil(t, p, func() bool { return p.thumbnailPending })
	changed := nextCall(t, r)
	if changed.request.thumbnailStamp != entries[0].stamp {
		t.Fatal("changed photo did not invalidate thumbnail")
	}
	changed.answer <- result{thumbnailImage: image.NewRGBA(image.Rect(0, 0, 18, 18))}
	await(t, func() bool { return len(p.results) == 1 })
	p.navigate("other", "")
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(p.thumbnails) != 0 {
		t.Fatal("stale thumbnail reappeared after navigation")
	}
	_ = nextCall(t, r)
	for i := 0; i < maxThumbnails+20; i++ {
		p.thumbnailResult(&result{request: request{path: string(rune(0x400 + i))}})
	}
	if len(p.thumbnails) != maxThumbnails || len(p.thumbnailOrder) != maxThumbnails {
		t.Fatal("thumbnail cache grew beyond bound")
	}
}
