package workspace

import "time"

// The renderer accepts a normalized phase rather than wall-clock time. Keeping
// the clock here lets the workspace freeze ambient projection motion without
// stopping user-requested model, media or data playback.
const hologramCycle = 4 * time.Second

func (w *Workspace) updateHologram(dt time.Duration) {
	if w.m.reducedMotion || dt <= 0 {
		return
	}
	w.hologramPhase = (w.hologramPhase + dt%hologramCycle) % hologramCycle
}
