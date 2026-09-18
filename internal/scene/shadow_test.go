package scene

import (
	"math"
	"testing"
)

func TestDirectionalShadowProjectionAndSceneFlags(t *testing.T) {
	center, light := Vec3{3, 2, -1}, Vec3{.5, 1, 1}
	shadow, err := DirectionalShadow(center, light, 8, 12)
	if err != nil {
		t.Fatal(err)
	}
	matrix := Mat4(shadow.Projection)
	if got := matrix.TransformPoint(center); got.Sub(Vec3{Z: .5}).Length() > 1e-5 {
		t.Fatalf("light volume center=%v", got)
	}
	if got := matrix.TransformPoint(center.Add(light.Normalize().Mul(6))); got.Length() > 1e-5 {
		t.Fatalf("light near center=%v", got)
	}
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	scene := NewScene()
	scene.Light = light
	scene.Shadow = shadow
	scene.Add(0, Node{Mesh: testTriangle(t), CastShadow: true, ReceiveShadow: true})
	scene.Draw(canvas, testCamera(), Viewport{Width: 100, Height: 100})
	command := canvas.Frame().Commands[0]
	if command.View.Shadow != shadow || command.View.Light != ([3]float32{light.X, light.Y, light.Z}) || !command.Draws[0].CastShadow || !command.Draws[0].ReceiveShadow {
		t.Fatal("scene lost authored lighting and instance shadow flags")
	}
}

func TestDirectionalShadowRejectsInvalidVolumes(t *testing.T) {
	for _, extent := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		if _, err := DirectionalShadow(Vec3{}, Vec3{Y: 1}, extent, 10); err == nil {
			t.Fatalf("accepted span %v", extent)
		}
	}
	if _, err := DirectionalShadow(Vec3{}, Vec3{}, 10, 10); err == nil {
		t.Fatal("accepted zero light direction")
	}
	if _, err := DirectionalShadow(Vec3{}, Vec3{Y: 1}, 10, 10); err != nil {
		t.Fatal("vertical light should have a stable alternative up vector", err)
	}
}
