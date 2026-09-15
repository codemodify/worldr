package shell

import (
	"time"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/input"
)

func handleWorkspaceKeys(scene *engine.Scene, ptr *input.Pointer, now time.Time, ctrlHeld, altHeld, shiftHeld *bool) map[uint32]bool {
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
		if isShift(k.Code) {
			if shiftHeld != nil {
				*shiftHeld = k.Pressed
			}
			continue
		}
		if !k.Pressed {
			continue
		}
		if ctrlHeld == nil || altHeld == nil || !*ctrlHeld || !*altHeld {
			continue
		}
		move := shiftHeld != nil && *shiftHeld
		if isEvdev(k.Code, keyRight) {
			if move {
				scene.MoveFocused(1, now)
			} else {
				scene.StepWorkspace(1, now)
			}
			consumed[k.Code] = true
			ptr.Quit = false
		}
		if isEvdev(k.Code, keyLeft) {
			if move {
				scene.MoveFocused(-1, now)
			} else {
				scene.StepWorkspace(-1, now)
			}
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

// desktopHasClient is true if the active desktop has a mapped actor.
// Stricter than "focused only": bare Esc must not quit while any client
// is on this desktop (focus can lag a frame after spawn).
func desktopHasClient(scene *engine.Scene) bool {
	if scene == nil {
		return false
	}
	return scene.HasVisibleOn(scene.ActiveWorkspace())
}
