package workspace

import (
	"fmt"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func cornerToolsDesktop(t *testing.T) (*Workspace, *dockApplications) {
	t.Helper()
	w, apps := desktopDock(t, "files")
	selected := testWindowSkin(t, "advanced")
	selected.ID = "custom.compact-tools"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	return w, apps
}

func TestCornerToolsUsePhysicalViewportAndKeepClientCenterClear(t *testing.T) {
	w, _ := cornerToolsDesktop(t)
	selected := w.CurrentSkin()
	selected.Palette["surface"] = "#123456"
	if err := w.SetSkin(*selected); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{1440, 900}, {2048, 900}, {900, 1440}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w.Draw(size[0], size[1])
			v, s := w.viewport, w.scale
			if abs(v.X-24*s) > .01 || abs(v.Y-24*s) > .01 || abs(v.X+v.Width-(float32(size[0])-24*s)) > .01 || abs(v.Y+v.Height-(float32(size[1])-52*s)) > .01 {
				t.Fatalf("viewport does not use the physical display with compact insets: %+v", v)
			}
			if !w.skinDesktopChromeVisible() || w.orbitPadVisible() {
				t.Fatal("compact chrome did not replace the full pad")
			}
			for i, e := range w.skinDesktopEntries() {
				x, y := skinChromePoint(w, i)
				left, top, right, bottom := w.ox+e.bounds.x*s, w.oy+e.bounds.y*s, w.ox+(e.bounds.x+e.bounds.w)*s, w.oy+(e.bounds.y+e.bounds.h)*s
				if left < 0 || top < v.Y+v.Height || right > float32(size[0]) || bottom > float32(size[1]) {
					t.Fatal("corner button clipped or entered the scene viewport")
				}
				if got := w.applicationDockIndexAt((x-w.ox)/s, (y-w.oy)/s); got != skinCornerDockIndex+i {
					t.Fatalf("scaled corner target %d resolves as %d", i, got)
				}
				if !pointer(w, experience.PointerMove, x, y) || w.applicationDockHover != skinCornerDockIndex+i {
					t.Fatal("corner target lost scaled hover")
				}
			}
			centerX, centerY := v.X+v.Width/2, v.Y+v.Height/2
			if w.handleApplicationDock(experience.Event{Kind: experience.PointerMove, X: centerX, Y: centerY}) {
				t.Fatal("corner chrome occludes the client center")
			}
			w.canvas.Reset(size[0], size[1])
			if !w.drawSkinDesktopChrome() {
				t.Fatal("custom-ID corner chrome was not drawn")
			}
			found := false
			for _, vertex := range w.canvas.Frame().Vertices {
				if vertex.X < float32(size[0])-335*s || vertex.Y < v.Y+v.Height || !finite(float64(vertex.X)) || !finite(float64(vertex.Y)) {
					t.Fatal("unhovered corner decoration left its small reserved area")
				}
				if abs(vertex.R-float32(0x12)/255) < .001 && abs(vertex.G-float32(0x34)/255) < .001 && abs(vertex.B-float32(0x56)/255) < .001 {
					found = true
				}
			}
			if !found {
				t.Fatal("corner controls ignored the custom palette")
			}
		})
	}
}

func TestCornerToolsCompleteClicksRunTheirNamedActions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		index  int
		active func(*Workspace) bool
	}{
		{"Tools", 0, func(w *Workspace) bool { return w.commands != nil && w.commands.open }},
		{"Windows", 1, func(w *Workspace) bool { return w.m.applicationState.Overview }},
		{"Skins", 2, func(w *Workspace) bool { return w.settingsOpen && w.settingsCategory == settingsSkins }},
		{"Help", 3, func(w *Workspace) bool { return w.helpOpen }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, apps := cornerToolsDesktop(t)
			w.Draw(900, 1440)
			x, y := skinChromePoint(w, tc.index)
			if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureApplicationDock || w.pointer.dockIndex != skinCornerDockIndex+tc.index || tc.active(w) {
				t.Fatal("corner action activated before a completed click")
			}
			if !pointer(w, experience.PointerUp, x, y) || !tc.active(w) || w.pointer.kind != captureNone || len(apps.launched) != 0 {
				t.Fatal("corner action did not route to its existing workspace action")
			}
		})
	}
}

func TestCornerToolsRejectCancelledResizedAndChangedChromeClicks(t *testing.T) {
	for _, mode := range []string{"cancel", "keyboard-cancel", "drag-away-back", "release-outside", "resize", "corner-to-slate", "corner-to-default", "slate-to-corner"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := cornerToolsDesktop(t)
			if mode == "slate-to-corner" {
				if err := w.SetSkin(testWindowSkin(t, "merrick")); err != nil {
					t.Fatal(err)
				}
				w.Draw(1440, 900)
			}
			x, y := skinChromePoint(w, 0)
			pointer(w, experience.PointerDown, x, y)
			switch mode {
			case "cancel":
				w.Handle(experience.Event{Kind: experience.PointerCancel})
			case "keyboard-cancel":
				w.Handle(experience.Event{Kind: experience.KeyboardCancel})
			case "drag-away-back":
				pointer(w, experience.PointerMove, x-40, y)
				pointer(w, experience.PointerMove, x, y)
			case "release-outside":
				x -= 100
			case "resize":
				w.Draw(2048, 900)
				x, y = skinChromePoint(w, 0)
			case "corner-to-slate", "corner-to-default", "slate-to-corner":
				id := "merrick"
				if mode == "corner-to-default" {
					id = "plasma"
				}
				if mode == "slate-to-corner" {
					id = "advanced"
				}
				if err := w.SetSkin(testWindowSkin(t, id)); err != nil {
					t.Fatal(err)
				}
				w.Draw(1440, 900)
				if mode == "corner-to-default" {
					x, y = dockPoint(w, 0)
				} else {
					x, y = skinChromePoint(w, 0)
				}
			}
			pointer(w, experience.PointerUp, x, y)
			if w.commands != nil && w.commands.open || w.pointer.kind != captureNone {
				t.Fatal("cancelled or stale corner click activated Tools")
			}
		})
	}
}

func TestCornerToolsPreserveAnExistingClientPointerGrab(t *testing.T) {
	w, apps := cornerToolsDesktop(t)
	if _, err := apps.LaunchApplication("files"); err != nil {
		t.Fatal(err)
	}
	w.syncApplications()
	w.Draw(1440, 900)
	x, y := visibleApplicationPoint(t, w)
	pointer(w, experience.PointerDown, x, y)
	if len(w.applicationButtons) == 0 {
		t.Fatal("fixture did not establish a client grab")
	}
	x, y = skinChromePoint(w, 0)
	pointer(w, experience.PointerMove, x, y)
	pointer(w, experience.PointerUp, x, y)
	if len(w.applicationButtons) != 0 || w.commands != nil && w.commands.open || w.pointer.kind != captureNone {
		t.Fatal("corner Tools stole the client's release")
	}
	releases := 0
	for _, delivered := range apps.events {
		if delivered.id == apps.surfaces[0].ID && delivered.event.Kind == experience.PointerUp {
			releases++
		}
	}
	if releases != 1 {
		t.Fatal("client did not receive exactly one captured release over corner tools")
	}
}

func TestCornerToolsPaletteTraitWorksOnAnIndependentlyAuthoredSkin(t *testing.T) {
	w, _ := cornerToolsDesktop(t)
	selected, _ := skin.Builtin("plasma")
	selected.ID = "user.corner-tools"
	selected.Desktop.Chrome = "corner-tools"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	if w.skinDesktopDockBase() != skinCornerDockIndex || len(w.skinDesktopEntries()) != 4 {
		t.Fatal("corner chrome was selected by built-in ID rather than its generic trait")
	}
}
