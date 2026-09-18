package app

import (
	"errors"
	"fmt"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"time"
)

// Automatic recovery is bounded: a persistent driver failure must not create
// a tight destroy/recreate loop. A later isolated fault gets a fresh allowance.
type graphicsRecovery struct {
	since    time.Time
	attempts int
}

func (g *graphicsRecovery) allow(now time.Time) bool {
	if g.since.IsZero() || now.Sub(g.since) > time.Minute {
		g.since = now
		g.attempts = 0
	}
	if g.attempts >= 2 {
		return false
	}
	g.attempts++
	return true
}
func recoverableGraphics(err error) bool {
	return errors.Is(err, native.ErrDeviceLost) || errors.Is(err, native.ErrNeedsRecovery) || errors.Is(err, native.ErrGPUTimeout)
}
func (p *presenter) recover(g *graphicsRecovery, now time.Time, cause error) error {
	if !g.allow(now) {
		return fmt.Errorf("graphics recovery stopped after two failures within a minute: %w", cause)
	}
	var err error
	if p.outputs != nil {
		err = p.outputs.RecoverAffected(cause)
	} else {
		err = p.vk.Recover()
	}
	if err != nil {
		return fmt.Errorf("graphics recovery: %w", err)
	}
	w, h := p.vk.Size()
	if !validExtent(int(w), int(h)) {
		return fmt.Errorf("unsupported extent after recovery %dx%d", w, h)
	}
	p.w, p.h = int(w), int(h)
	if p.outputs != nil {
		p.w, p.h = p.outputs.Size()
	}
	if p.pixels != nil {
		p.pixels = make([]byte, p.w*p.h*4)
	}
	return nil
}
