package shell

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func handleWorkspaceKeys(scene *engine.Scene, ptr *input.Pointer, now time.Time, ctrlHeld, altHeld *bool) map[uint32]bool {
	consumed := map[uint32]bool{}
	if scene == nil || ptr == nil {
		return consumed
	}
	for _, k := range ptr.Keys {
		if isCtrl(k.Code) {
			if ctrlHeld != nil {
				*ctrlHeld = k.Pressed
			}
			continue
		}
		if isAlt(k.Code) {
			if altHeld != nil {
				*altHeld = k.Pressed
			}
			continue
		}
		if !k.Pressed {
			continue
		}
		if ctrlHeld == nil || altHeld == nil || !*ctrlHeld || !*altHeld {
			continue
		}
		if isEvdev(k.Code, keyRight) {
			scene.StepWorkspace(1, now)
			consumed[k.Code] = true
			ptr.Quit = false
		}
		if isEvdev(k.Code, keyLeft) {
			scene.StepWorkspace(-1, now)
			consumed[k.Code] = true
			ptr.Quit = false
		}
	}
	return consumed
}

func desktopActors(scene *engine.Scene) []*engine.Actor {
	if scene == nil {
		return nil
	}
	return scene.ActorsOn(scene.ActiveWorkspace())
}
