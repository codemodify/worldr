package scene

import (
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func TestCanvasRetainsDisplayReferredOutputTransformAcrossReset(t *testing.T) {
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	output := render.OutputTransform{Exposure: .25, Saturation: .1, Contrast: .08, ToneMap: render.ToneMapFilmic, BloomStrength: .45, BloomThreshold: .8, BloomRadius: 5}
	canvas.SetLinearColor(true)
	canvas.SetOutputTransform(output)
	canvas.Reset(320, 200)
	frame := canvas.Frame()
	if !frame.LinearColor || frame.Output != output {
		t.Fatalf("canvas lost output transform across reset: %+v", frame)
	}
}

func TestSceneCopiesPointLightsAndKeepsGlassOpticsPerInstance(t *testing.T) {
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	defer canvas.Close()
	world := NewScene()
	mesh := testTriangle(t)
	glass := render.Material{Specular: .9, Roughness: .16, RimStrength: .5, RimColor: [3]float32{.3, .9, 1}, Transmission: .88, Refraction: .7, RefractionBlur: .15}
	world.Add(0, Node{Mesh: mesh, Translucent: true, Color: ColorHex(0x58d9f4, .3), Material: glass})
	world.Add(0, Node{Mesh: mesh, Transform: Translate(1, 0, 0)})
	world.PointLights = []render.PointLight{{Position: [3]float32{-1, 2, 3}, Color: [3]float32{.2, .8, 1}, Intensity: 2, Radius: 7}}
	world.Draw(canvas, testCamera(), Viewport{Width: 320, Height: 200})
	command := canvas.Frame().Commands[0]
	if command.View.PointLightCount != 1 || command.View.PointLights[0] != world.PointLights[0] {
		t.Fatal("scene lost view-local point light")
	}
	if command.Draws[0].Material != glass || command.Draws[1].Material != (render.Material{}) {
		t.Fatalf("glass optics inherited or crossed instances: %+v", command.Draws)
	}
	world.PointLights[0].Intensity = 7
	if command.View.PointLights[0].Intensity != 2 {
		t.Fatal("completed frame aliased mutable scene point lights")
	}
}
