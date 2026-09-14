package engine

// FillBGRA writes a solid color into dst (row-major, stride bytes per row).
func FillBGRA(dst []byte, stride, w, h int, pixel uint32) {
	b0 := byte(pixel)
	b1 := byte(pixel >> 8)
	b2 := byte(pixel >> 16)
	b3 := byte(pixel >> 24)
	for y := 0; y < h; y++ {
		row := dst[y*stride : y*stride+w*4]
		for x := 0; x < w; x++ {
			i := x * 4
			row[i+0] = b0
			row[i+1] = b1
			row[i+2] = b2
			row[i+3] = b3
		}
	}
}

// BlitBGRA copies src onto dst at (dx,dy), clipping to the destination.
func BlitBGRA(dst []byte, dStride, dW, dH, dx, dy int, src []byte, sStride, sW, sH int) {
	if src == nil || sW <= 0 || sH <= 0 {
		return
	}
	for y := 0; y < sH; y++ {
		yy := dy + y
		if yy < 0 || yy >= dH {
			continue
		}
		for x := 0; x < sW; x++ {
			xx := dx + x
			if xx < 0 || xx >= dW {
				continue
			}
			si := y*sStride + x*4
			di := yy*dStride + xx*4
			if si+4 > len(src) || di+4 > len(dst) {
				continue
			}
			dst[di+0] = src[si+0]
			dst[di+1] = src[si+1]
			dst[di+2] = src[si+2]
			dst[di+3] = src[si+3]
		}
	}
}

// BlitBGRAScaledAlpha nearest-neighbor scales src into (dx,dy,dw,dh) and
// blends with uniform alpha (0..1). dw/dh == src size is the fade-only path.
func BlitBGRAScaledAlpha(dst []byte, dStride, dW, dH, dx, dy, dw, dh int, src []byte, sStride, sW, sH int, alpha float64) {
	if src == nil || sW <= 0 || sH <= 0 || dw <= 0 || dh <= 0 || alpha <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	a := int(alpha*256 + 0.5)
	if a > 256 {
		a = 256
	}
	na := 256 - a
	for y := 0; y < dh; y++ {
		yy := dy + y
		if yy < 0 || yy >= dH {
			continue
		}
		sy := y * sH / dh
		for x := 0; x < dw; x++ {
			xx := dx + x
			if xx < 0 || xx >= dW {
				continue
			}
			sx := x * sW / dw
			si := sy*sStride + sx*4
			di := yy*dStride + xx*4
			if si+4 > len(src) || di+4 > len(dst) {
				continue
			}
			dst[di+0] = byte((int(src[si+0])*a + int(dst[di+0])*na) >> 8)
			dst[di+1] = byte((int(src[si+1])*a + int(dst[di+1])*na) >> 8)
			dst[di+2] = byte((int(src[si+2])*a + int(dst[di+2])*na) >> 8)
			dst[di+3] = byte((int(src[si+3])*a + int(dst[di+3])*na) >> 8)
		}
	}
}

// FillRectAlpha writes a solid rectangle blended with alpha (0..1).
func FillRectAlpha(dst []byte, stride, dW, dH, x, y, w, h int, pixel uint32, alpha float64) {
	if alpha <= 0 || w <= 0 || h <= 0 {
		return
	}
	if alpha >= 1 {
		FillRect(dst, stride, dW, dH, x, y, w, h, pixel)
		return
	}
	a := int(alpha*256 + 0.5)
	na := 256 - a
	b0 := int(byte(pixel))
	b1 := int(byte(pixel >> 8))
	b2 := int(byte(pixel >> 16))
	b3 := int(byte(pixel >> 24))
	for yy := y; yy < y+h; yy++ {
		if yy < 0 || yy >= dH {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx < 0 || xx >= dW {
				continue
			}
			i := yy*stride + xx*4
			if i+4 > len(dst) {
				continue
			}
			dst[i+0] = byte((b0*a + int(dst[i+0])*na) >> 8)
			dst[i+1] = byte((b1*a + int(dst[i+1])*na) >> 8)
			dst[i+2] = byte((b2*a + int(dst[i+2])*na) >> 8)
			dst[i+3] = byte((b3*a + int(dst[i+3])*na) >> 8)
		}
	}
}

// FillRect writes a solid rectangle.
func FillRect(dst []byte, stride, dW, dH, x, y, w, h int, pixel uint32) {
	b0 := byte(pixel)
	b1 := byte(pixel >> 8)
	b2 := byte(pixel >> 16)
	b3 := byte(pixel >> 24)
	for yy := y; yy < y+h; yy++ {
		if yy < 0 || yy >= dH {
			continue
		}
		for xx := x; xx < x+w; xx++ {
			if xx < 0 || xx >= dW {
				continue
			}
			i := yy*stride + xx*4
			if i+4 > len(dst) {
				continue
			}
			dst[i+0] = b0
			dst[i+1] = b1
			dst[i+2] = b2
			dst[i+3] = b3
		}
	}
}
