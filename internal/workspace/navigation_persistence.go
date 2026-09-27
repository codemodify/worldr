package workspace

import "fmt"

// navigationPreferences extends the desktop's version 1 document without
// storing live focus, pointer/key capture or presentation animation. ReturnFocus
// is a stable application key and may refer to a provider not yet reconnected.
type navigationPreferences struct {
	Home          bool   `json:"home"`
	MotionEnabled bool   `json:"motion_enabled"`
	Page          int    `json:"page"`
	ReturnFocus   string `json:"return_focus,omitempty"`
}

func (p *navigationPreferences) validate() error {
	if p == nil {
		return nil
	}
	if p.Page < 0 || p.Page >= MaxApplicationLayouts {
		return fmt.Errorf("navigation page must be between 0 and %d", MaxApplicationLayouts-1)
	}
	if p.ReturnFocus != "" && !validApplicationKey(p.ReturnFocus) {
		return fmt.Errorf("navigation return focus is not a valid application key")
	}
	return nil
}

func (w *Workspace) savedNavigationPreferences() *navigationPreferences {
	if w.navigation == nil {
		return nil
	}
	n := w.navigation
	return &navigationPreferences{Home: n.home, MotionEnabled: n.motionEnabled, Page: n.page, ReturnFocus: n.returnFocus}
}

func (w *Workspace) installNavigationPreferences(preferences *navigationPreferences) {
	if w.navigation == nil {
		return
	}
	selected := navigationPreferences{Home: true, MotionEnabled: true}
	if preferences != nil {
		selected = *preferences
	}
	// Install after the document: its initial layout may run while providers
	// are disconnected. Preserve the saved page until the next normal layout.
	w.navigation = &navigationState{
		home: selected.Home, motionEnabled: selected.MotionEnabled,
		page: selected.Page, returnFocus: selected.ReturnFocus,
		held: make(map[helpKey]bool), demoStep: -1,
	}
	if selected.Home {
		w.navigation.homePage = selected.Page
	} else if !w.m.applicationState.Reading || w.m.applicationState.Overview {
		w.navigation.projectPage = selected.Page
		w.navigation.projectPages[w.m.applicationState.Space] = selected.Page
	}
}
