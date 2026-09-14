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
