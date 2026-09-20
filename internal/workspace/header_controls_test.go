package workspace

import (
	"fmt"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

func headerDesktopApplications(t *testing.T, count int) (*Workspace, *fakeApplications) {
	t.Helper()
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApplications{}
	for i := 0; i < count; i++ {
		apps.surfaces = append(apps.surfaces, experience.ApplicationSurface{
			ID: uint64(800 + i), Key: fmt.Sprintf("header-%d/window-1", i+1), Title: fmt.Sprintf("Window %d", i+1), Texture: texture,
		})
	}
	w.SetApplications(apps)
	return w, apps
}

func clickDesignButton(t *testing.T, w *Workspace, target box) {
	t.Helper()
	x := w.ox + (target.x+target.w/2)*w.scale
	y := w.oy + (target.y+target.h/2)*w.scale
	if !pointer(w, experience.PointerDown, x, y) || w.pointer.kind != captureButton {
		t.Fatalf("button at %+v did not capture its press", target)
	}
	if !pointer(w, experience.PointerUp, x, y) {
		t.Fatalf("button at %+v did not consume its release", target)
	}
}

func TestDesktopFormerHeaderControlsAreInertAtEveryScale(t *testing.T) {
	formerControls := map[string]box{
		"reset-view":             resetViewButton,
		"place-group":            applicationPlaceButton,
		"group-selected":         applicationGroupButton,
		"ungroup":                applicationUngroupButton,
		"selection-depth-toggle": applicationDepthHeaderButton,
	}
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w, _ := headerDesktopApplications(t, 2)
			w.Draw(dimensions.width, dimensions.height)
			for name, target := range formerControls {
				t.Run(name, func(t *testing.T) {
					x, y := target.x+target.w/2, target.y+target.h/2
					if action, ok := w.buttonAction(x, y); ok {
						t.Fatalf("removed desktop header control retained button action %+v", action)
					}
				})
			}
		})
	}
}

func TestAxialHeaderResetAndApplicationControlsRemainAvailable(t *testing.T) {
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w, apps := multipleApplications(t, 2)
			w.Draw(dimensions.width, dimensions.height)
			command(t, w, Action{Kind: PanCamera, DeltaX: 4, DeltaY: -2, DeltaDepth: 1})
			clickDesignButton(t, w, resetViewButton)
			if w.Document().View.Camera != initialModel().document().View.Camera {
				t.Fatal("AXIAL Reset header target no longer restores the study")
			}

			command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
			clickDesignButton(t, w, applicationPlaceButton)
			if !w.Document().View.Application.Placing {
				t.Fatal("AXIAL Place / Group header target no longer enters placement")
			}
			clickDesignButton(t, w, applicationGroupButton)
			view := w.Document().View.Application
			if view.Layouts[0].Group == 0 || view.Layouts[0].Group != view.Layouts[1].Group {
				t.Fatal("AXIAL Group Selected header target no longer groups the selection")
			}
			clickDesignButton(t, w, applicationUngroupButton)
			view = w.Document().View.Application
			if view.Layouts[0].Group != 0 || view.Layouts[1].Group != 0 {
				t.Fatal("AXIAL Ungroup header target no longer releases the selection")
			}
		})
	}
}

func TestRemovedSideControlTargetsDoNotDispatchButtons(t *testing.T) {
	formerControls := map[string]box{
		"old-depth-toggle": {32, 214, 208, 38},
		"old-place":        {32, 258, 208, 38},
		"read-selected":    {32, 302, 208, 38},
		"depth-minus":      {32, 390, 100, 38},
		"depth-plus":       {140, 390, 100, 38},
		"size-compact":     {32, 434, 208, 38},
		"old-group":        {32, 478, 208, 38},
		"old-ungroup":      {32, 522, 208, 38},
		"close-selected":   {32, 744, 208, 32},
	}
	w, _ := headerDesktopApplications(t, 1)
	w.Draw(1440, 900)
	for name, target := range formerControls {
		t.Run(name, func(t *testing.T) {
			x, y := target.x+target.w/2, target.y+target.h/2
			if action, ok := w.buttonAction(x, y); ok {
				t.Fatalf("removed %s control retained button action %+v", name, action)
			}
		})
	}
}

func TestRemovedHeaderToolsAndSpacesTargetIsInertAtEveryScale(t *testing.T) {
	target := box{215, 48, 310, 34}
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w, apps := headerDesktopApplications(t, 1)
			provider := &rollbackLaunchingApplications{launchingApplications: terminalLauncher(t, apps)}
			w.SetApplications(provider)
			w.Draw(dimensions.width, dimensions.height)
			before, history, historyLen := w.Document(), w.historyPosition, len(w.history)
			x := w.ox + (target.x+target.w/2)*w.scale
			y := w.oy + (target.y+target.h/2)*w.scale
			down := pointer(w, experience.PointerDown, x, y)
			up := pointer(w, experience.PointerUp, x, y)
			if down || up || w.pointer.kind != captureNone || w.commands != nil && w.commands.open {
				t.Fatal("removed Tools + Spaces header target retained an input action")
			}
			if w.Document() != before || w.historyPosition != history || len(w.history) != historyLen || len(provider.launched) != 0 || len(provider.closed) != 0 {
				t.Fatal("removed Tools + Spaces header target changed workspace or provider state")
			}
		})
	}
}
