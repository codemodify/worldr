package engine

import (
	"math"
	"time"
)

// Compiz-style burn / fire dissolve on map-out (--effects=high).
const (
	BurnDuration = 720 * time.Millisecond
	BurnSparks   = 72
)

// MapOutLen is how long an unmap animation lasts at this theater tier.
func MapOutLen(tier Tier) time.Duration {
	if tier == TierHigh {
		return BurnDuration
	}
	return MapOutDuration
}

type spark struct {
	x, y, vx, vy, life float32
}

type burnState struct {
	sparks [BurnSparks]spark
	seeded bool
}

func (a *Actor) ensureBurn() *burnState {
	if a == nil {
		return nil
	}
	if a.burn == nil {
		a.burn = &burnState{}
	}
	return a.burn
}

// TickBurn seeds and integrates rising embers while the window is burning.
func (a *Actor) TickBurn(now time.Time, tier Tier) {
	if a == nil || tier != TierHigh || a.UnmapAt.IsZero() {
		return
	}
	p := float64(now.Sub(a.UnmapAt)) / float64(BurnDuration)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	b := a.ensureBurn()
	bx, by, bw, bh := a.meshBox()
	if !b.seeded {
		b.seeded = true
		for i := range b.sparks {
			u := hash01(i*13, int(a.UnmapAt.UnixNano()))
			v := hash01(i*7+3, int(a.UnmapAt.UnixNano()>>3))
			b.sparks[i] = spark{
				x:    float32(bx) + float32(u)*float32(bw),
				y:    float32(by+bh) - float32(v)*18,
				vx:   float32(u-0.5) * 1.6,
				vy:   -1.2 - float32(v)*2.4,
				life: 0.35 + float32(u)*0.65,
			}
		}
	}
	front := float32(by+bh) - float32(p)*float32(bh+36)
	for i := range b.sparks {
		s := &b.sparks[i]
		s.x += s.vx
		s.y += s.vy
		s.vy -= 0.08
		s.life -= 0.018
		if s.life <= 0 || s.y < front-40 {
			u := hash01(i*17+int(now.UnixNano()), i+int(p*1000))
			v := hash01(i*31, int(p*777)+i)
			s.x = float32(bx) + float32(u)*float32(bw)
			s.y = front + float32(v)*10
			s.vx = float32(u-0.5) * 2
			s.vy = -1.8 - float32(v)*2.8
			s.life = 0.4 + float32(v)*0.6
		}
	}
}

func hash01(x, y int) float64 {
	n := uint32(x*374761393 + y*668265263)
	n = (n ^ (n >> 13)) * 1274126177
	return float64(n&0xffff) / 65535
}

// BurnShade classifies one source pixel: 0 gone, 1 ember, 2 intact (tinted).
func BurnShade(x, y, w, h int, progress float64) (kind int, fire float64) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	n := hash01(x, y)
	fromBottom := 1 - float64(y)/float64(h)
	front := progress*1.15 + n*0.18
	if fromBottom < front-0.10 {
		return 0, 0
	}
	if fromBottom < front+0.06 {
		return 1, clamp01((front + 0.06 - fromBottom) / 0.16)
	}
	return 2, 0
}

// DrawBurn dissolves src (window pack) into dest at (dx,dy) and paints sparks.
func DrawBurn(dst []byte, dStride, dW, dH, dx, dy int, src []byte, sStride, sW, sH int, progress float64, a *Actor) {
	if src == nil || sW < 1 || sH < 1 {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
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
			kind, fire := BurnShade(x, y, sW, sH, progress)
			if kind == 0 {
				continue
			}
			si := y*sStride + x*4
			di := yy*dStride + xx*4
			if si+4 > len(src) || di+4 > len(dst) {
				continue
			}
			b0, b1, b2 := int(src[si+0]), int(src[si+1]), int(src[si+2])
			if kind == 1 {
				// BGRA: ember yellow/orange
				hot := int(180 + 70*fire)
				b0 = hot / 5
				b1 = hot * 2 / 3
				b2 = hot
				a8 := int((0.55 + 0.45*fire) * 256)
				na := 256 - a8
				dst[di+0] = byte((b0*a8 + int(dst[di+0])*na) >> 8)
				dst[di+1] = byte((b1*a8 + int(dst[di+1])*na) >> 8)
				dst[di+2] = byte((b2*a8 + int(dst[di+2])*na) >> 8)
				continue
			}
			// intact: slight red shift as the front approaches
			tint := progress * 0.35
			b0 = b0 * 4 / 5
			b1 = b1 * 5 / 6
			b2 = b2 + int(float64(255-b2)*tint)
			if b2 > 255 {
				b2 = 255
			}
			dst[di+0] = byte(b0)
			dst[di+1] = byte(b1)
			dst[di+2] = byte(b2)
			dst[di+3] = src[si+3]
		}
	}
	if a == nil || a.burn == nil {
		return
	}
	for i := range a.burn.sparks {
		s := a.burn.sparks[i]
		if s.life <= 0 {
			continue
		}
		sz := 2
		if s.life > 0.55 {
			sz = 3
		}
		hot := 0.45 + 0.55*float64(s.life)
		pix := uint32(0xff) | uint32(int(160*hot))<<8 | uint32(int(40+200*hot))<<16 | 0xff<<24
		FillRectAlpha(dst, dStride, dW, dH, int(s.x), int(s.y), sz, sz, pix, math.Min(1, hot))
	}
}
