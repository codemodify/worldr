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

// filterNestedButtons drops a host pointer release that was not preceded
// by a press while the nested window had the host seat. Plasma can emit a
// release when the pointer enters worldr after a click elsewhere; forwarding
// that produced foot's "stray button release event (compositor bug?)".
func filterNestedButtons(ptr *input.Pointer, down *bool) {
	if ptr == nil || down == nil {
		return
	}
	if ptr.Click {
		*down = true
	}
	if ptr.Release && !*down {
		ptr.Release = false
	}
	if ptr.Release {
		*down = false
	}
}

// clientButtonGate is the shell→wlsrv press/release pair. Panel / SSD /
// launcher clicks never arm it, so a later host release is not forwarded.
type clientButtonGate struct {
	armed bool
}

func (g *clientButtonGate) onClientPress() {
	if g != nil {
		g.armed = true
	}
}

func (g *clientButtonGate) onRelease() bool {
	if g == nil || !g.armed {
		return false
	}
	g.armed = false
	return true
}
