package mediaapp

import (
	"fmt"
	"math"
	"os"

	"github.com/codemodify/worldr/internal/media"
	"github.com/codemodify/worldr/internal/resourcepath"
)

const maxSessionPosition = 365 * 24 * 60 * 60 // One year, in seconds.

type SessionState struct {
	Path     string  `json:"path"`
	Position float64 `json:"position"`
	Paused   bool    `json:"paused"`
	Volume   float64 `json:"volume"`
	Muted    bool    `json:"muted"`
}

func (s SessionState) Validate() error {
	if err := resourcepath.Validate(s.Path); err != nil {
		return fmt.Errorf("media session path: %w", err)
	}
	if math.IsNaN(s.Position) || math.IsInf(s.Position, 0) || s.Position < 0 || s.Position > maxSessionPosition {
		return fmt.Errorf("media session position must be between 0 and %d seconds", maxSessionPosition)
	}
	if math.IsNaN(s.Volume) || math.IsInf(s.Volume, 0) || s.Volume < 0 || s.Volume > 100 {
		return fmt.Errorf("media session volume must be 0..100")
	}
	return nil
}

type pendingResume struct {
	state        SessionState
	seekIssued   bool
	seekRevision uint64
}

// RestoreFile has the same descriptor ownership as OpenFile: success transfers
// ownership, and failure leaves the caller's descriptor and current player intact.
// The saved path is validated as session data; the actual source remains file.
func (m *Manager) RestoreFile(file *os.File, name string, state SessionState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	return m.openFile(file, name, &state)
}

// SessionState resolves the retained descriptor again, following renames while
// refusing unlinked/replaced paths. A save during loading retains the desired
// resume point rather than the decoder's temporary paused-at-zero state.
func (m *Manager) SessionState() (SessionState, bool) {
	if m.closed || m.player == nil || m.file == nil {
		return SessionState{}, false
	}
	path, err := resourcepath.FromFile(m.file)
	if err != nil {
		return SessionState{}, false
	}
	s := SessionState{Path: path, Position: m.state.Position, Paused: m.state.Paused, Volume: m.state.Volume, Muted: m.state.Muted}
	if m.resume != nil {
		s = m.resume.state
		s.Path = path
	}
	// Decoder snapshots are normally finite and bounded. Keep session output
	// valid even if a damaged container reports implausible metadata.
	if math.IsNaN(s.Position) || math.IsInf(s.Position, 0) {
		s.Position = 0
	}
	s.Position = max(0, min(maxSessionPosition, s.Position))
	if math.IsNaN(s.Volume) || math.IsInf(s.Volume, 0) {
		s.Volume = 70
	}
	s.Volume = max(0, min(100, s.Volume))
	return s, true
}

func (m *Manager) resumePlayback(state *media.State) {
	r := m.resume
	if r == nil {
		return
	}
	if state.Error != "" {
		m.resume = nil // Keep a failed restore paused, with the decoder error visible.
		return
	}
	if !state.Loaded {
		return
	}
	if !r.seekIssued {
		position := r.state.Position
		if state.Duration > 0 {
			position = min(position, state.Duration)
		}
		if err := m.player.Seek(position); err != nil {
			m.remember(err)
			m.resume = nil
			return
		}
		r.seekIssued, r.seekRevision = true, state.SeekRevision
		return
	}
	if state.SeekRevision == r.seekRevision && !state.Ended {
		return
	}
	if err := m.player.Pause(r.state.Paused); err != nil {
		m.remember(err)
	} else {
		state.Paused = r.state.Paused
	}
	m.resume = nil
}
