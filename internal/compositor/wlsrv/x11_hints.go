package wlsrv

import "github.com/codemodify/worldr/internal/engine"

// X11MapHints is the compositor-side view of an X11 window (from the XWM).
type X11MapHints struct {
	Win, TransientFor uint32
	Title, AppID      string
	NoChrome          bool
	X, Y, W, H        int
}

func applyX11Hints(a *engine.Actor, h X11MapHints, scene *engine.Scene) {
	if a == nil {
		return
	}
	if h.Title != "" {
		a.Title = h.Title
	}
	if h.AppID != "" {
		a.AppID = h.AppID
	}
	a.X11Win = h.Win
	if h.NoChrome {
		a.NoChrome = true
		a.X, a.Y = h.X, h.Y
	}
	if h.TransientFor != 0 && a.Owner == nil && scene != nil {
		a.Owner = findX11Actor(scene, h.TransientFor)
	}
}

func findX11Actor(scene *engine.Scene, win uint32) *engine.Actor {
	if scene == nil || win == 0 {
		return nil
	}
	for _, a := range scene.Actors() {
		if a != nil && a.X11Win == win {
			return a
		}
	}
	return nil
}

// ApplyX11Hints updates a live actor when the XWM learns title/class/chrome.
// Matches X11Win first, then an unmatched xwayland actor of the same size.
func ApplyX11Hints(scene *engine.Scene, h X11MapHints) *engine.Actor {
	if scene == nil || h.Win == 0 {
		return nil
	}
	if a := findX11Actor(scene, h.Win); a != nil {
		applyX11Hints(a, h, scene)
		return a
	}
	for _, a := range scene.Actors() {
		if a == nil || a.X11Win != 0 {
			continue
		}
		if a.AppID != "xwayland" && a.Title != "X11" {
			continue
		}
		if h.W > 0 && h.H > 0 && (a.Width != h.W || a.Height != h.H) {
			continue
		}
		applyX11Hints(a, h, scene)
		return a
	}
	return nil
}
