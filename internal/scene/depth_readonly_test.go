package scene

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestDepthReadOnlyDrawsFollowOpaqueContentInStableTraversalOrder(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := NewScene()
	mesh, texture := testTriangle(t), testTexture(t)
	first := s.Add(0, Node{Mesh: mesh, Transform: Translate(10, 0, 0), DepthReadOnly: true})
	s.Add(first, Node{Mesh: mesh, Transform: Translate(1, 0, 0)}) // The flag is not inherited.
	s.Add(0, Node{Surface: texture, Transform: Translate(20, 0, 0)})
	s.Add(0, Node{Mesh: mesh, Transform: Translate(30, 0, 0), Color: Color{1, 1, 1, .5}})
	s.Add(0, Node{Mesh: mesh, Transform: Translate(40, 0, 0), DepthReadOnly: true})
	s.Add(0, Node{Surface: texture, Transform: Translate(50, 0, 0)})
	s.Add(0, Node{Mesh: mesh, Transform: Translate(60, 0, 0), DepthReadOnly: true, Hidden: true})
	for frame := 0; frame < 2; frame++ {
		c.Reset(200, 200)
		s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
		commands := c.Frame().Commands
		if len(commands) != 1 || commands[0].Kind != render.SceneCommand {
			t.Fatal("deferred meshes created an additional depth pass")
		}
		draws := commands[0].Draws
		want := []float32{11, 20, 30, 50, 10, 40}
		if len(draws) != len(want) {
			t.Fatalf("draw count=%d, want %d", len(draws), len(want))
		}
		for i, draw := range draws {
			if draw.Model[12] != want[i] || draw.DepthReadOnly != (i >= 4) {
				t.Fatalf("draw %d lost stable order/instance flag: x=%v readonly=%v", i, draw.Model[12], draw.DepthReadOnly)
			}
			if i == 1 || i == 3 {
				if draw.Texture != texture || draw.Geometry != nil {
					t.Fatal("opaque surface lost its content resource")
				}
			} else if draw.Geometry != mesh.Geometry() || draw.Texture != nil {
				t.Fatal("ordering recreated or replaced retained mesh geometry")
			}
		}
		if draws[2].Color[3] != .5 || draws[2].DepthReadOnly {
			t.Fatal("default draw behavior was inferred from color alpha")
		}
	}
}

func TestDepthReadOnlyEditsStayInsideTheirCameraCommand(t *testing.T) {
	c, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := NewScene()
	mesh := testTriangle(t)
	first := s.Add(0, Node{Mesh: mesh, Transform: Translate(1, 0, 0), DepthReadOnly: true})
	second := s.Add(0, Node{Mesh: mesh, Transform: Translate(2, 0, 0)})
	c.Rect(0, 0, 200, 200, Color{0, 0, 0, 1})
	s.Draw(c, testCamera(), Viewport{Width: 100, Height: 200})
	c.Rect(5, 5, 10, 10, Color{1, 1, 1, 1})
	s.Node(first).DepthReadOnly = false
	s.Node(second).DepthReadOnly = true
	other := testCamera()
	other.Eye.X = 2
	s.Draw(c, other, Viewport{X: 100, Width: 100, Height: 200})
	commands := c.Frame().Commands
	if len(commands) != 4 || commands[0].Kind != render.OverlayCommand || commands[1].Kind != render.SceneCommand || commands[2].Kind != render.OverlayCommand || commands[3].Kind != render.SceneCommand {
		t.Fatal("deferred meshes moved across an overlay/camera command boundary")
	}
	left, right := commands[1], commands[3]
	if left.View.Viewport[0] != 0 || right.View.Viewport[0] != 100 || left.View.Projection == right.View.Projection {
		t.Fatal("camera commands lost their own viewport/projection")
	}
	if len(left.Draws) != 2 || len(right.Draws) != 2 || left.Draws[0].Model[12] != 2 || left.Draws[1].Model[12] != 1 || right.Draws[0].Model[12] != 1 || right.Draws[1].Model[12] != 2 {
		t.Fatal("editing a later pass changed prior draw order or lost the new order")
	}
	for _, command := range []render.Command{left, right} {
		if command.Draws[0].DepthReadOnly || !command.Draws[1].DepthReadOnly {
			t.Fatal("depth-write flag edits did not remain per recorded instance")
		}
		for _, draw := range command.Draws {
			if draw.Geometry != mesh.Geometry() {
				t.Fatal("changing depth policy recreated retained geometry")
			}
		}
	}
	// A pass containing only deferred draws still exists, and hiding its last
	// drawable does not leave a stale retained instance on the following frame.
	s.Node(first).Hidden = true
	c.Reset(200, 200)
	s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
	if commands := c.Frame().Commands; len(commands) != 1 || len(commands[0].Draws) != 1 || !commands[0].Draws[0].DepthReadOnly {
		t.Fatal("all-deferred pass disappeared")
	}
	s.Node(second).Hidden = true
	c.Reset(200, 200)
	s.Draw(c, testCamera(), Viewport{Width: 200, Height: 200})
	if len(c.Frame().Commands) != 0 {
		t.Fatal("hidden deferred geometry survived in a later frame")
	}
}
