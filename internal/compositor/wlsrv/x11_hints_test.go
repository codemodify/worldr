package wlsrv

import (
	"testing"

	"github.com/codemodify/worldr/internal/engine"
)

func TestApplyX11HintsSetsTitleAndChrome(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{Title: "X11", AppID: "xwayland", Width: 80, Height: 40}
	scene.Add(a)
	got := ApplyX11Hints(scene, X11MapHints{
		Win: 0x21, Title: "xterm", AppID: "XTerm",
		NoChrome: false, W: 80, H: 40,
	})
	if got != a {
		t.Fatal("match")
	}
	if a.Title != "xterm" || a.AppID != "XTerm" || a.X11Win != 0x21 || a.NoChrome {
		t.Fatalf("%+v", a)
	}
}

func TestApplyX11HintsOverrideNoChromeAndPos(t *testing.T) {
	scene := engine.NewScene()
	a := &engine.Actor{X11Win: 9, Title: "menu", Width: 20, Height: 10}
	scene.Add(a)
	ApplyX11Hints(scene, X11MapHints{
		Win: 9, Title: "xcalc", AppID: "XCalc",
		NoChrome: true, X: 120, Y: 80,
	})
	if !a.NoChrome || a.X != 120 || a.Y != 80 || a.Title != "xcalc" {
		t.Fatalf("%+v", a)
	}
}

func TestApplyX11HintsLinksTransientOwner(t *testing.T) {
	scene := engine.NewScene()
	parent := &engine.Actor{X11Win: 1, Title: "xterm"}
	child := &engine.Actor{X11Win: 2, Title: "Find"}
	scene.Add(parent)
	scene.Add(child)
	ApplyX11Hints(scene, X11MapHints{Win: 2, TransientFor: 1, Title: "Find", NoChrome: true})
	if child.Owner != parent {
		t.Fatal("owner")
	}
}
