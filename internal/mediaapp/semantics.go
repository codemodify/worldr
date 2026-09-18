package mediaapp

import (
	"fmt"

	"github.com/codemodify/worldr/internal/nativeui"
)

// Semantics exposes transport and continuous controls without treating decoded
// video pixels as document content.
func (m *Manager) Semantics(id uint64) nativeui.SemanticTree {
	if !m.valid(id) || m.renderer == nil {
		return nativeui.SemanticTree{}
	}
	layout := m.renderer.layout()
	disabled := !m.state.Loaded || m.resume != nil
	playLabel := "Play"
	if !m.state.Paused && !m.state.Ended {
		playLabel = "Pause"
	}
	muteLabel := "Mute"
	if m.state.Muted {
		muteLabel = "Unmute"
	}
	nodes := []nativeui.Node{
		{ID: "back", Role: nativeui.RoleButton, Label: "Back 10 seconds", Bounds: layout.Back, Disabled: disabled},
		{ID: "play", Role: nativeui.RoleButton, Label: playLabel, Bounds: layout.Play, Disabled: disabled, Selected: !m.state.Paused},
		{ID: "forward", Role: nativeui.RoleButton, Label: "Forward 10 seconds", Bounds: layout.Forward, Disabled: disabled},
		{ID: "stop", Role: nativeui.RoleButton, Label: "Stop", Bounds: layout.Stop, Disabled: disabled},
		{ID: "mute", Role: nativeui.RoleButton, Label: muteLabel, Bounds: layout.Mute, Disabled: disabled, Selected: m.state.Muted},
		{ID: "position", Role: nativeui.RoleSlider, Label: "Playback position", Value: fmt.Sprintf("%.1f of %.1f seconds", m.state.Position, m.state.Duration), Bounds: layout.Seek, Disabled: disabled || m.state.Duration <= 0},
		{ID: "volume", Role: nativeui.RoleSlider, Label: "Volume", Value: fmt.Sprintf("%.0f percent", m.state.Volume), Bounds: layout.Volume, Disabled: disabled},
	}
	return nativeui.SemanticTree{Nodes: nodes}
}

func (c *Collection) Semantics(id uint64) nativeui.SemanticTree {
	if window := c.route(id); window != nil {
		return window.manager.Semantics(window.manager.next)
	}
	return nativeui.SemanticTree{}
}
