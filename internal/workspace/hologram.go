package workspace

import "time"

// The renderer accepts a normalized phase rather than wall-clock time. Keeping
// the clock here keeps ambient projection motion independent from model,
// media, or data playback.
const hologramCycle = 4 * time.Second

func (w *Workspace) updateHologram(dt time.Duration) {
	if dt <= 0 {
		return
	}
	w.hologramPhase = (w.hologramPhase + dt%hologramCycle) % hologramCycle
}
