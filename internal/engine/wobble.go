package engine

import (
	"math"
	"time"
)

// Compiz-style wobbly mesh. Vertices spring to rest + neighbors.
// One alloc on first high-tier move; then reused.
const (
	WobbleCols   = 8
	WobbleRows   = 6
	wobbleAnchor = 0.12
	wobbleSpring = 0.18
	wobbleDamp   = 0.84
	wobbleSnap   = 0.60
	wobbleVSnap  = 0.25
	wobbleVMax   = 18
	meshChromeL  = 6
	meshChromeT  = 28
	meshChromeR  = 6
	meshChromeB  = 6
	vertStride   = 6 // x y vx vy rx ry
)

type wobbleMesh struct {
	cols, rows             int
	boxX, boxY, boxW, boxH int
	v                      []float32
	f                      []float32 // Jacobi fx,fy — reused
	live                   bool
}

func (a *Actor) meshBox() (x, y, w, h int) {
	if a == nil {
		return 0, 0, 1, 1
	}
	w, h = a.Width, a.Height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if a.NoChrome {
		return a.X, a.Y, w, h
	}
	return a.X - meshChromeL, a.Y - meshChromeT, w + meshChromeL + meshChromeR, h + meshChromeT + meshChromeB
}

func (a *Actor) ensureMesh() *wobbleMesh {
	if a == nil {
		return nil
	}
	bx, by, bw, bh := a.meshBox()
	m := a.mesh
	need := WobbleCols * WobbleRows * vertStride
	if m == nil {
		m = &wobbleMesh{cols: WobbleCols, rows: WobbleRows, v: make([]float32, need), f: make([]float32, WobbleCols*WobbleRows*2)}
		a.mesh = m
		m.reset(bx, by, bw, bh)
		return m
	}
	if m.boxW != bw || m.boxH != bh || len(m.v) < need {
		if cap(m.v) < need {
			m.v = make([]float32, need)
		} else {
			m.v = m.v[:need]
		}
		if cap(m.f) < WobbleCols*WobbleRows*2 {
			m.f = make([]float32, WobbleCols*WobbleRows*2)
		} else {
			m.f = m.f[:WobbleCols*WobbleRows*2]
		}
		m.cols, m.rows = WobbleCols, WobbleRows
		m.reset(bx, by, bw, bh)
		return m
	}
	if m.boxX != bx || m.boxY != by {
		dx := float32(bx - m.boxX)
		dy := float32(by - m.boxY)
		for i := 0; i < m.cols*m.rows; i++ {
			o := i * vertStride
			m.v[o+4] += dx
			m.v[o+5] += dy
		}
		m.boxX, m.boxY = bx, by
	}
	return m
}

func (m *wobbleMesh) reset(x, y, w, h int) {
	m.boxX, m.boxY, m.boxW, m.boxH = x, y, w, h
	m.live = false
	cols, rows := m.cols, m.rows
	if cols < 2 {
		cols = 2
	}
	if rows < 2 {
		rows = 2
	}
	for j := 0; j < rows; j++ {
		for i := 0; i < cols; i++ {
			rx := float32(x) + float32(i*w)/float32(cols-1)
			ry := float32(y) + float32(j*h)/float32(rows-1)
			o := (j*cols + i) * vertStride
			m.v[o+0], m.v[o+1] = rx, ry
			m.v[o+2], m.v[o+3] = 0, 0
			m.v[o+4], m.v[o+5] = rx, ry
		}
	}
}

func (m *wobbleMesh) pinGrab(lx, ly int, chrome bool) {
	if m == nil {
		return
	}
	gx := float32(m.boxX + lx)
	gy := float32(m.boxY + ly)
	if chrome {
		gx += float32(meshChromeL)
		gy += float32(meshChromeT)
	}
	best, bestD := 0, float32(1e12)
	n := m.cols * m.rows
	for i := 0; i < n; i++ {
		o := i * vertStride
		dx, dy := m.v[o+4]-gx, m.v[o+5]-gy
		d := dx*dx + dy*dy
		if d < bestD {
			bestD, best = d, i
		}
	}
	o := best * vertStride
	m.v[o+0], m.v[o+1] = m.v[o+4], m.v[o+5]
	m.v[o+2], m.v[o+3] = 0, 0
}

func (m *wobbleMesh) step() {
	if m == nil {
		return
	}
	cols, rows := m.cols, m.rows
	nvert := cols * rows
	if cap(m.f) < nvert*2 {
		m.f = make([]float32, nvert*2)
	} else {
		m.f = m.f[:nvert*2]
	}
	maxOff, maxV := float32(0), float32(0)
	for sub := 0; sub < 2; sub++ {
		maxOff, maxV = 0, 0
		for j := 0; j < rows; j++ {
			for i := 0; i < cols; i++ {
				idx := j*cols + i
				o := idx * vertStride
				x, y := m.v[o], m.v[o+1]
				rx, ry := m.v[o+4], m.v[o+5]
				fx := wobbleAnchor * (rx - x)
				fy := wobbleAnchor * (ry - y)
				if i > 0 {
					n := (j*cols + i - 1) * vertStride
					fx += wobbleSpring * ((m.v[n] - x) - (m.v[n+4] - rx))
					fy += wobbleSpring * ((m.v[n+1] - y) - (m.v[n+5] - ry))
				}
				if i+1 < cols {
					n := (j*cols + i + 1) * vertStride
					fx += wobbleSpring * ((m.v[n] - x) - (m.v[n+4] - rx))
					fy += wobbleSpring * ((m.v[n+1] - y) - (m.v[n+5] - ry))
				}
				if j > 0 {
					n := ((j-1)*cols + i) * vertStride
					fx += wobbleSpring * ((m.v[n] - x) - (m.v[n+4] - rx))
					fy += wobbleSpring * ((m.v[n+1] - y) - (m.v[n+5] - ry))
				}
				if j+1 < rows {
					n := ((j+1)*cols + i) * vertStride
					fx += wobbleSpring * ((m.v[n] - x) - (m.v[n+4] - rx))
					fy += wobbleSpring * ((m.v[n+1] - y) - (m.v[n+5] - ry))
				}
				m.f[idx*2], m.f[idx*2+1] = fx, fy
			}
		}
		for i := 0; i < nvert; i++ {
			o := i * vertStride
			vx := (m.v[o+2] + m.f[i*2]) * wobbleDamp
			vy := (m.v[o+3] + m.f[i*2+1]) * wobbleDamp
			if vx > wobbleVMax {
				vx = wobbleVMax
			} else if vx < -wobbleVMax {
				vx = -wobbleVMax
			}
			if vy > wobbleVMax {
				vy = wobbleVMax
			} else if vy < -wobbleVMax {
				vy = -wobbleVMax
			}
			x := m.v[o] + vx
			y := m.v[o+1] + vy
			m.v[o], m.v[o+1], m.v[o+2], m.v[o+3] = x, y, vx, vy
			ox, oy := x-m.v[o+4], y-m.v[o+5]
			if d := float32(math.Hypot(float64(ox), float64(oy))); d > maxOff {
				maxOff = d
			}
			if d := float32(math.Hypot(float64(vx), float64(vy))); d > maxV {
				maxV = d
			}
		}
	}
	if maxOff < wobbleSnap && maxV < wobbleVSnap {
		m.snapRest()
		m.live = false
		return
	}
	m.live = true
}

func (m *wobbleMesh) snapRest() {
	n := m.cols * m.rows
	for i := 0; i < n; i++ {
		o := i * vertStride
		m.v[o], m.v[o+1] = m.v[o+4], m.v[o+5]
		m.v[o+2], m.v[o+3] = 0, 0
	}
}

// TickWobble integrates the deformable mesh. Grab (title drag) pins a vertex
// to the rest pose so the handle follows the pointer and the rest jelly-lags.
func (a *Actor) TickWobble(now time.Time, tier Tier) {
	if a == nil {
		return
	}
	if tier != TierHigh {
		if a.mesh != nil {
			a.mesh.live = false
		}
		a.wobbleOn = false
		return
	}
	m := a.ensureMesh()
	if m == nil {
		return
	}
	if a.GrabOn {
		m.pinGrab(a.GrabLX, a.GrabLY, !a.NoChrome)
	}
	m.step()
	if a.GrabOn {
		m.pinGrab(a.GrabLX, a.GrabLY, !a.NoChrome)
		m.live = true
	}
	_ = now
	a.wobbleOn = true
}

// MeshLive is true while the jelly has not settled.
func (a *Actor) MeshLive() bool {
	return a != nil && a.mesh != nil && a.mesh.live
}

// MeshBox is the chrome-inclusive rectangle the wobble/burn pack uses.
func (a *Actor) MeshBox() (x, y, w, h int) {
	return a.meshBox()
}

// MeshGrid writes current vertex x,y pairs into dst (reuses dst).
func (a *Actor) MeshGrid(dst []float32) (cols, rows int, pts []float32) {
	if a == nil || a.mesh == nil {
		return 0, 0, dst[:0]
	}
	m := a.mesh
	n := m.cols * m.rows
	if cap(dst) < n*2 {
		dst = make([]float32, n*2)
	} else {
		dst = dst[:n*2]
	}
	for i := 0; i < n; i++ {
		o := i * vertStride
		dst[i*2], dst[i*2+1] = m.v[o], m.v[o+1]
	}
	return m.cols, m.rows, dst
}

// BlitMesh affine-maps src onto the dest mesh (two triangles per cell).
func BlitMesh(dst []byte, dStride, dW, dH int, src []byte, sStride, sW, sH int, cols, rows int, pts []float32, alpha float64) {
	if src == nil || cols < 2 || rows < 2 || len(pts) < cols*rows*2 || sW < 1 || sH < 1 {
		return
	}
	if alpha <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	for j := 0; j < rows-1; j++ {
		for i := 0; i < cols-1; i++ {
			i00 := (j*cols + i) * 2
			i10 := (j*cols + i + 1) * 2
			i01 := ((j+1)*cols + i) * 2
			i11 := ((j+1)*cols + i + 1) * 2
			u0 := float32(i*sW) / float32(cols-1)
			u1 := float32((i+1)*sW) / float32(cols-1)
			v0 := float32(j*sH) / float32(rows-1)
			v1 := float32((j+1)*sH) / float32(rows-1)
			if u1 >= float32(sW) {
				u1 = float32(sW - 1)
			}
			if v1 >= float32(sH) {
				v1 = float32(sH - 1)
			}
			blitTri(dst, dStride, dW, dH, src, sStride, sW, sH, alpha,
				pts[i00], pts[i00+1], u0, v0,
				pts[i10], pts[i10+1], u1, v0,
				pts[i11], pts[i11+1], u1, v1)
			blitTri(dst, dStride, dW, dH, src, sStride, sW, sH, alpha,
				pts[i00], pts[i00+1], u0, v0,
				pts[i11], pts[i11+1], u1, v1,
				pts[i01], pts[i01+1], u0, v1)
		}
	}
}

func blitTri(dst []byte, dStride, dW, dH int, src []byte, sStride, sW, sH int, alpha float64,
	x0, y0, u0, v0, x1, y1, u1, v1, x2, y2, u2, v2 float32) {
	minX := int(math.Floor(float64(min3(x0, x1, x2))))
	maxX := int(math.Ceil(float64(max3(x0, x1, x2))))
	minY := int(math.Floor(float64(min3(y0, y1, y2))))
	maxY := int(math.Ceil(float64(max3(y0, y1, y2))))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX >= dW {
		maxX = dW - 1
	}
	if maxY >= dH {
		maxY = dH - 1
	}
	den := (y1-y2)*(x0-x2) + (x2-x1)*(y0-y2)
	if den > -1e-3 && den < 1e-3 {
		return
	}
	a := int(alpha*256 + 0.5)
	if a > 256 {
		a = 256
	}
	na := 256 - a
	for y := minY; y <= maxY; y++ {
		fy := float32(y) + 0.5
		for x := minX; x <= maxX; x++ {
			fx := float32(x) + 0.5
			w0 := ((y1-y2)*(fx-x2) + (x2-x1)*(fy-y2)) / den
			w1 := ((y2-y0)*(fx-x2) + (x0-x2)*(fy-y2)) / den
			w2 := 1 - w0 - w1
			if w0 < 0 || w1 < 0 || w2 < 0 {
				continue
			}
			sx := int(w0*u0 + w1*u1 + w2*u2)
			sy := int(w0*v0 + w1*v1 + w2*v2)
			if sx < 0 {
				sx = 0
			}
			if sy < 0 {
				sy = 0
			}
			if sx >= sW {
				sx = sW - 1
			}
			if sy >= sH {
				sy = sH - 1
			}
			si := sy*sStride + sx*4
			di := y*dStride + x*4
			if si+4 > len(src) || di+4 > len(dst) {
				continue
			}
			if a == 256 {
				dst[di+0] = src[si+0]
				dst[di+1] = src[si+1]
				dst[di+2] = src[si+2]
				dst[di+3] = src[si+3]
				continue
			}
			dst[di+0] = byte((int(src[si+0])*a + int(dst[di+0])*na) >> 8)
			dst[di+1] = byte((int(src[si+1])*a + int(dst[di+1])*na) >> 8)
			dst[di+2] = byte((int(src[si+2])*a + int(dst[di+2])*na) >> 8)
			dst[di+3] = byte((int(src[si+3])*a + int(dst[di+3])*na) >> 8)
		}
	}
}

func min3(a, b, c float32) float32 {
	if b < a {
		a = b
	}
	if c < a {
		return c
	}
	return a
}

func max3(a, b, c float32) float32 {
	if b > a {
		a = b
	}
	if c > a {
		return c
	}
	return a
}
