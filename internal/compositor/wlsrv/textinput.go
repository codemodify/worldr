package wlsrv

import "github.com/codemodify/worldr/internal/wayland"

// zwp_text_input_v3 (interface version 1) request / event opcodes.
const (
	zwpTextInputDestroy uint16 = 0
	zwpTextInputEnable  uint16 = 1
	zwpTextInputDisable uint16 = 2
	zwpTextInputCommit  uint16 = 7

	zwpTextInputEnter uint16 = 0
	zwpTextInputLeave uint16 = 1
	zwpTextInputDone  uint16 = 5

	zwpTextInputMgrDestroy uint16 = 0
	zwpTextInputMgrGet     uint16 = 1
)

// textInput is one zwp_text_input_v3 object. v0: advertise + enter/leave
// following keyboard focus + enable/disable/commit → done. No ibus/fcitx
// bridge; compose stays on the xkb keymap via wl_keyboard.key.
type textInput struct {
	id      uint32
	pending int8 // +1 enable, -1 disable, 0 none (applied on commit)
	enabled bool
	entered uint32
	commits uint32
}

func (c *Client) reqTextInputMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case zwpTextInputMgrDestroy:
		return nil
	case zwpTextInputMgrGet:
		id, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32() // seat
		ti := &textInput{id: id}
		c.objs[id] = &object{id: id, kind: kindTextInput, ti: ti}
		if c.kbdSurf != 0 {
			c.sendTextInputEnter(ti, c.kbdSurf)
		}
	}
	return nil
}

func (c *Client) reqTextInput(o *object, op uint16, _ *wayland.Cursor) error {
	ti := o.ti
	if ti == nil {
		return nil
	}
	switch op {
	case zwpTextInputDestroy:
		if ti.entered != 0 {
			c.sendTextInputLeave(ti, ti.entered)
		}
		delete(c.objs, o.id)
	case zwpTextInputEnable:
		// After leave, ignore until the next enter (protocol).
		if ti.entered != 0 {
			ti.pending = 1
		}
	case zwpTextInputDisable:
		if ti.entered != 0 {
			ti.pending = -1
		}
	case zwpTextInputCommit:
		ti.commits++
		if ti.entered != 0 {
			switch ti.pending {
			case 1:
				ti.enabled = true
			case -1:
				ti.enabled = false
			}
		}
		ti.pending = 0
		// Always ack so the client does not stall. No preedit/commit_string:
		// keys stay on wl_keyboard + xkb (avoids double-insert).
		_ = c.send(ti.id, zwpTextInputDone, wayland.PutU32(nil, ti.commits), nil)
	}
	return nil
}

func (c *Client) textInputEnter(s *surface) {
	if s == nil {
		return
	}
	for _, o := range c.objs {
		if o == nil || o.kind != kindTextInput || o.ti == nil {
			continue
		}
		if o.ti.entered == s.id {
			continue
		}
		if o.ti.entered != 0 {
			c.sendTextInputLeave(o.ti, o.ti.entered)
		}
		c.sendTextInputEnter(o.ti, s.id)
	}
}

func (c *Client) textInputLeave(sid uint32) {
	if sid == 0 {
		return
	}
	for _, o := range c.objs {
		if o == nil || o.kind != kindTextInput || o.ti == nil {
			continue
		}
		if o.ti.entered == sid {
			c.sendTextInputLeave(o.ti, sid)
		}
	}
}

func (c *Client) sendTextInputEnter(ti *textInput, sid uint32) {
	if ti == nil || sid == 0 {
		return
	}
	_ = c.send(ti.id, zwpTextInputEnter, wayland.PutU32(nil, sid), nil)
	ti.entered = sid
}

func (c *Client) sendTextInputLeave(ti *textInput, sid uint32) {
	if ti == nil || sid == 0 {
		return
	}
	_ = c.send(ti.id, zwpTextInputLeave, wayland.PutU32(nil, sid), nil)
	ti.entered = 0
	ti.enabled = false
	ti.pending = 0
}
