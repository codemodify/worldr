package photoapp

import "github.com/codemodify/worldr/internal/resourcepath"

// SessionState contains only a reopenable resource, independent of its display
// title or workspace placement. Closing the viewer removes it from the session.
type SessionState struct {
	Path string `json:"path"`
}

func (s SessionState) Validate() error { return resourcepath.Validate(s.Path) }

func (m *Manager) SessionState() (SessionState, bool) {
	if m.closed || m.renderer == nil {
		return SessionState{}, false
	}
	path := m.path
	if m.photo == nil && m.loading {
		path = m.pendingPath
	}
	if path == "" {
		return SessionState{}, false
	}
	return SessionState{Path: path}, true
}
