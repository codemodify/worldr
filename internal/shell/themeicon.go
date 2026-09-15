package shell

import (
	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/icontheme"
)

// FillThemeIcons paints XDG theme PNGs onto actors that have no client
// xdg_toplevel_icon buffer. Client buffers always win.
func FillThemeIcons(actors []*engine.Actor, catalog []LaunchItem) {
	s := icontheme.Default()
	s.Want = 16
	for _, a := range actors {
		if a == nil || a.HasIcon() {
			continue
		}
		name := a.IconName
		if name == "" {
			name = LookupDesktopIcon(catalog, a.AppID)
		}
		if name == "" {
			continue
		}
		if pix, w, h, st, ok := icontheme.Lookup(name, s); ok {
			a.IconPix, a.IconW, a.IconH, a.IconStride = pix, w, h, st
		}
	}
}
