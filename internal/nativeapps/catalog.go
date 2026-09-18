package nativeapps

import "github.com/codemodify/worldr/internal/experience"

func (m *Manager) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{{Kind: "terminal", Title: "New terminal"}}
}
