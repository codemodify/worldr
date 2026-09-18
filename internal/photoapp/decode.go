package photoapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/codemodify/worldr/internal/resourcepath"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const (
	maxEncodedBytes = 64 << 20
	maxImageSide    = 16384
	maxImagePixels  = 40_000_000
)

type photoDecoder func(context.Context, *os.File) (image.Image, error)

type photoRequest struct {
	ctx        context.Context
	generation uint64
	file       *os.File
	name       string
}

type photoResult struct {
	generation uint64
	name       string
	path       string
	image      image.Image
	err        error
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

// decodePhoto belongs exclusively to the worker. The encoded size and header
// dimensions are checked before any codec allocates its pixel storage. A single
// worker bounds concurrent codec allocations; canceled codecs are discarded
// after returning, and context-aware reads/normalization stop cooperatively.
func decodePhoto(ctx context.Context, file *os.File) (image.Image, error) {
	return decodePhotoLimited(ctx, file, maxImagePixels)
}

func decodePhotoLimited(ctx context.Context, file *os.File, pixelBudget int64) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("only regular image files can be opened")
	}
	if info.Size() > maxEncodedBytes {
		return nil, fmt.Errorf("image file exceeds the 64 MiB limit")
	}
	data, err := io.ReadAll(contextReader{ctx, io.NewSectionReader(file, 0, maxEncodedBytes+1)})
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxEncodedBytes {
		return nil, fmt.Errorf("image file exceeds the 64 MiB limit")
	}
	config, format, err := image.DecodeConfig(contextReader{ctx, bytes.NewReader(data)})
	if err != nil {
		return nil, fmt.Errorf("read image header: %w", err)
	}
	switch format {
	case "jpeg", "png", "webp", "gif", "bmp":
	default:
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
	if err := validImageSize(config.Width, config.Height); err != nil {
		return nil, err
	}
	if int64(config.Width)*int64(config.Height) > pixelBudget {
		return nil, fmt.Errorf("photo windows exceed their combined 64 megapixel limit; close a photo and open this file again")
	}
	decoded, _, err := image.Decode(contextReader{ctx, bytes.NewReader(data)})
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, fmt.Errorf("image has no pixels")
	}
	bounds := decoded.Bounds()
	if err := validImageSize(bounds.Dx(), bounds.Dy()); err != nil {
		return nil, err
	}
	if bounds.Min.X < 0 || bounds.Min.Y < 0 || bounds.Max.X > config.Width || bounds.Max.Y > config.Height {
		return nil, fmt.Errorf("decoded image exceeds its header dimensions")
	}
	// GIF Decode returns its first image rectangle, which may cover only part
	// of the logical canvas. Preserve the canvas and its transparent remainder.
	if bounds != image.Rect(0, 0, config.Width, config.Height) {
		canvas := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
		draw.Draw(canvas, bounds, decoded, bounds.Min, draw.Src)
		decoded = canvas
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(data)
	}
	return orientPhoto(ctx, decoded, orientation)
}

func validImageSize(width, height int) error {
	if width < 1 || height < 1 || width > maxImageSide || height > maxImageSide || uint64(width)*uint64(height) > maxImagePixels {
		return fmt.Errorf("image dimensions exceed 16384 pixels per side or 40 megapixels")
	}
	return nil
}

// jpegOrientation reads only bounded APP1/TIFF metadata. Malformed or absent
// orientation data is ignored; it never triggers an auxiliary file or network
// read. Other EXIF fields, thumbnails and recursive IFD links are not followed.
func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for at := 2; at < len(data); {
		if data[at] != 0xff {
			return 1
		}
		for at < len(data) && data[at] == 0xff {
			at++
		}
		if at >= len(data) {
			return 1
		}
		marker := data[at]
		at++
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if at+2 > len(data) {
			return 1
		}
		length := int(binary.BigEndian.Uint16(data[at : at+2]))
		if length < 2 || length > len(data)-at {
			return 1
		}
		segment := data[at+2 : at+length]
		at += length
		if marker != 0xe1 || len(segment) < 14 || !bytes.Equal(segment[:6], []byte("Exif\x00\x00")) {
			continue
		}
		tiff := segment[6:]
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			continue
		}
		if order.Uint16(tiff[2:4]) != 42 {
			continue
		}
		offset := uint64(order.Uint32(tiff[4:8]))
		if offset+2 > uint64(len(tiff)) {
			continue
		}
		count := uint64(order.Uint16(tiff[offset : offset+2]))
		if count > (uint64(len(tiff))-offset-2)/12 {
			continue
		}
		for i := uint64(0); i < count; i++ {
			entry := tiff[offset+2+i*12 : offset+14+i*12]
			if order.Uint16(entry[:2]) == 0x0112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
				orientation := int(order.Uint16(entry[8:10]))
				if orientation >= 1 && orientation <= 8 {
					return orientation
				}
			}
		}
	}
	return 1
}

func orientPhoto(ctx context.Context, source image.Image, orientation int) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if orientation <= 1 || orientation > 8 {
		return source, nil
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	output := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < dw; x++ {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			output.Set(x, y, source.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return output, nil
}

func runPhotoDecoder(ctx context.Context, decode photoDecoder, requests chan photoRequest, results chan photoResult, done chan struct{}) {
	defer close(done)
	defer drainPhotoRequests(requests)
	defer drainPhotoResults(results)
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-requests:
			if request.ctx.Err() != nil {
				_ = request.file.Close()
				continue
			}
			// os.File supports concurrent Close/ReadAt. Closing on cancellation
			// also releases the descriptor while a bounded codec unwinds.
			stopClose := context.AfterFunc(request.ctx, func() { _ = request.file.Close() })
			decoded, err := decode(request.ctx, request.file)
			path := ""
			// Fd(), unlike ReadAt/Stat, must not race a concurrent Close. Stop
			// the cancellation closer before resolving the final descriptor path;
			// if it already started, this canceled result will be discarded.
			if stopClose() {
				path, _ = resourcepath.FromFile(request.file)
			}
			_ = request.file.Close()
			if request.ctx.Err() != nil || ctx.Err() != nil {
				continue
			}
			result := photoResult{generation: request.generation, name: request.name, path: path, image: decoded, err: err}
			select {
			case results <- result:
			case <-request.ctx.Done():
			case <-ctx.Done():
				return
			}
		}
	}
}

func drainPhotoRequests(requests chan photoRequest) {
	for {
		select {
		case request := <-requests:
			_ = request.file.Close()
		default:
			return
		}
	}
}
func drainPhotoResults(results chan photoResult) {
	for {
		select {
		case <-results:
		default:
			return
		}
	}
}
