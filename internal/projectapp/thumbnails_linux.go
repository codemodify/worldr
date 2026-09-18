//go:build linux

package projectapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
)

const maxThumbnailBytes = 8 << 20
const maxThumbnailPixels = 16 << 20

type thumbnailReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r thumbnailReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func (r *directoryReader) readThumbnail(req request) result {
	res := result{request: req}
	file, err := r.open(req.ctx, req.path, false)
	if err != nil {
		res.err = err
		return res
	}
	defer file.Close()
	stop := context.AfterFunc(req.ctx, func() { _ = file.Close() })
	defer stop()
	var stat unix.Stat_t
	if err = withFileDescriptor(file, func(fd int) error { return unix.Fstat(fd, &stat) }); err != nil {
		res.err = err
		return res
	}
	before := stampOf(&stat)
	if before.mode&unix.S_IFMT != unix.S_IFREG || before != req.thumbnailStamp {
		res.err = fmt.Errorf("photo changed before thumbnail read")
		return res
	}
	if before.size > maxThumbnailBytes {
		res.err = fmt.Errorf("thumbnail encoded size exceeds 8 MiB")
		return res
	}
	data, err := io.ReadAll(io.LimitReader(thumbnailReader{req.ctx, file}, maxThumbnailBytes+1))
	if err != nil {
		res.err = err
		return res
	}
	if len(data) > maxThumbnailBytes {
		res.err = fmt.Errorf("thumbnail encoded size exceeds 8 MiB")
		return res
	}
	res.thumbnailImage, res.err = decodeThumbnail(req.ctx, data)
	if res.err == nil {
		if err = withFileDescriptor(file, func(fd int) error { return unix.Fstat(fd, &stat) }); err != nil || before != stampOf(&stat) {
			res.thumbnailImage = nil
			res.err = fmt.Errorf("photo changed during thumbnail read")
		}
	}
	return res
}

func decodeThumbnail(ctx context.Context, data []byte) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, _, err := image.DecodeConfig(thumbnailReader{ctx, bytes.NewReader(data)})
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > maxThumbnailPixels {
		return nil, fmt.Errorf("thumbnail image dimensions exceed the bounded preview limit")
	}
	source, format, err := image.Decode(thumbnailReader{ctx, bytes.NewReader(data)})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	canvas := image.Rect(0, 0, config.Width, config.Height)
	if format == "gif" && bounds.In(canvas) {
		source = thumbnailCanvas{Image: source, bounds: canvas}
		bounds = canvas
	} else if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return nil, fmt.Errorf("thumbnail image dimensions changed while decoding")
	}
	w, h := thumbnailSide, thumbnailSide
	if bounds.Dx() > bounds.Dy() {
		h = max(1, thumbnailSide*bounds.Dy()/bounds.Dx())
	} else {
		w = max(1, thumbnailSide*bounds.Dx()/bounds.Dy())
	}
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), source, bounds, xdraw.Src, nil)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if format == "jpeg" {
		thumb = orientThumbnail(thumb, thumbnailOrientation(data))
	}
	return thumb, nil
}

type thumbnailCanvas struct {
	image.Image
	bounds image.Rectangle
}

func (c thumbnailCanvas) Bounds() image.Rectangle { return c.bounds }

func orientThumbnail(source *image.RGBA, orientation int) *image.RGBA {
	if orientation <= 1 || orientation > 8 {
		return source
	}
	w, h := source.Rect.Dx(), source.Rect.Dy()
	rw, rh := w, h
	if orientation >= 5 {
		rw, rh = h, w
	}
	result := image.NewRGBA(image.Rect(0, 0, rw, rh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			tx, ty := x, y
			switch orientation {
			case 2:
				tx = w - 1 - x
			case 3:
				tx, ty = w-1-x, h-1-y
			case 4:
				ty = h - 1 - y
			case 5:
				tx, ty = y, x
			case 6:
				tx, ty = h-1-y, x
			case 7:
				tx, ty = h-1-y, w-1-x
			case 8:
				tx, ty = y, w-1-x
			}
			result.SetRGBA(tx, ty, source.RGBAAt(x, y))
		}
	}
	return result
}

// thumbnailOrientation reads only bounded APP1/TIFF metadata. Malformed or absent
// orientation data is ignored; it never triggers an auxiliary file or network
// read. Other EXIF fields, thumbnails and recursive IFD links are not followed.
func thumbnailOrientation(data []byte) int {
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
