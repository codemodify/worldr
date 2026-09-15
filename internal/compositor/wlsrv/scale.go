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
	if destW > 0 && destH > 0 {
		return destW, destH
	}
	if bufScale < 1 {
		bufScale = 1
	}
	w, h = bufW/bufScale, bufH/bufScale
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
