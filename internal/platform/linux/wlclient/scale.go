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
	out := w.hostOutScale
	if w.surfOut != 0 && w.outScale != nil {
		if sc, ok := w.outScale[w.surfOut]; ok && sc > 0 {
			out = sc
		}
	}
	return wlsrv.CombineHostScale(w.hostFrac120, out)
}

// OnHostScale is called when Plasma's output / preferred_scale changes.
// If a host scale is already known, fn is invoked once immediately.
func (w *Window) OnHostScale(fn func(float64)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.onHostScale = fn
	known := w.hostFrac120 != 0 || w.hostOutScale != 0
	sc := w.hostScaleLocked()
	w.mu.Unlock()
	if fn != nil && known {
		go fn(sc)
	}
}

func (w *Window) handleScale(msg wayland.Message) error {
	if w.isHostOutput(msg.Object) && msg.Opcode == wlOutputScale {
		cur := wayland.NewCursor(msg.Payload, nil)
		sc, err := cur.I32()
		if err != nil || sc < 1 {
			return nil
		}
		if w.outScale == nil {
			w.outScale = map[uint32]int32{}
		}
		prev := w.outScale[msg.Object]
		w.outScale[msg.Object] = sc
		if w.output == msg.Object || w.output == 0 {
			w.output = msg.Object
			if w.hostOutScale == 0 {
				w.hostOutScale = sc
			}
		}
		if w.surfOut == 0 || w.surfOut == msg.Object {
			if w.hostOutScale == sc && prev == sc {
				return nil
			}
			w.hostOutScale = sc
			logClient("host wl_output.scale=%d id=%d", sc, msg.Object)
			w.emitHostScale()
		}
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
		return nil
	}
	if w.surf != 0 && msg.Object == w.surf {
		cur := wayland.NewCursor(msg.Payload, nil)
		switch msg.Opcode {
		case 0: // enter
			oid, err := cur.U32()
			if err != nil || oid == 0 {
				return nil
			}
			if w.surfOut == oid {
				return nil
			}
			w.surfOut = oid
			if w.outScale != nil {
				if sc := w.outScale[oid]; sc > 0 {
					w.hostOutScale = sc
				}
			}
			logClient("host surface.enter output=%d", oid)
			w.emitHostScale()
		case 1: // leave
			oid, _ := cur.U32()
			if w.surfOut == oid {
				w.surfOut = 0
			}
		}
	}
	return nil
}

func (w *Window) isHostOutput(id uint32) bool {
	if id == 0 {
		return false
	}
	if id == w.output {
		return true
	}
	for _, o := range w.hostOutIDs {
		if o == id {
			return true
		}
	}
	if w.outScale != nil {
		if _, ok := w.outScale[id]; ok {
			return true
		}
	}
	return false
}

func (w *Window) emitHostScale() {
	fn := w.onHostScale
	if fn == nil {
		return
	}
	sc := w.hostScaleLocked()
	go fn(sc)
}
