package app

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/modelapp"
	"github.com/codemodify/worldr/internal/noteapp"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/researchapp"
)

// Native viewers expose a shared open contract; collections additionally reserve
// a fresh stable key. Single managers remain useful for isolated integration tests.
type fileViewer interface {
	experience.Applications
	OpenFile(*os.File, string) (string, error)
}

func nextViewerKey(viewer fileViewer, fallback string) (string, error) {
	if viewer == nil {
		return "", fmt.Errorf("viewer is unavailable")
	}
	if collection, ok := viewer.(interface{ NextKey() (string, error) }); ok {
		return collection.NextKey()
	}
	return fallback, nil
}
func placeOpened(work experience.Experience, key string) {
	if placer, ok := work.(experience.ApplicationNewPlacement); ok {
		placer.PlaceNewApplication(key)
	}
}
func connectFileBrowser(browser *projectapp.Provider, player fileViewer, photos fileViewer, hub *applicationHub, work experience.Experience, models ...*modelapp.Manager) {
	connectFileBrowserWithResearch(browser, player, photos, nil, hub, work, models...)
}

func connectFileBrowserWithResearch(browser *projectapp.Provider, player fileViewer, photos fileViewer, research *researchapp.Manager, hub *applicationHub, work experience.Experience, models ...*modelapp.Manager) {
	connectFileBrowserWithNativeTools(browser, player, photos, research, nil, hub, work, models...)
}

// connectFileBrowserWithNativeTools is the complete native document router.
// The older wrappers remain intentionally narrow for package integration tests
// and callers which only provide media/model/research tools.
func connectFileBrowserWithNativeTools(browser *projectapp.Provider, player fileViewer, photos fileViewer, research *researchapp.Manager, notes *noteapp.Manager, hub *applicationHub, work experience.Experience, models ...*modelapp.Manager) {
	browser.SetOpenHandler(func(file *os.File, name string) error {
		var viewer fileViewer
		fallback := "native:media-player"
		switch {
		case modelapp.IsModelPath(name):
			if len(models) == 0 || models[0] == nil {
				return fmt.Errorf("model inspector is unavailable")
			}
			viewer = models[0]
			fallback = "native:model"
		case projectapp.IsPhotoPath(name):
			viewer = photos
			fallback = "native:photo-viewer"
		case projectapp.IsDatasetPath(name):
			viewer = research
			fallback = "native:research-workbench"
		case noteapp.IsNotePath(name):
			viewer = notes
			fallback = "native:note"
		default:
			viewer = player
		}
		key, err := nextViewerKey(viewer, fallback)
		if err != nil {
			return err
		}
		if err = checkSessionPlacement(work, hub, key); err != nil {
			return err
		}
		key, err = viewer.OpenFile(file, name)
		if err == nil {
			placeOpened(work, key)
		}
		return err
	})
}
