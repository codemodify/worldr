package workspace

import (
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/presentation"
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

func TestCinematicFillLightsFollowPresentationWithoutGradingCompositorPixels(t *testing.T) {
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
	command(t, w, Action{Kind: SetReducedMotion, Enabled: true})
	fixed := framePointLights(w.Draw(1440, 900))
	w.Update(time.Second)
	if next := framePointLights(w.Draw(1440, 900)); len(next) != 2 || next[0].Position != fixed[0].Position || next[1].Position != fixed[1].Position {
		t.Fatal("reduced motion left cinematic fill lights moving")
	}
	command(t, w, Action{Kind: SetPresentation, Presentation: presentation.Adaptive})
	command(t, w, Action{Kind: SetFocused, Enabled: true})
	quiet := w.Draw(1440, 900)
	if quiet.Output != (render.OutputTransform{}) || len(framePointLights(quiet)) != 0 {
		t.Fatalf("focused Adaptive frame retained cinematic finish: output=%+v lights=%v", quiet.Output, framePointLights(quiet))
	}
}
