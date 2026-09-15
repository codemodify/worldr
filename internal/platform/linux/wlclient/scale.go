//go:build linux

package wlclient

import (
	"github.com/codemodify/worldr/internal/compositor/wlsrv"
	"github.com/codemodify/worldr/internal/wayland"
)

const (
	wlOutputScale uint16 = 3 // v2
	fracPrefScale uint16 = 0
	fracMgrGet    uint16 = 1
)

func (w *Window) setupFracScale() error {
	if w.fracMgr == 0 || w.surf == 0 {
		return nil
	}
	w.fracID = w.alloc()
	p := wayland.PutU32(nil, w.fracID)
	p = wayland.PutU32(p, w.surf)
	if err := w.send(w.fracMgr, fracMgrGet, p, nil); err != nil {
		return err
	}
	logClient("fractional_scale id=%d surface=%d (host preferred_scale)", w.fracID, w.surf)
	return nil
}

// HostScale is the current host output scale (fractional 120ths preferred).
func (w *Window) HostScale() float64 {
	if w == nil {
		return 1
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hostScaleLocked()
}

func (w *Window) hostScaleLocked() float64 {
	return wlsrv.CombineHostScale(w.hostFrac120, w.hostOutScale)
}

// OnHostScale is called when Plasma's output / preferred_scale changes.
func (w *Window) OnHostScale(fn func(float64)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.onHostScale = fn
	w.mu.Unlock()
}

func (w *Window) handleScale(msg wayland.Message) error {
	if w.output != 0 && msg.Object == w.output && msg.Opcode == wlOutputScale {
		cur := wayland.NewCursor(msg.Payload, nil)
		sc, err := cur.I32()
		if err != nil || sc < 1 {
			return nil
		}
		if w.hostOutScale == sc {
			return nil
		}
		w.hostOutScale = sc
		logClient("host wl_output.scale=%d", sc)
		w.emitHostScale()
		return nil
	}
	if w.fracID != 0 && msg.Object == w.fracID && msg.Opcode == fracPrefScale {
		cur := wayland.NewCursor(msg.Payload, nil)
		n, err := cur.U32()
		if err != nil || n == 0 {
			return nil
		}
		if w.hostFrac120 == n {
			return nil
		}
		w.hostFrac120 = n
		logClient("host preferred_scale=%d/120 (%.2f)", n, wlsrv.ScaleFrom120ths(n))
		w.emitHostScale()
	}
	return nil
}

func (w *Window) emitHostScale() {
	fn := w.onHostScale
	if fn == nil {
		return
	}
	sc := w.hostScaleLocked()
	go fn(sc)
}
