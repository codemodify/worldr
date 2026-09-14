package shell

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseColor parses #RRGGBB or #RRGGBBAA into linear-ish 0..1 RGBA (sRGB bytes / 255).
func ParseColor(s string) ([4]float32, error) {
	var z [4]float32
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return z, fmt.Errorf("color %q: want #RRGGBB", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return z, fmt.Errorf("color %q: %w", s, err)
	}
	if len(s) == 6 {
		z[0] = float32((n>>16)&0xff) / 255
		z[1] = float32((n>>8)&0xff) / 255
		z[2] = float32(n&0xff) / 255
		z[3] = 1
		return z, nil
	}
	z[0] = float32((n>>24)&0xff) / 255
	z[1] = float32((n>>16)&0xff) / 255
	z[2] = float32((n>>8)&0xff) / 255
	z[3] = float32(n&0xff) / 255
	return z, nil
}

// PackBGRA packs a 0..1 RGBA color as a little-endian XRGB8888 / BGRA8 pixel.
func PackBGRA(c [4]float32) uint32 {
	b := uint32(clamp01(c[2])*255 + 0.5)
	g := uint32(clamp01(c[1])*255 + 0.5)
	r := uint32(clamp01(c[0])*255 + 0.5)
	a := uint32(clamp01(c[3])*255 + 0.5)
	return a<<24 | r<<16 | g<<8 | b
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
