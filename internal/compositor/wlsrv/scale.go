package wlsrv

import "math"

// PreferredScale120ths is 1.0 in wp_fractional_scale_v1 units (scale × 120).
const PreferredScale120ths uint32 = 120

// ScaleTo120ths converts a display scale (1, 1.25, 1.5, 2, …) to protocol units.
// Non-positive values become 1.0 (120).
func ScaleTo120ths(scale float64) uint32 {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return PreferredScale120ths
	}
	n := math.Round(scale * 120)
	if n < 1 {
		return 1
	}
	if n > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n)
}

// ScaleFrom120ths is the floating scale encoded by n.
func ScaleFrom120ths(n uint32) float64 {
	if n == 0 {
		return 1
	}
	return float64(n) / 120
}

// CombineHostScale prefers a wp_fractional_scale preferred_scale (120ths)
// over a wl_output.scale integer. Either 0 falls through; both 0 → 1.0.
func CombineHostScale(frac120 uint32, outputScale int32) float64 {
	if frac120 > 0 {
		return ScaleFrom120ths(frac120)
	}
	if outputScale > 0 {
		return float64(outputScale)
	}
	return 1
}

// IntegerScaleFrom120ths is the coherent wl_output.scale (nearest integer, min 1).
// 1.0 → 1, 1.25 → 1, 1.5 → 2, 2.0 → 2.
func IntegerScaleFrom120ths(n uint32) int32 {
	if n == 0 {
		return 1
	}
	v := (n + 60) / 120
	if v < 1 {
		return 1
	}
	return int32(v)
}

// LogicalSize is the compositor window size for a buffer.
// Viewport destination wins; otherwise buffer pixels are divided by buffer scale.
func LogicalSize(bufW, bufH, destW, destH, bufScale int) (w, h int) {
	return LogicalSizeScaled(bufW, bufH, destW, destH, bufScale, 0)
}

// LogicalSizeScaled is LogicalSize plus a wp_fractional_scale hint (120ths).
// When destination and integer buffer scale are unset, a preferred scale
// above 1.0 converts buffer pixels into logical units (GTK4 at 1.75).
func LogicalSizeScaled(bufW, bufH, destW, destH, bufScale int, scale120 uint32) (w, h int) {
	if destW > 0 && destH > 0 {
		return destW, destH
	}
	if bufScale < 1 {
		bufScale = 1
	}
	if bufScale == 1 && scale120 > PreferredScale120ths && bufW > 0 && bufH > 0 {
		s := ScaleFrom120ths(scale120)
		w = int(math.Round(float64(bufW) / s))
		h = int(math.Round(float64(bufH) / s))
	} else {
		w, h = bufW/bufScale, bufH/bufScale
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// GeoRect is xdg_surface.set_window_geometry in surface-local coordinates.
type GeoRect struct {
	X, Y, W, H int
	Set        bool
}

// ApplyWindowGeometry maps a client buffer onto the decorated client area.
// dest / buffer-scale / preferred_scale pick the logical surface; a set
// geometry crops CSD shadow padding so SSD sits on the real window edge.
func ApplyWindowGeometry(bufW, bufH, destW, destH, bufScale int, scale120 uint32, geo GeoRect) (logW, logH, srcX, srcY, srcW, srcH int) {
	logW, logH = LogicalSizeScaled(bufW, bufH, destW, destH, bufScale, scale120)
	srcX, srcY, srcW, srcH = 0, 0, bufW, bufH
	if bufW < 1 || bufH < 1 {
		return logW, logH, 0, 0, bufW, bufH
	}
	if !geo.Set || geo.W < 1 || geo.H < 1 {
		return logW, logH, srcX, srcY, srcW, srcH
	}
	gx, gy, gw, gh := geo.X, geo.Y, geo.W, geo.H
	if gx < 0 {
		gw += gx
		gx = 0
	}
	if gy < 0 {
		gh += gy
		gy = 0
	}
	if gx >= logW || gy >= logH {
		return logW, logH, 0, 0, bufW, bufH
	}
	if gx+gw > logW {
		gw = logW - gx
	}
	if gy+gh > logH {
		gh = logH - gy
	}
	if gw < 1 || gh < 1 {
		return logW, logH, 0, 0, bufW, bufH
	}
	sx := float64(bufW) / float64(logW)
	sy := float64(bufH) / float64(logH)
	srcX = int(math.Round(float64(gx) * sx))
	srcY = int(math.Round(float64(gy) * sy))
	srcW = int(math.Round(float64(gw) * sx))
	srcH = int(math.Round(float64(gh) * sy))
	if srcX < 0 {
		srcX = 0
	}
	if srcY < 0 {
		srcY = 0
	}
	if srcW < 1 {
		srcW = 1
	}
	if srcH < 1 {
		srcH = 1
	}
	if srcX+srcW > bufW {
		srcW = bufW - srcX
	}
	if srcY+srcH > bufH {
		srcH = bufH - srcY
	}
	if srcW < 1 || srcH < 1 {
		return gw, gh, 0, 0, bufW, bufH
	}
	return gw, gh, srcX, srcY, srcW, srcH
}

// CropBGRA copies a (x,y,cw,ch) sub-rect from a BGRA buffer.
func CropBGRA(pix []byte, stride, w, h, x, y, cw, ch int) (out []byte, outStride int) {
	if pix == nil || cw < 1 || ch < 1 || w < 1 || h < 1 {
		return pix, stride
	}
	if x <= 0 && y <= 0 && cw == w && ch == h {
		return pix, stride
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x+cw > w {
		cw = w - x
	}
	if y+ch > h {
		ch = h - y
	}
	if cw < 1 || ch < 1 {
		return pix, stride
	}
	outStride = cw * 4
	out = make([]byte, outStride*ch)
	for row := 0; row < ch; row++ {
		srcOff := (y+row)*stride + x*4
		dstOff := row * outStride
		n := outStride
		if srcOff < 0 || srcOff >= len(pix) {
			continue
		}
		if srcOff+n > len(pix) {
			n = len(pix) - srcOff
		}
		if n > 0 {
			copy(out[dstOff:dstOff+n], pix[srcOff:srcOff+n])
		}
	}
	return out, outStride
}
