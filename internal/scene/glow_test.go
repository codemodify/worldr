package scene

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestGlowRemainsAuthoredPerInstanceWithoutInheritingIntoContent(t *testing.T) {
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	s := NewScene()
	mesh, texture := testTriangle(t), testTexture(t)
	firstGlow, secondGlow := [3]float32{.1, .6, .9}, [3]float32{.7, .2, .1}
	parent := s.Add(0, Node{Mesh: mesh, Transform: Translate(1, 0, 0), Glow: firstGlow})
	plain := s.Add(parent, Node{Mesh: mesh, Transform: Translate(1, 0, 0)})
	s.Add(parent, Node{Mesh: mesh, Transform: Translate(2, 0, 0), Glow: secondGlow})
	s.Add(parent, Node{Surface: texture, Transform: Translate(3, 0, 0)})
	revision := texture.Revision()
	s.Draw(canvas, testCamera(), Viewport{Width: 200, Height: 200})
	// Record another view after editing authoring on shared geometry. Earlier
	// camera commands must keep their original per-instance emission values.
	s.Node(parent).Glow = [3]float32{}
	s.Node(plain).Glow = secondGlow
	s.Draw(canvas, testCamera(), Viewport{X: 200, Width: 200, Height: 200})
	commands := canvas.Frame().Commands
	if len(commands) != 2 {
		t.Fatalf("expected two independent scene commands, got %d", len(commands))
	}
	want := [2][4][3]float32{{firstGlow, {}, secondGlow, {}}, {{}, secondGlow, secondGlow, {}}}
	for pass, command := range commands {
		if command.Kind != render.SceneCommand || len(command.Draws) != 4 {
			t.Fatal("glow changed scene instance recording")
		}
		for i, draw := range command.Draws {
			if draw.Glow != want[pass][i] || draw.Model[12] != float32(i+1) {
				t.Fatalf("pass %d instance %d inherited glow or lost its transform: %+v", pass, i, draw)
			}
			if i == 3 {
				if draw.Texture != texture || draw.Geometry != nil || draw.Color != [4]float32{1, 1, 1, 1} {
					t.Fatal("glowing ancestor changed child application content")
				}
			} else if draw.Geometry != mesh.Geometry() || draw.Texture != nil {
				t.Fatal("glow edits recreated or replaced retained mesh geometry")
			}
		}
	}
	if texture.Revision() != revision {
		t.Fatal("mesh glow changed the content texture")
	}
}
