package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

// SetSkin installs an owned, validated appearance package. Appearance never
// edits application placement, focus, or the undoable workspace document.
func (w *Workspace) SetSkin(selected skin.Skin) error {
	if err := selected.Validate(); err != nil {
		return err
	}
	owned := selected.Clone()
	w.activeSkin = &owned
	w.skinPreview.dirty = true
	w.publishSkin()
	return nil
}

// CurrentSkin returns an independent copy suitable for exporting or editing.
func (w *Workspace) CurrentSkin() *skin.Skin {
	if w.activeSkin == nil {
		return nil
	}
	copy := w.activeSkin.Clone()
	return &copy
}

func (w *Workspace) publishSkin() {
	if w.activeSkin == nil {
		return
	}
	if receiver, ok := w.applications.(experience.ApplicationSkinSetter); ok {
		receiver.SetSkin(w.activeSkin.Clone())
	}
}

func (w *Workspace) clearSkin() {
	if w.activeSkin == nil {
		return
	}
	w.activeSkin = nil
	w.skinPreview.dirty = true
	w.publishControlTheme()
}
