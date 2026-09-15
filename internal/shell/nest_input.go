package shell

import (
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/platform/linux/wlclient"
)

// applyNestedPointer replaces evdev pointer (and keys) with the host Wayland
// seat. Call after ptr.Poll() when p.wl != nil. Merging Click from both
// sources made panel apps toggle open then immediately shut.
func applyNestedPointer(ptr *input.Pointer, in wlclient.Input) {
	if ptr == nil {
		return
	}
	ptr.X, ptr.Y = in.X, in.Y
	ptr.Click = in.Click
	ptr.Release = in.Release
	ptr.Keys = ptr.Keys[:0]
	ptr.Quit = false
	for _, k := range in.Keys {
		ptr.Keys = append(ptr.Keys, input.Key{Code: k.Code, Pressed: k.Pressed})
	}
}
