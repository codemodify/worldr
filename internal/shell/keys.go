package shell

// Linux evdev codes. Nested Wayland seats usually send evdev+8 (XKB).
const (
	keyEsc       uint32 = 1
	keyTab       uint32 = 15
	keyQ         uint32 = 16
	keyEnter     uint32 = 28
	keyLeft      uint32 = 105
	keyRight     uint32 = 106
	keyF12       uint32 = 88
	keyLeftMeta  uint32 = 125
	keyRightMeta uint32 = 126
)

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

func isQuit(code uint32) bool {
	return isEvdev(code, keyEsc) || isEvdev(code, keyQ)
}
