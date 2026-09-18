package photoapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/bmp"
)

func photoTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestDecodePhotoSupportedFormatsAndDescriptorOrigin(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 190, G: 40, B: 80, A: 255})
		}
	}
	for _, format := range []string{"jpeg", "png", "gif", "bmp", "webp"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			var err error
			switch format {
			case "jpeg":
				err = jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100})
			case "png":
				err = png.Encode(&encoded, source)
			case "gif":
				err = gif.Encode(&encoded, source, nil)
			case "bmp":
				err = bmp.Encode(&encoded, source)
			case "webp":
				// A generated, lossless 2x2 red fixture; no external encoder or
				// assets are needed when running the pure-Go test suite.
				data, decodeErr := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ9Y/+ByKi/wEA")
				err = decodeErr
				encoded.Write(data)
			}
			if err != nil {
				t.Fatal(err)
			}
			file := photoTestFile(t, encoded.Bytes())
			if _, err := file.Seek(0, 2); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(file.Name(), file.Name()+".renamed"); err != nil {
				t.Fatal(err)
			}
			photo, err := decodePhoto(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			want := source.Bounds()
			if format == "webp" {
				want = image.Rect(0, 0, 2, 2)
			}
			if photo.Bounds() != want {
				t.Fatalf("%s bounds %v, want %v", format, photo.Bounds(), want)
			}
			r, _, _, a := photo.At(0, 0).RGBA()
			if r < 150<<8 || a != 65535 {
				t.Fatalf("%s lost image pixels", format)
			}
		})
	}
}

func TestDecodeGIFFirstFrameKeepsLogicalCanvas(t *testing.T) {
	palette := color.Palette{color.NRGBA{A: 0}, color.NRGBA{R: 230, A: 255}, color.NRGBA{B: 230, A: 255}}
	first := image.NewPaletted(image.Rect(1, 1, 3, 3), palette)
	second := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	for i := range first.Pix {
		first.Pix[i] = 1
	}
	for i := range second.Pix {
		second.Pix[i] = 2
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{5, 5}, Config: image.Config{ColorModel: palette, Width: 4, Height: 4}}); err != nil {
		t.Fatal(err)
	}
	photo, err := decodePhoto(context.Background(), photoTestFile(t, encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if photo.Bounds() != image.Rect(0, 0, 4, 4) {
		t.Fatal("first GIF frame lost logical canvas")
	}
	r, _, b, a := photo.At(1, 1).RGBA()
	if r == 0 || b != 0 || a != 65535 {
		t.Fatal("GIF decoder displayed a later frame")
	}
	_, _, _, a = photo.At(0, 0).RGBA()
	if a != 0 {
		t.Fatal("GIF's unpainted logical canvas lost transparency")
	}
}

func TestPhotoOrientationAllEXIFTransforms(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for i := 0; i < 6; i++ {
		source.SetNRGBA(i%2, i/2, color.NRGBA{R: byte('A' + i), A: 255})
	}
	expected := []string{"ABCDEF", "BADCFE", "FEDCBA", "EFCDAB", "ACEBDF", "ECAFDB", "FDBECA", "BDFACE"}
	for orientation := 1; orientation <= 8; orientation++ {
		photo, err := orientPhoto(context.Background(), source, orientation)
		if err != nil {
			t.Fatal(err)
		}
		var got strings.Builder
		for y := 0; y < photo.Bounds().Dy(); y++ {
			for x := 0; x < photo.Bounds().Dx(); x++ {
				value, _, _, _ := photo.At(x, y).RGBA()
				got.WriteByte(byte(value >> 8))
			}
		}
		if got.String() != expected[orientation-1] {
			t.Fatalf("orientation %d: %q, want %q", orientation, got.String(), expected[orientation-1])
		}
	}
}

func exifSegment(orientation uint16, order binary.ByteOrder) []byte {
	tiff := make([]byte, 26)
	copy(tiff, "II")
	if order == binary.BigEndian {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:4], 42)
	order.PutUint32(tiff[4:8], 8)
	order.PutUint16(tiff[8:10], 1)
	order.PutUint16(tiff[10:12], 0x112)
	order.PutUint16(tiff[12:14], 3)
	order.PutUint32(tiff[14:18], 1)
	order.PutUint16(tiff[18:20], orientation)
	segment := []byte{0xff, 0xe1, 0, byte(len(tiff) + 8), 'E', 'x', 'i', 'f', 0, 0}
	return append(segment, tiff...)
}

func TestDecodeJPEGUsesBoundedEXIFOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 12, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 12; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: byte(x * 15), G: byte(y * 30), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	plain, err := decodePhoto(context.Background(), photoTestFile(t, encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := append([]byte{0xff, 0xd8}, exifSegment(6, order)...)
		data = append(data, encoded.Bytes()[2:]...)
		photo, err := decodePhoto(context.Background(), photoTestFile(t, data))
		if err != nil {
			t.Fatal(err)
		}
		if photo.Bounds() != image.Rect(0, 0, 6, 12) {
			t.Fatal("JPEG orientation did not swap portrait dimensions")
		}
		want := color.NRGBAModel.Convert(plain.At(0, 5)).(color.NRGBA)
		got := color.NRGBAModel.Convert(photo.At(0, 0)).(color.NRGBA)
		if got != want {
			t.Fatalf("JPEG rotation pixels: got %v, want %v", got, want)
		}
		for end := 0; end < len(data); end++ {
			_ = jpegOrientation(data[:end])
		}
	}
	bad := append([]byte{0xff, 0xd8}, exifSegment(6, binary.LittleEndian)...)
	binary.LittleEndian.PutUint32(bad[16:20], ^uint32(0))
	if jpegOrientation(bad) != 1 {
		t.Fatal("accepted an out-of-bounds EXIF offset")
	}
	if jpegOrientation(append([]byte{0xff, 0xd8}, exifSegment(9, binary.LittleEndian)...)) != 1 {
		t.Fatal("accepted invalid EXIF orientation")
	}
}

func TestDecodePhotoRejectsOversizeHeadersAndCanceledReads(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{16385, 1}, {10000, 5000}} {
		data := bytes.Clone(encoded.Bytes())
		binary.BigEndian.PutUint32(data[16:20], uint32(size[0]))
		binary.BigEndian.PutUint32(data[20:24], uint32(size[1]))
		binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		if _, err := decodePhoto(context.Background(), photoTestFile(t, data)); err == nil || !strings.Contains(err.Error(), "40 megapixels") {
			t.Fatalf("size %v was not rejected before decoding: %v", size, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodePhoto(ctx, photoTestFile(t, encoded.Bytes())); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled decode: %v", err)
	}
	if _, err := decodePhoto(context.Background(), photoTestFile(t, []byte("not an image"))); err == nil {
		t.Fatal("accepted malformed image")
	}
	path := filepath.Join(t.TempDir(), "large.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(maxEncodedBytes + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := decodePhoto(context.Background(), file); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversize encoded input accepted: %v", err)
	}
}
