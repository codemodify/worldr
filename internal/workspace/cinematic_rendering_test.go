package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/render"
)

func framePointLights(frame render.Frame) []render.PointLight {
	for _, command := range frame.Commands {
		if command.Kind == render.SceneCommand && command.View.PointLightCount > 0 {
			return command.View.PointLights[:command.View.PointLightCount]
		}
	}
	return nil
}

func TestCinematicFillLightsRemainActiveWithoutGradingCompositorPixels(t *testing.T) {
	w := study(t)
	frame := w.Draw(1440, 900)
	initial := framePointLights(frame)
	if frame.Output != (render.OutputTransform{}) || len(initial) != 2 {
		t.Fatalf("cinematic presentation graded compositor pixels or omitted fill lights: output=%+v lights=%+v", frame.Output, initial)
	}
	w.Update(time.Second)
	moved := framePointLights(w.Draw(1440, 900))
	if len(moved) != 2 || moved[0].Position == initial[0].Position {
		t.Fatal("cinematic fill light did not move with the live study")
	}
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	focused := w.Draw(1440, 900)
	if focused.Output != (render.OutputTransform{}) || len(framePointLights(focused)) != 2 {
		t.Fatalf("focused work lost the permanent cinematic lighting: output=%+v lights=%v", focused.Output, framePointLights(focused))
	}
}
