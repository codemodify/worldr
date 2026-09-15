package shell

import "github.com/codemodify/worldr/internal/input"

// Linux evdev codes. Nested Wayland seats usually send evdev+8 (XKB).
const (
	keyEsc       uint32 = 1
	keyTab       uint32 = 15
	keyQ         uint32 = 16
	keyEnter     uint32 = 28
	keyLeft      uint32 = 105
	keyRight     uint32 = 106
	keyF12       uint32 = 88
	keyF1        uint32 = 59
	keySpace     uint32 = 57
	keyUp        uint32 = 103
	keyDown      uint32 = 108
	keyLeftMeta  uint32 = 125
	keyRightMeta uint32 = 126
	keyLeftCtrl  uint32 = 29
	keyRightCtrl uint32 = 97
	keyLeftAlt   uint32 = 56
	keyRightAlt  uint32 = 100
)

func isCtrl(code uint32) bool {
	return isEvdev(code, keyLeftCtrl) || isEvdev(code, keyRightCtrl)
}

func isAlt(code uint32) bool {
	return isEvdev(code, keyLeftAlt) || isEvdev(code, keyRightAlt)
}

func isEvdev(code, evdev uint32) bool {
	return code == evdev || code == evdev+8
}

func isMeta(code uint32) bool {
	return isEvdev(code, keyLeftMeta) || isEvdev(code, keyRightMeta)
}

func isOverviewToggle(code uint32, metaHeld bool) bool {
	if isEvdev(code, keyF12) {
		return true
	}
	return metaHeld && isEvdev(code, keyTab)
}

// isQuitChord is Ctrl+Q (evdev or evdev+8). Bare Q never quits.
func isQuitChord(code uint32, ctrlHeld bool) bool {
	return ctrlHeld && isEvdev(code, keyQ)
}

// handleQuitKeys: Ctrl+Q always quits. Bare Esc quits only on an empty
// desktop (launcher/overview closed, no mapped client). Bare Q never quits
// so typing in foot cannot kill the compositor.
func handleQuitKeys(ptr *input.Pointer, ctrlHeld, overlayOpen, desktopHasClient bool) (quit bool, consumed map[uint32]bool) {
	consumed = map[uint32]bool{}
	if ptr == nil {
		return false, consumed
	}
	ptr.Quit = false
	for _, k := range ptr.Keys {
		if !k.Pressed {
			continue
		}
		if isQuitChord(k.Code, ctrlHeld) {
			consumed[k.Code] = true
			ptr.Quit = true
			return true, consumed
		}
		if isEvdev(k.Code, keyEsc) && !overlayOpen && !desktopHasClient {
			consumed[k.Code] = true
			ptr.Quit = true
			return true, consumed
		}
	}
	return false, consumed
}
