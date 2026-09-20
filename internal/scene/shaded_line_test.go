package scene

import (
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestShadedLineColorsFollowOrientedNormalAtBothEndpoints(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	negative := Color{R: .1, G: .2, B: .3, A: .8}
	positive := Color{R: .7, G: .6, B: .5, A: .4}
	check := func(x1, y1, x2, y2 float32, want map[[2]float32]Color) {
		t.Helper()
		c.Reset(100, 100)
		c.ShadedLine(x1, y1, x2, y2, 3, negative, positive)
		if len(c.vertices) != 18 {
			t.Fatalf("shaded line generated %d vertices, want 18", len(c.vertices))
		}
		seen := make(map[[2]float32]bool, len(want))
		for _, vertex := range c.vertices {
			key := [2]float32{vertex.X, vertex.Y}
			color, ok := want[key]
			if !ok {
				t.Fatalf("unexpected shaded-line vertex at (%v, %v)", vertex.X, vertex.Y)
			}
			got := Color{vertex.R, vertex.G, vertex.B, vertex.A}
			if got != color {
				t.Fatalf("vertex at (%v, %v) color = %+v, want %+v", vertex.X, vertex.Y, got, color)
			}
			seen[key] = true
		}
		if len(seen) != len(want) {
			t.Fatalf("shaded line covered %d distinct endpoint samples, want %d", len(seen), len(want))
		}
	}
	transparentNegative := negative.WithAlpha(0)
	transparentPositive := positive.WithAlpha(0)
	check(10, 20, 30, 20, map[[2]float32]Color{
		{10, 18}: transparentNegative, {30, 18}: transparentNegative,
		{10, 19}: negative, {30, 19}: negative,
		{10, 21}: positive, {30, 21}: positive,
		{10, 22}: transparentPositive, {30, 22}: transparentPositive,
	})

	// Reversing the endpoints reverses the oriented normal, so each color moves
	// to the opposite screen-space side while remaining on its semantic side.
	check(30, 20, 10, 20, map[[2]float32]Color{
		{10, 22}: transparentNegative, {30, 22}: transparentNegative,
		{10, 21}: negative, {30, 21}: negative,
		{10, 19}: positive, {30, 19}: positive,
		{10, 18}: transparentPositive, {30, 18}: transparentPositive,
	})
}

func TestShadedLineGeometryIsBoundedAndVisibilityMatchesCanvasShapes(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	visible := Color{R: .2, G: .5, B: .8, A: .7}
	transparent := visible.WithAlpha(0)
	for _, width := range []float32{.1, 1, 12, 100000} {
		c.Reset(100, 100)
		c.ShadedLine(4, 7, 73, 81, width, transparent, visible)
		count := len(c.vertices)
		if count == 0 || count > 18 || count%3 != 0 {
			t.Fatalf("width %v generated invalid or unbounded geometry: %d vertices", width, count)
		}
		frame := c.Frame()
		if len(frame.Commands) != 1 || frame.Commands[0].Kind != render.OverlayCommand || frame.Commands[0].First != 0 || frame.Commands[0].Count != count {
			t.Fatalf("width %v produced unexpected draw commands: %+v", width, frame.Commands)
		}
	}

	for name, draw := range map[string]func(){
		"zero width":       func() { c.ShadedLine(1, 2, 4, 6, 0, visible, visible) },
		"negative width":   func() { c.ShadedLine(1, 2, 4, 6, -2, visible, visible) },
		"invisible colors": func() { c.ShadedLine(1, 2, 4, 6, 2, transparent, transparent) },
		"degenerate":       func() { c.ShadedLine(3, 3, 3, 3, 2, visible, visible) },
	} {
		t.Run(name, func(t *testing.T) {
			c.Reset(100, 100)
			draw()
			if len(c.vertices) != 0 || len(c.Frame().Commands) != 0 {
				t.Fatal("invalid or invisible shaded line changed the canvas")
			}
		})
	}
}

func TestLineMatchesUniformShadedLine(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	color := Color{R: .2, G: .4, B: .9, A: .6}

	c.Line(5, 8, 31, 47, .65, color)
	want := append([]render.Vertex(nil), c.vertices...)
	c.Reset(100, 100)
	c.ShadedLine(5, 8, 31, 47, .65, color, color)
	if !reflect.DeepEqual(c.vertices, want) {
		t.Fatal("uniform ShadedLine does not preserve Line geometry and color")
	}
}
