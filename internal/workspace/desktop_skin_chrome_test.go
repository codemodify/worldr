package workspace

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func skinChromeDesktop(t *testing.T, kinds ...string) (*Workspace, *dockApplications) {
	t.Helper()
	w, apps := desktopDock(t, kinds...)
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the authored treatment independently of a preset's identity.
	selected.ID = "my-slate-desktop"
	selected.Desktop.Chrome = "slate-tabs"
	if err := w.SetSkin(selected); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	return w, apps
}

func skinChromePoint(w *Workspace, index int) (float32, float32) {
	b := w.skinDesktopEntries()[index].bounds
	return w.ox + (b.x+b.w/2)*w.scale, w.oy + (b.y+min(12, b.h/2))*w.scale
}

func TestSkinDesktopRailsAreAuthoredAndStayOnPhysicalEdges(t *testing.T) {
	w, _ := skinChromeDesktop(t)
	if !w.skinDesktopChromeVisible() || w.orbitPadVisible() {
		t.Fatal("authored desktop treatment did not replace legacy chrome")
	}
	for _, size := range [][2]int{{1440, 900}, {2048, 900}, {900, 1440}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w.Draw(size[0], size[1])
			left, right := w.skinDesktopRails()
			if abs(w.ox+left.x*w.scale) > .01 || abs(w.oy+left.y*w.scale) > .01 || abs(w.ox+(right.x+right.w)*w.scale-float32(size[0])) > .01 || abs(w.oy+(right.y+right.h)*w.scale-float32(size[1])) > .01 {
				t.Fatal("desktop rail failed to cover a physical edge")
			}
			for i := range w.skinDesktopEntries() {
				x, y := skinChromePoint(w, i)
				if got := w.applicationDockIndexAt((x-w.ox)/w.scale, (y-w.oy)/w.scale); got != skinDesktopDockIndex+i {
					t.Fatalf("scaled target %d resolved to %d", i, got)
				}
				if !pointer(w, experience.PointerMove, x, y) || w.applicationDockHover != skinDesktopDockIndex+i {
					t.Fatalf("rail target %d did not own its scaled hover", i)
				}
			}
		})
	}
	other, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetSkin(other); err != nil {
		t.Fatal(err)
	}
	if w.skinDesktopChromeVisible() || !w.orbitPadVisible() || w.drawSkinDesktopChrome() {
		t.Fatal("other skins inherited Merrick's desktop treatment")
	}
}

func TestSkinDesktopChromePaintUsesPackagePaletteAndLeavesCenterClear(t *testing.T) {
	w, _ := skinChromeDesktop(t)
	selected := w.CurrentSkin()
	selected.Palette["desktop-rail"] = "#123456"
	if err := w.SetSkin(*selected); err != nil {
		t.Fatal(err)
	}
	w.layout(2048, 900)
	w.canvas.Reset(2048, 900)
	if !w.drawSkinDesktopChrome() {
		t.Fatal("authored desktop chrome did not draw")
	}
	frame := w.canvas.Frame()
	found := false
	for _, v := range frame.Vertices {
		if !finite(float64(v.X)) || !finite(float64(v.Y)) || v.X > 86 && v.X < 1928 {
			t.Fatalf("rail decoration entered the client center or had invalid geometry: %+v", v)
		}
		if abs(v.R-float32(0x12)/255) < .001 && abs(v.G-float32(0x34)/255) < .001 && abs(v.B-float32(0x56)/255) < .001 {
			found = true
		}
	}
	if !found {
		t.Fatal("rail ignored the package's desktop palette")
	}
}

func TestSkinDesktopTabsLaunchExistingNativeActions(t *testing.T) {
	w, apps := skinChromeDesktop(t, "files", "research", "note", "terminal")
	w.Draw(2048, 900)
	for n, index := range []int{1, 2, 3, 17} {
		x, y := skinChromePoint(w, index)
		if !pointer(w, experience.PointerDown, x, y) || len(apps.launched) != n || w.pointer.kind != captureApplicationDock {
			t.Fatal("native launch did not wait for the completed rail click")
		}
		if !pointer(w, experience.PointerUp, x, y) || len(apps.launched) != n+1 {
			t.Fatal("rail click did not launch its native action")
		}
	}
	if !slices.Equal(apps.launched, []string{"files", "research", "note", "terminal"}) {
		t.Fatalf("wrong rail actions: %v", apps.launched)
	}
	if w.pointer.kind != captureNone {
		t.Fatal("rail launch retained pointer capture")
	}
}

func TestSkinDesktopUtilityActionsAndUnavailableIntent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index int
		check func(*Workspace) bool
	}{
		{"programs", 0, func(w *Workspace) bool { return w.commands != nil && w.commands.open }},
		{"merrick", 4, func(w *Workspace) bool { return w.settingsOpen }},
		{"personal", 5, func(w *Workspace) bool { return w.m.applicationState.Overview }},
		{"workspace", 6, func(w *Workspace) bool { return w.m.applicationState.Overview }},
		{"help", 18, func(w *Workspace) bool { return w.helpOpen }},
		{"system", 16, func(w *Workspace) bool { return w.settingsOpen }},
		{"settings", 19, func(w *Workspace) bool { return w.settingsOpen }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, apps := skinChromeDesktop(t)
			w.Draw(2880, 1800)
			x, y := skinChromePoint(w, tc.index)
			pointer(w, experience.PointerDown, x, y)
			if tc.check(w) {
				t.Fatal("utility activated before pointer release")
			}
			pointer(w, experience.PointerUp, x, y)
			if !tc.check(w) || len(apps.launched) != 0 {
				t.Fatal("utility did not route to its workspace action")
			}
		})
	}
	w, apps := skinChromeDesktop(t)
	x, y := skinChromePoint(w, 2)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if len(apps.launched) != 0 || !strings.Contains(w.applicationNotice, "Research launcher is unavailable") {
		t.Fatal("unavailable Records intent did not explain its missing provider")
	}
}

func TestSkinDesktopClickCancelDragResizeAndAppearanceSwitch(t *testing.T) {
	for _, mode := range []string{"cancel", "drag", "resize", "skin-switch"} {
		t.Run(mode, func(t *testing.T) {
			w, apps := skinChromeDesktop(t, "files")
			x, y := skinChromePoint(w, 1)
			pointer(w, experience.PointerDown, x, y)
			switch mode {
			case "cancel":
				w.Handle(experience.Event{Kind: experience.PointerCancel})
			case "drag":
				pointer(w, experience.PointerMove, x-30, y)
				pointer(w, experience.PointerMove, x, y)
			case "resize":
				w.Draw(2880, 1800)
				x, y = skinChromePoint(w, 1)
			case "skin-switch":
				other, err := skin.Builtin("plasma")
				if err != nil {
					t.Fatal(err)
				}
				if err := w.SetSkin(other); err != nil {
					t.Fatal(err)
				}
				x, y = dockPoint(w, 1)
			}
			pointer(w, experience.PointerUp, x, y)
			if len(apps.launched) != 0 || w.pointer.kind != captureNone {
				t.Fatal("cancelled or stale rail click launched an application")
			}
		})
	}
}

func TestSkinDesktopChromeOccludesClientsWithoutStealingTheirGrab(t *testing.T) {
	w, apps := skinChromeDesktop(t, "files")
	if _, err := apps.LaunchApplication("files"); err != nil {
		t.Fatal(err)
	}
	w.syncApplications()
	w.applicationHoveredID, w.applicationHover = apps.surfaces[0].ID, true
	left, _ := w.skinDesktopRails()
	x, y := w.ox+(left.x+40)*w.scale, w.oy+300*w.scale
	if !pointer(w, experience.PointerMove, x, y) || w.applicationHoveredID != 0 || w.applicationDockHover != -1 {
		t.Fatal("plain rail background leaked client hover")
	}
	w.applicationButtons = map[uint32]bool{272: true}
	if w.handleApplicationDock(experience.Event{Kind: experience.PointerUp, X: x, Y: y, Button: experience.ButtonPrimary}) {
		t.Fatal("rail stole the release of a held client button")
	}
}
