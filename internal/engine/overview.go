package engine

import "math"

// OverviewDuration is the expose enter/leave ease.
const OverviewDuration = MapInDuration // 260ms, same theater family

// GridCell is one expose slot in screen space.
type GridCell struct {
	X, Y, W, H int
}

// GridCols is the expose column count (ceil(sqrt(n))).
func GridCols(n int) int {
	if n <= 1 {
		return 1
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	if cols < 1 {
		return 1
	}
	return cols
}

// LayoutGrid tiles n slots into the screen. Empty n → nil.
func LayoutGrid(n, screenW, screenH int) []GridCell {
	return LayoutGridInto(nil, n, screenW, screenH)
}

// LayoutGridInto writes n cells into dst (reuses cap). Empty n → nil when dst is nil.
func LayoutGridInto(dst []GridCell, n, screenW, screenH int) []GridCell {
	if n <= 0 || screenW <= 0 || screenH <= 0 {
		if dst == nil {
			return nil
		}
		return dst[:0]
	}
	cols := GridCols(n)
	rows := (n + cols - 1) / cols
	margin := overviewMargin(screenW, screenH, cols, rows)
	gap := margin
	innerW := screenW - margin*2 - gap*(cols-1)
	innerH := screenH - margin*2 - gap*(rows-1)
	if innerW < cols {
		innerW = cols
	}
	if innerH < rows {
		innerH = rows
	}
	cw := innerW / cols
	ch := innerH / rows
	if cap(dst) < n {
		dst = make([]GridCell, n)
	} else {
		dst = dst[:n]
	}
	for i := 0; i < n; i++ {
		c := i % cols
		r := i / cols
		dst[i] = GridCell{
			X: margin + c*(cw+gap),
			Y: margin + r*(ch+gap),
			W: cw,
			H: ch,
		}
	}
	return dst
}

// ScaleCell grows or shrinks a cell about its center. s==1 is identity.
func ScaleCell(c GridCell, s float64) GridCell {
	if s == 1 || c.W < 1 || c.H < 1 {
		return c
	}
	nw := int(float64(c.W)*s + 0.5)
	nh := int(float64(c.H)*s + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return GridCell{
		X: c.X + (c.W-nw)/2,
		Y: c.Y + (c.H-nh)/2,
		W: nw,
		H: nh,
	}
}

func overviewMargin(screenW, screenH, cols, rows int) int {
	m := 56
	if screenW < 640 || screenH < 400 {
		m = 24
	}
	if screenW < 400 {
		m = 12
	}
	_ = cols
	_ = rows
	return m
}

// FitInCell letterboxes a src box into cell, centered.
func FitInCell(srcW, srcH int, cell GridCell) GridCell {
	if srcW <= 0 || srcH <= 0 || cell.W <= 0 || cell.H <= 0 {
		return cell
	}
	pad := 8
	cw, ch := cell.W-2*pad, cell.H-2*pad
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	// scale = min(cw/srcW, ch/srcH)
	sw := cw
	sh := srcH * cw / srcW
	if sh > ch {
		sh = ch
		sw = srcW * ch / srcH
	}
	if sw < 1 {
		sw = 1
	}
	if sh < 1 {
		sh = 1
	}
	return GridCell{
		X: cell.X + (cell.W-sw)/2,
		Y: cell.Y + (cell.H-sh)/2,
		W: sw,
		H: sh,
	}
}

// LerpCell interpolates two rects. t is 0..1.
func LerpCell(a, b GridCell, t float64) GridCell {
	t = clamp01(t)
	return GridCell{
		X: lerpI(a.X, b.X, t),
		Y: lerpI(a.Y, b.Y, t),
		W: lerpI(a.W, b.W, t),
		H: lerpI(a.H, b.H, t),
	}
}

func lerpI(a, b int, t float64) int {
	return int(float64(a) + (float64(b)-float64(a))*t + 0.5)
}

// HitGrid returns the cell index containing (px,py), or -1.
func HitGrid(cells []GridCell, px, py int) int {
	for i, c := range cells {
		if px >= c.X && px < c.X+c.W && py >= c.Y && py < c.Y+c.H {
			return i
		}
	}
	return -1
}

// HomeFrame is the SSD-framed rect of an actor (or the buffer if no chrome).
func HomeFrame(a *Actor, ssd bool, border, titleH int) GridCell {
	if a == nil {
		return GridCell{}
	}
	if !ssd || a.NoChrome {
		return GridCell{X: a.X, Y: a.Y, W: a.Width, H: a.Height}
	}
	return GridCell{
		X: a.X - border,
		Y: a.Y - titleH,
		W: a.Width + 2*border,
		H: a.Height + titleH + border,
	}
}
