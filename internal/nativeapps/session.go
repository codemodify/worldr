package nativeapps

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/resourcepath"
	"github.com/codemodify/worldr/internal/terminal"
)

// TerminalState describes a fresh shell to reopen, never a process snapshot,
// command history, or an instruction to replay a previously running command.
type TerminalState struct {
	Key       string `json:"key"`
	Directory string `json:"directory"`
	// Tasks are inert definitions. Restoring this state never sends them to the
	// fresh shell; the person must stage or execute a recipe from Tasks.
	Tasks []TaskRecipe `json:"tasks,omitempty"`
}

func terminalKeySlot(key string) (int, error) {
	for slot := 0; slot < maxNativeTerminals; slot++ {
		if key == terminalSlotKey(slot) {
			return slot, nil
		}
	}
	return -1, fmt.Errorf("invalid terminal key %q", key)
}

// Validate checks the manifest representation without opening files or starting
// a process. The host separately checks duplicate keys across the manifest.
func (s TerminalState) Validate() error {
	if _, err := terminalKeySlot(s.Key); err != nil {
		return err
	}
	if err := resourcepath.Validate(s.Directory); err != nil {
		return fmt.Errorf("terminal %q directory: %w", s.Key, err)
	}
	if err := validateTaskRecipes(s.Tasks); err != nil {
		return fmt.Errorf("terminal %q: %w", s.Key, err)
	}
	return nil
}

// SessionTerminals snapshots visible terminal slots in registration-slot order.
// Stable key gaps remain intact, and merely capturing state never changes focus
// or removes exited shells whose final output is still visible.
func (m *Manager) SessionTerminals() []TerminalState {
	if m.closed {
		return nil
	}
	var states []TerminalState
	for _, entry := range m.slots {
		if entry == nil || entry.provider.closed || entry.provider.dismissed || len(entry.provider.Surfaces()) == 0 {
			continue
		}
		if directory, err := entry.provider.WorkingDirectory(); err == nil && resourcepath.Validate(directory) == nil {
			entry.directory = directory
		}
		if entry.directory != "" {
			states = append(states, TerminalState{Key: entry.key, Directory: entry.directory, Tasks: entry.provider.TaskRecipes()})
		}
	}
	return states
}

func validateTerminalDirectory(directory *os.File) error {
	if directory == nil {
		return fmt.Errorf("a terminal directory is required")
	}
	info, err := directory.Stat()
	if err != nil {
		return fmt.Errorf("terminal directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("terminal location must be a directory")
	}
	return nil
}

// RestoreTerminal starts a new configured shell in precisely the saved slot.
// It borrows directory through synchronous startup; defaults, existing slots,
// keyboard focus, and the caller's ownership of directory remain unchanged.
func (m *Manager) RestoreTerminal(key string, directory *os.File) (string, error) {
	if m.closed {
		return "", terminal.ErrClosed
	}
	slot, err := terminalKeySlot(key)
	if err != nil {
		return "", err
	}
	if err := validateTerminalDirectory(directory); err != nil {
		return "", err
	}
	if err := m.prepareTerminalLaunch(); err != nil {
		return "", err
	}
	if m.slots[slot] != nil {
		return "", fmt.Errorf("terminal key %q is already open", key)
	}
	options := m.options
	options.Directory = directory
	return m.launchTerminalAt(slot, options)
}

// RestoreTerminalState starts a fresh shell and restores only inert task
// definitions. No recipe is staged or executed during restoration.
func (m *Manager) RestoreTerminalState(state TerminalState, directory *os.File) (string, error) {
	if m.closed {
		return "", terminal.ErrClosed
	}
	if err := state.Validate(); err != nil {
		return "", err
	}
	key, err := m.RestoreTerminal(state.Key, directory)
	if err != nil {
		return "", err
	}
	slot, _ := terminalKeySlot(key)
	if err := m.slots[slot].provider.SetTaskRecipes(state.Tasks); err != nil {
		// Validation above makes this unreachable, but never keep a partially
		// restored terminal if that invariant changes later.
		m.slots[slot].provider.CloseApplication(1)
		m.detachClosed()
		return "", err
	}
	return key, nil
}
