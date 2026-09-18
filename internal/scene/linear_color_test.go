package scene

import "testing"

func TestLinearColorHierarchyMultipliesTintInLightSpace(t *testing.T) {
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	scene := NewScene()
	parent := scene.Add(0, Node{Color: Color{.5, .5, .5, .8}})
	scene.Add(parent, Node{Mesh: testTriangle(t), Color: Color{.5, .5, .5, .5}})
	for _, linear := range []bool{false, true, false} {
		canvas.SetLinearColor(linear)
		canvas.Reset(100, 100)
		scene.Draw(canvas, testCamera(), Viewport{Width: 100, Height: 100})
		frame := canvas.Frame()
		color := frame.Commands[0].Draws[0].Color
		if frame.LinearColor != linear {
			t.Fatal("frame lost color mode across reset")
		}
		want := float32(.25)
		if linear {
			want = .2369668
		}
		if abs(color[0]-want) > 1e-5 || color[1] != color[0] || color[2] != color[0] || abs(color[3]-.4) > 1e-6 {
			t.Fatalf("linear=%v inherited color=%v", linear, color)
		}
	}
}
