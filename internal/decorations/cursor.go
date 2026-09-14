package decorations

import "github.com/codemodify/worldr/internal/engine"

// CursorShapeText is wp_cursor_shape_v1.shape "text" (I-beam).
const CursorShapeText uint32 = 9

// DrawPointer paints a small server-side arrow at (x,y).
func DrawPointer(dst []byte, stride, dW, dH, x, y int) {
	// 11x17 classic-ish arrow in BGRA.
	const (
		white uint32 = 0xffffffff
		black uint32 = 0xff101018
	)
	pts := [][2]int{
		{0, 0}, {0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}, {0, 6}, {0, 7}, {0, 8}, {0, 9}, {0, 10}, {0, 11},
		{1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}, {1, 6}, {1, 7}, {1, 8}, {1, 9}, {1, 10},
		{2, 2}, {2, 3}, {2, 4}, {2, 5}, {2, 6}, {2, 7}, {2, 8},
		{3, 3}, {3, 4}, {3, 5}, {3, 6}, {3, 7},
		{4, 4}, {4, 5}, {4, 6}, {4, 10}, {4, 11},
		{5, 5}, {5, 6}, {5, 11}, {5, 12},
		{6, 6}, {6, 12}, {6, 13},
		{7, 13}, {7, 14},
	}
	outline := [][2]int{
		{0, 12}, {1, 11}, {2, 9}, {3, 8}, {3, 9}, {4, 7}, {4, 9}, {5, 7}, {5, 9}, {5, 10},
		{6, 7}, {6, 8}, {6, 9}, {6, 10}, {6, 11}, {7, 7}, {8, 14}, {8, 15},
	}
	for _, p := range pts {
		engine.FillRect(dst, stride, dW, dH, x+p[0], y+p[1], 1, 1, white)
	}
	for _, p := range outline {
		engine.FillRect(dst, stride, dW, dH, x+p[0], y+p[1], 1, 1, black)
	}
}

// DrawTextCaret paints a short I-beam for the text cursor shape.
func DrawTextCaret(dst []byte, stride, dW, dH, x, y int) {
	const col uint32 = 0xffe8e8f0
	engine.FillRect(dst, stride, dW, dH, x, y, 7, 2, col)
	engine.FillRect(dst, stride, dW, dH, x+3, y, 1, 14, col)
	engine.FillRect(dst, stride, dW, dH, x, y+14, 7, 2, col)
}

// OverlayCursor draws the software cursor. pix is an optional client shm/dmabuf
// image (BGRA); when nil, shape selects the built-in arrow or I-beam.
func OverlayCursor(dst []byte, stride, dW, dH, x, y, hx, hy int, pix []byte, cw, ch, cstride int, shape uint32) {
	dx, dy := x-hx, y-hy
	if len(pix) > 0 && cw > 0 && ch > 0 {
		blitCursor(dst, stride, dW, dH, dx, dy, pix, cstride, cw, ch)
		return
	}
	if shape == CursorShapeText {
		DrawTextCaret(dst, stride, dW, dH, dx, dy)
		return
	}
	DrawPointer(dst, stride, dW, dH, dx, dy)
}

func blitCursor(dst []byte, dStride, dW, dH, dx, dy int, src []byte, sStride, sW, sH int) {
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
			if src[si+3] == 0 {
				continue
			}
			dst[di+0] = src[si+0]
			dst[di+1] = src[si+1]
			dst[di+2] = src[si+2]
			dst[di+3] = src[si+3]
		}
	}
}
