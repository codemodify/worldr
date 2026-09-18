package app

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/projectapp"
)

type directoryTerminalLauncher interface {
	NextTerminalKey() (string, error)
	LaunchTerminalInDirectory(*os.File) (string, error)
}

// The browser lends an already-open, anchored directory during this callback.
// The launch never reparses a pathname or changes focus after asynchronous I/O.
func connectTerminalBrowser(browser *projectapp.Provider, terminal directoryTerminalLauncher, hub *applicationHub, work experience.Experience) {
	browser.SetTerminalHandler(func(directory *os.File, _ string) error {
		if terminal == nil || hub == nil {
			return fmt.Errorf("native terminal is unavailable in this workspace")
		}
		if hub.closed {
			return errApplicationHubClosed
		}
		if len(hub.Surfaces()) >= 32 {
			return fmt.Errorf("workspace is full; close a window before opening a terminal")
		}
		key, err := terminal.NextTerminalKey()
		if err != nil {
			return err
		}
		if checker, ok := work.(experience.ApplicationPlacementChecker); ok {
			if err := checker.CheckApplicationPlacement(key); err != nil {
				return err
			}
		}
		key, err = terminal.LaunchTerminalInDirectory(directory)
		if err == nil {
			placeOpened(work, key)
		}
		return err
	})
}
