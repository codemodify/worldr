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

func TestHeaderResetPlaceGroupAndUngroupTargetsAtEveryScale(t *testing.T) {
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w, apps := headerDesktopApplications(t, 2)
			w.Draw(dimensions.width, dimensions.height)
			command(t, w, Action{Kind: PanCamera, DeltaX: 4, DeltaY: -2, DeltaDepth: 1})
			if w.Document().View.Camera == initialModel().document().View.Camera {
				t.Fatal("fixture did not move the camera before Reset View")
			}

			clickDesignButton(t, w, resetViewButton)
			if w.Document().View.Camera != initialModel().document().View.Camera {
				t.Fatal("Reset View header target did not restore the camera")
			}

			command(t, w, Action{Kind: SelectApplication, ApplicationKey: apps.surfaces[1].Key, Additive: true})
			if w.Document().View.Application.Selected != 3 {
				t.Fatal("fixture did not select both applications")
			}
			clickDesignButton(t, w, applicationPlaceButton)
			if !w.Document().View.Application.Placing {
				t.Fatal("Place / Group header target did not enter placement")
			}

			clickDesignButton(t, w, applicationGroupButton)
			view := w.Document().View.Application
			if view.Layouts[0].Group == 0 || view.Layouts[0].Group != view.Layouts[1].Group {
				t.Fatal("Group Selected header target did not group the selection")
			}

			clickDesignButton(t, w, applicationUngroupButton)
			view = w.Document().View.Application
			if view.Layouts[0].Group != 0 || view.Layouts[1].Group != 0 {
				t.Fatal("Ungroup header target did not release the selection")
			}
		})
	}
}

func TestRemovedToolbarAndSideControlAreasAreInert(t *testing.T) {
	formerControls := map[string]box{
		"new-terminal":   {535, 30, 180, 37},
		"reduced-motion": {912, 10, 173, 20},
		"old-place":      {32, 258, 208, 38},
		"read-selected":  {32, 302, 208, 38},
		"depth-minus":    {32, 390, 100, 38},
		"depth-plus":     {140, 390, 100, 38},
		"size-compact":   {32, 434, 208, 38},
		"old-group":      {32, 478, 208, 38},
		"old-ungroup":    {32, 522, 208, 38},
		"close-selected": {32, 744, 208, 32},
	}
	for _, dimensions := range []struct {
		width, height int
	}{{1440, 900}, {2880, 1800}} {
		t.Run(fmt.Sprintf("%dx%d", dimensions.width, dimensions.height), func(t *testing.T) {
			w, apps := headerDesktopApplications(t, 2)
			provider := &rollbackLaunchingApplications{launchingApplications: terminalLauncher(t, apps)}
			w.SetApplications(provider)
			w.Draw(dimensions.width, dimensions.height)
			before, history, historyLen := w.Document(), w.historyPosition, len(w.history)

			for name, target := range formerControls {
				t.Run(name, func(t *testing.T) {
					x := w.ox + (target.x+target.w/2)*w.scale
					y := w.oy + (target.y+target.h/2)*w.scale
					if pointer(w, experience.PointerDown, x, y) || pointer(w, experience.PointerUp, x, y) || w.pointer.kind != captureNone {
						t.Fatalf("removed %s control retained an input target", name)
					}
					if w.Document() != before || w.historyPosition != history || len(w.history) != historyLen || len(provider.launched) != 0 || len(provider.closed) != 0 {
						t.Fatalf("removed %s control still changed workspace or provider state", name)
					}
				})
			}
		})
	}
}
