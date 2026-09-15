package wlsrv

import (
	"syscall"

	"github.com/codemodify/worldr/internal/engine"
	"github.com/codemodify/worldr/internal/wayland"
)

// IconSize is the preferred SSD / panel icon edge in pixels.
// Advertised via xdg_toplevel_icon_manager_v1.icon_size.
const IconSize = 16

type iconSnap struct {
	pix          []byte
	w, h, stride int
}

type toplevelIcon struct {
	name  string
	snaps []*iconSnap
}

func (ic *iconSnap) apply(a *engine.Actor) {
	if a == nil {
		return
	}
	if ic == nil || len(ic.pix) == 0 || ic.w < 1 || ic.h < 1 {
		a.ClearIcon()
		return
	}
	out := make([]byte, len(ic.pix))
	copy(out, ic.pix)
	a.IconPix = out
	a.IconW, a.IconH, a.IconStride = ic.w, ic.h, ic.stride
}

func pickIcon(snaps []*iconSnap, want int) *iconSnap {
	if len(snaps) == 0 {
		return nil
	}
	if want < 1 {
		want = IconSize
	}
	best := snaps[0]
	bestD := absInt(best.w - want)
	for _, s := range snaps[1:] {
		if s == nil {
			continue
		}
		d := absInt(s.w - want)
		if d < bestD {
			best, bestD = s, d
		}
	}
	return best
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func snapshotShm(b *shmBuffer) *iconSnap {
	if b == nil || b.pool == nil || b.w <= 0 || b.h <= 0 || b.stride <= 0 {
		return nil
	}
	n := b.stride * b.h
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	need := b.offset + n
	switch {
	case b.pool.mem != nil && need <= len(b.pool.mem) && b.offset >= 0:
		copy(out, b.pool.mem[b.offset:need])
	case b.pool.fd > 0 && b.offset >= 0:
		got, err := syscall.Pread(b.pool.fd, out, int64(b.offset))
		if err != nil || got != n {
			return nil
		}
	default:
		return nil
	}
	return &iconSnap{pix: out, w: b.w, h: b.h, stride: b.stride}
}

func (c *Client) reqIconMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // destroy
		return nil
	case 1: // create_icon
		id, err := cur.U32()
		if err != nil {
			return err
		}
		c.objs[id] = &object{id: id, kind: kindToplevelIcon, icon: &toplevelIcon{}}
	case 2: // set_icon(toplevel, icon|null)
		tid, err := cur.U32()
		if err != nil {
			return err
		}
		iid, err := cur.U32()
		if err != nil {
			return err
		}
		var snap *iconSnap
		if iid != 0 {
			if io := c.objs[iid]; io != nil && io.icon != nil {
				snap = pickIcon(io.icon.snaps, IconSize)
			}
		}
		to := c.objs[tid]
		if to == nil || to.xdgT == nil {
			// get_toplevel not processed yet — keep the last set/unset.
			if c.pendingIcon == nil {
				c.pendingIcon = map[uint32]iconPending{}
			}
			c.pendingIcon[tid] = iconPending{snap: snap}
			return nil
		}
		to.xdgT.icon = snap
		if to.xdgT.xdg != nil && to.xdgT.xdg.surf != nil {
			snap.apply(to.xdgT.xdg.surf.actor)
		}
	}
	return nil
}

func (c *Client) reqToplevelIcon(o *object, op uint16, cur *wayland.Cursor) error {
	if o.icon == nil {
		o.icon = &toplevelIcon{}
	}
	switch op {
	case 0: // destroy
		delete(c.objs, o.id)
	case 1: // set_name
		s, err := cur.String()
		if err != nil {
			return err
		}
		o.icon.name = s
	case 2: // add_buffer(buffer, scale)
		bid, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.I32() // scale — v0 picks by pixel size
		if bo := c.objs[bid]; bo != nil && bo.buf != nil {
			if snap := snapshotShm(bo.buf); snap != nil {
				o.icon.snaps = append(o.icon.snaps, snap)
			}
			_ = c.send(bid, 0, nil, nil) // wl_buffer.release
		}
	}
	return nil
}

func (c *Client) sendIconMgr(id uint32) error {
	// icon_size then done. 16 fits the 28px title bar; 24 for panel.
	if err := c.send(id, 0, wayland.PutI32(nil, IconSize), nil); err != nil {
		return err
	}
	if err := c.send(id, 0, wayland.PutI32(nil, 24), nil); err != nil {
		return err
	}
	return c.send(id, 1, nil, nil)
}
