package projectapp

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/resourcepath"
)

// SessionState records navigation only. Restoring it never opens a photo,
// video, or terminal, grants keyboard focus, or restores preview contents.
type SessionState struct {
	Root      string `json:"root"`
	Directory string `json:"directory,omitempty"`
	Selected  string `json:"selected,omitempty"`
}

// Validate checks the saved shape without filesystem I/O. Paths are data only;
// NewSession anchors the root and checks child traversal when restoring it.
func (s SessionState) Validate() error {
	if err := resourcepath.Validate(s.Root); err != nil {
		return fmt.Errorf("project session root: %w", err)
	}
	if !validSessionText(s.Directory) || filepath.IsAbs(s.Directory) ||
		s.Directory != "" && (filepath.Clean(s.Directory) != s.Directory || s.Directory == "." || s.Directory == ".." || strings.HasPrefix(s.Directory, ".."+string(filepath.Separator))) {
		return fmt.Errorf("project session directory must be a clean path relative to the root")
	}
	if !validSessionText(s.Selected) || s.Selected != "" &&
		(filepath.Base(s.Selected) != s.Selected || s.Selected == "." || s.Selected == ".." || filepath.IsAbs(s.Selected)) {
		return fmt.Errorf("project session selection must be a single filename")
	}
	return nil
}

func validSessionText(text string) bool {
	return len(text) <= 4096 && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}

// NewSession anchors the saved root and asynchronously restores its listing and
// selection. An unavailable root is an error; an unavailable nested folder
// falls back to the root with a visible notice. The saved root cannot reopen as
// a final symlink, and child traversal never follows symlinks.
func NewSession(state SessionState) (*Provider, error) {
	if err := state.Validate(); err != nil {
		return nil, err
	}
	reader, absolute, err := openSessionReader(state.Root)
	if err != nil {
		return nil, err
	}
	return newProviderAt(reader, absolute, state.Directory, state.Selected, true)
}

// SessionState snapshots navigation without changing selection or starting I/O
// on the reader worker. A pending listing retains its requested selection.
// Closed providers and roots/folders that cannot be represented safely in JSON
// are omitted. On Linux, the root's current pathname is recovered from its open
// descriptor when possible; an unlinked root with no verified pathname is omitted.
func (p *Provider) SessionState() (SessionState, bool) {
	if p.closed {
		return SessionState{}, false
	}
	state := SessionState{Root: p.root, Directory: p.directory}
	if p.sessionRoot != nil {
		state.Root = p.sessionRoot()
	}
	if p.loadingDirectory {
		state.Selected = p.pendingSelected
	} else if p.selected >= 0 && p.selected < len(p.entries) {
		state.Selected = p.entries[p.selected].name
	}
	// Linux filenames can be arbitrary bytes; JSON replacement would point to
	// a different filename. The containing folder is still safe to restore.
	if !validSessionText(state.Selected) {
		state.Selected = ""
	}
	if state.Validate() != nil {
		return SessionState{}, false
	}
	return state, true
}
