package workspace

import (
	"fmt"
	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
	"image"
	"math"
)

func (w *Workspace) TextInput() experience.TextInputState {
	if p := w.commands; p != nil && p.open {
		r := image.Rect(int(w.ox+(commandBounds.x+22)*w.scale), int(w.oy+(commandBounds.y+57)*w.scale), int(w.ox+(commandBounds.x+698)*w.scale), int(w.oy+(commandBounds.y+99)*w.scale))
		return p.field.TextInput(fmt.Sprintf("workspace:commands:%d", p.epoch), r)
	}
	if !w.applicationKeyboard || w.applicationFocusedID == 0 || w.m.applicationState.Overview || w.m.applicationState.Placing {
		return experience.TextInputState{}
	}
	provider, ok := w.applications.(experience.ApplicationTextInput)
	if !ok {
		return experience.TextInputState{}
	}
	state := provider.TextInput(w.applicationFocusedID)
	if !state.Enabled {
		return experience.TextInputState{}
	}
	surface := experience.ApplicationSurface{}
	for _, candidate := range w.applicationSurfaces {
		if candidate.ID == w.applicationFocusedID {
			surface = candidate
			break
		}
	}
	node := w.scene.Node(w.applicationNodes[w.applicationFocusedID])
	if node == nil || node.Hidden || surface.Texture == nil {
		return experience.TextInputState{}
	}
	width, height := surface.Texture.Size()
	minX, minY, maxX, maxY := float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, corner := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x, y := state.CursorRect[0]+corner[0]*state.CursorRect[2], state.CursorRect[1]+corner[1]*state.CursorRect[3]
		local := scene.Vec3{X: float32(x)/float32(width) - .5, Y: .5 - float32(y)/float32(height)}
		px, py, _, visible := w.camera.Project(node.Transform.TransformPoint(local), w.viewport)
		if !visible {
			return experience.TextInputState{}
		}
		minX, minY, maxX, maxY = min(minX, px), min(minY, py), max(maxX, px), max(maxY, py)
	}
	state.CursorRect = [4]int{int(minX), int(minY), max(1, int(maxX-minX)), max(1, int(maxY-minY))}
	return state
}
