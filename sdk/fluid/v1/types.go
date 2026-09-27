// Package fluid provides renderer-independent geometry and interaction for
// independently movable panels with smoothly joined rounded contours.
package fluid

import (
	"fmt"
	"math"
)

const (
	MaxSurfaces           = 16
	MaxCoordinate float32 = 1 << 20
	MaxDimension  float32 = 32768
	MaxBlend      float32 = 1024
	MaxTime       float32 = 1e6
)

// Point and Rect use the same framebuffer coordinate space as the native
// renderer. Rectangles need not begin at the origin or remain inside a Field.
type Point struct{ X, Y float32 }
type Rect struct{ X, Y, Width, Height float32 }

// Surface describes one rounded panel. Tint is straight (not premultiplied)
// RGBA, with each channel in [0,1]. Fuse controls visual joining, not movement.
type Surface struct {
	Bounds Rect
	Radius float32
	Tint   [4]float32
	Fuse   bool
}

// Style contains renderer-neutral material intent. Blend is a distance in
// framebuffer units; all other scalars and color channels are in [0,1]. Zero
// values are meaningful, including hard union (Blend=0) and invisible glass
// (Opacity=0). Use DefaultStyle explicitly when defaults are wanted.
type Style struct {
	Blend, Rim, Refraction, Frost, Glow, Opacity float32
	Background                                   [3][4]float32
}

// Field is a bounded drawing layer. Bounds supplies its viewport/clip; it is
// not an extra signed-distance shape. Surfaces may extend beyond the clip.
// The slice is borrowed until a synchronous renderer returns; retained or
// asynchronous consumers can use Clone. An empty field still has a backdrop.
type Field struct {
	Bounds        Rect
	Surfaces      []Surface
	Style         Style
	Time          float32
	Pointer       Point
	PointerActive bool
}

func DefaultStyle() Style {
	return Style{
		Blend: 28, Rim: .55, Refraction: .35, Frost: .25, Glow: .15, Opacity: .68,
		Background: [3][4]float32{{.04, .07, .12, 1}, {.19, .34, .46, 1}, {.34, .22, .42, 1}},
	}
}

func finite(values ...float32) bool {
	for _, v := range values {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return false
		}
	}
	return true
}
func unit(v float32) bool       { return finite(v) && v >= 0 && v <= 1 }
func coordinate(v float32) bool { return finite(v) && v >= -MaxCoordinate && v <= MaxCoordinate }

func (p Point) Validate() error {
	if !coordinate(p.X) || !coordinate(p.Y) {
		return fmt.Errorf("fluid: point is not finite or exceeds coordinate limits")
	}
	return nil
}
func (r Rect) Validate() error {
	if !coordinate(r.X) || !coordinate(r.Y) || !finite(r.Width, r.Height) || r.Width <= 0 || r.Height <= 0 || r.Width > MaxDimension || r.Height > MaxDimension {
		return fmt.Errorf("fluid: rectangle must have finite bounded coordinates and positive dimensions")
	}
	if !coordinate(r.X+r.Width) || !coordinate(r.Y+r.Height) {
		return fmt.Errorf("fluid: rectangle extent exceeds coordinate limits")
	}
	return nil
}
func (s Surface) Validate() error {
	if err := s.Bounds.Validate(); err != nil {
		return err
	}
	if !finite(s.Radius) || s.Radius < 0 || s.Radius > min(s.Bounds.Width, s.Bounds.Height)/2 {
		return fmt.Errorf("fluid: radius must be within half the shorter dimension")
	}
	for _, v := range s.Tint {
		if !unit(v) {
			return fmt.Errorf("fluid: tint channels must be within [0,1]")
		}
	}
	return nil
}
func (s Style) Validate() error {
	if !finite(s.Blend) || s.Blend < 0 || s.Blend > MaxBlend {
		return fmt.Errorf("fluid: blend must be within [0,%g] framebuffer units", MaxBlend)
	}
	for _, v := range []float32{s.Rim, s.Refraction, s.Frost, s.Glow, s.Opacity} {
		if !unit(v) {
			return fmt.Errorf("fluid: material values must be within [0,1]")
		}
	}
	for _, color := range s.Background {
		for _, v := range color {
			if !unit(v) {
				return fmt.Errorf("fluid: background channels must be within [0,1]")
			}
		}
	}
	return nil
}
func (f Field) Validate() error {
	if err := f.Bounds.Validate(); err != nil {
		return fmt.Errorf("fluid: field bounds: %w", err)
	}
	if len(f.Surfaces) > MaxSurfaces {
		return fmt.Errorf("fluid: field exceeds %d surfaces", MaxSurfaces)
	}
	if err := f.Style.Validate(); err != nil {
		return err
	}
	if !finite(f.Time) || f.Time < 0 || f.Time > MaxTime {
		return fmt.Errorf("fluid: time must be finite and within [0,%g] seconds", MaxTime)
	}
	if err := f.Pointer.Validate(); err != nil {
		return fmt.Errorf("fluid: pointer: %w", err)
	}
	for i, surface := range f.Surfaces {
		if err := surface.Validate(); err != nil {
			return fmt.Errorf("fluid: surface %d: %w", i, err)
		}
	}
	return nil
}
func (f Field) Clone() Field {
	f.Surfaces = append([]Surface(nil), f.Surfaces...)
	return f
}
