package scene

import (
	"math"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestRadialGradientInvalidArgumentsLeaveCanvasUnchanged(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Reset(80, 60)
	c.Rect(1, 2, 3, 4, Color{1, 1, 1, 1})
	c.Frame()
	vertices := append([]render.Vertex(nil), c.vertices...)
	commands := append([]render.Command(nil), c.commands...)
	pending := c.pendingUI
	check := func(values [11]float32) {
		t.Helper()
		c.RadialGradient(values[0], values[1], values[2], Color{values[3], values[4], values[5], values[6]}, Color{values[7], values[8], values[9], values[10]})
		if !reflect.DeepEqual(c.vertices, vertices) || !reflect.DeepEqual(c.commands, commands) || c.pendingUI != pending {
			t.Fatalf("invalid gradient changed existing canvas content: %v", values)
		}
	}
	valid := [11]float32{20, 30, 10, .2, .4, .6, 1, .8, .5, .3, .2}
	for channel := range valid {
		for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
			values := valid
			values[channel] = bad
			check(values)
		}
	}
	for _, radius := range []float32{0, -1, -math.MaxFloat32} {
		values := valid
		values[2] = radius
		check(values)
	}
	for _, center := range []float32{math.MaxFloat32, -math.MaxFloat32} {
		for coordinate := 0; coordinate < 2; coordinate++ {
			values := valid
			values[coordinate], values[2] = center, math.MaxFloat32
			check(values)
		}
	}
	values := valid
	values[6], values[10] = 0, 0
	check(values)
	values[6], values[10] = -1, -.1
	check(values)
}

func TestRadialGradientUsesBoundedFiniteFanAndExactSeam(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	inner, outer := Color{.1, .3, .7, .75}, Color{.9, .6, .2, 0}
	for _, radius := range []float32{math.SmallestNonzeroFloat32, 1e-20, .25, 1, 1024, math.MaxFloat32} {
		c.Reset(80, 60)
		c.RadialGradient(0, 0, radius, inner, outer)
		vertices := c.vertices
		if len(vertices) < 3*12 || len(vertices) > 3*512 || len(vertices)%3 != 0 {
			t.Fatalf("radius %g generated unbounded/incomplete triangles: %d vertices", radius, len(vertices))
		}
		for i, vertex := range vertices {
			for _, value := range [...]float32{vertex.X, vertex.Y, vertex.Z, vertex.U, vertex.V, vertex.R, vertex.G, vertex.B, vertex.A} {
				if !finite(value) {
					t.Fatalf("radius %g vertex %d has nonfinite attributes: %+v", radius, i, vertex)
				}
			}
			want := outer
			if i%3 == 0 {
				want = inner
				if vertex.X != 0 || vertex.Y != 0 {
					t.Fatal("fan center moved away from its requested position")
				}
			}
			if (Color{vertex.R, vertex.G, vertex.B, vertex.A}) != want || vertex.U != c.whiteU || vertex.V != c.whiteV {
				t.Fatal("gradient premultiplied its endpoint color or changed coverage sampling")
			}
			if i%3 == 1 && i > 1 && vertex != vertices[i-2] {
				t.Fatal("adjacent fan triangles did not share their perimeter vertex")
			}
		}
		if vertices[len(vertices)-1] != vertices[1] {
			t.Fatal("gradient seam was not closed exactly")
		}
		commands := c.Frame().Commands
		if len(commands) != 1 || commands[0].Kind != render.OverlayCommand || commands[0].Count != len(vertices) {
			t.Fatal("gradient needed a texture/scene command or lost its triangle range")
		}
	}
}

func TestRadialGradientPreservesPartlyOffscreenGeometry(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Reset(80, 60)
	c.RadialGradient(-5, 30, 20, Color{.2, .4, .8, 0}, Color{.2, .4, .8, .5})
	left, inside := false, false
	for _, vertex := range c.vertices {
		left = left || vertex.X < 0
		inside = inside || vertex.X > 0 && vertex.X < 80 && vertex.Y > 0 && vertex.Y < 60
	}
	if !left || !inside {
		t.Fatal("partial clipping rejected/clamped geometry instead of leaving it to the GPU")
	}
	if c.vertices[0].A != 0 || c.vertices[1].A != .5 {
		t.Fatal("transparent center with a visible perimeter was treated as a no-op")
	}
}
