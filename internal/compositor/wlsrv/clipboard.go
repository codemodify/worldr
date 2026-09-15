package wlsrv

import (
	"syscall"

	"github.com/codemodify/worldr/internal/clipbridge"
	"github.com/codemodify/worldr/internal/wayland"
)

const (
	mimeTextPlain = "text/plain"
	mimeTextUTF8  = "text/plain;charset=utf-8"
)

const (
	wlDataDevDataOffer uint16 = 0
	wlDataDevSelection uint16 = 5
	wlDataOfferOffer   uint16 = 0
	wlDataSrcSend      uint16 = 1
	wlDataSrcCancelled uint16 = 2

	primDevDataOffer uint16 = 0
	primDevSelection uint16 = 1
	primSrcSend      uint16 = 0
	primSrcCancelled uint16 = 1
)

type dataSource struct {
	id        uint32
	client    *Client
	mimes     []string
	primary   bool
	hostBytes []byte // nest import: transfer writes these instead of send
}

type dataOffer struct {
	id     uint32
	source *dataSource
	prim   bool
}

type selection struct {
	src    *Client
	source *dataSource
}

func (s *dataSource) sendOp() uint16 {
	if s != nil && s.primary {
		return primSrcSend
	}
	return wlDataSrcSend
}

func (s *dataSource) cancelledOp() uint16 {
	if s != nil && s.primary {
		return primSrcCancelled
	}
	return wlDataSrcCancelled
}

func isPlainText(m string) bool {
	return clipbridge.IsPlainText(m)
}

func matchMime(offered []string, want string) string {
	for _, m := range offered {
		if m == want {
			return m
		}
	}
	if isPlainText(want) {
		for _, m := range offered {
			if isPlainText(m) {
				return m
			}
		}
	}
	return ""
}

func (c *Client) reqDataDevMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // create_data_source
		id, err := cur.U32()
		if err != nil {
			return err
		}
		src := &dataSource{id: id, client: c}
		c.objs[id] = &object{id: id, kind: kindDataSource, src: src}
	case 1: // get_data_device(id, seat)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32()
		c.objs[id] = &object{id: id, kind: kindDataDevice}
		if c.dataDev == 0 {
			c.dataDev = id
		}
		c.sendSelection(false)
	}
	return nil
}

func (c *Client) reqDataSource(o *object, op uint16, cur *wayland.Cursor) error {
	src := o.src
	if src == nil {
		return nil
	}
	switch op {
	case 0: // offer
		m, err := cur.String()
		if err != nil {
			return err
		}
		src.mimes = append(src.mimes, m)
	case 1: // destroy
		if c.srv != nil {
			c.srv.clearIfCurrent(src)
		}
		delete(c.objs, o.id)
	}
	return nil
}

func (c *Client) reqDataDevice(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 1: // set_selection(source, serial)
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32()
		var src *dataSource
		if sid != 0 {
			if so := c.objs[sid]; so != nil {
				src = so.src
			}
		}
		if c.srv != nil {
			c.srv.setSelection(false, c, src)
		}
	case 2: // release
		if c.dataDev == o.id {
			c.dataDev = 0
		}
		delete(c.objs, o.id)
	}
	return nil
}

func (c *Client) reqDataOffer(o *object, op uint16, cur *wayland.Cursor) error {
	recv, dest := uint16(1), uint16(2) // wl_data_offer
	if o.offer != nil && o.offer.prim {
		recv, dest = 0, 1 // zwp_primary_selection_offer_v1
	}
	switch op {
	case recv: // receive(mime, fd)
		mime, err := cur.String()
		if err != nil {
			return err
		}
		fd, err := cur.FD()
		if err != nil {
			return err
		}
		if c.srv != nil {
			c.srv.transfer(o.offer, mime, fd)
		} else if fd > 0 {
			_ = syscall.Close(fd)
		}
	case dest: // destroy
		delete(c.objs, o.id)
	}
	return nil
}

func (c *Client) reqPrimaryMgr(_ *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // create_source
		id, err := cur.U32()
		if err != nil {
			return err
		}
		src := &dataSource{id: id, client: c, primary: true}
		c.objs[id] = &object{id: id, kind: kindPrimSource, src: src}
	case 1: // get_device(new_id, seat)
		id, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32()
		c.objs[id] = &object{id: id, kind: kindPrimDevice}
		if c.primDev == 0 {
			c.primDev = id
		}
		c.sendSelection(true)
	case 2: // destroy
	}
	return nil
}

func (c *Client) reqPrimSource(o *object, op uint16, cur *wayland.Cursor) error {
	src := o.src
	if src == nil {
		return nil
	}
	switch op {
	case 0: // offer
		m, err := cur.String()
		if err != nil {
			return err
		}
		src.mimes = append(src.mimes, m)
	case 1: // destroy
		if c.srv != nil {
			c.srv.clearIfCurrent(src)
		}
		delete(c.objs, o.id)
	}
	return nil
}

func (c *Client) reqPrimDevice(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // set_selection(source, serial)
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32()
		var src *dataSource
		if sid != 0 {
			if so := c.objs[sid]; so != nil {
				src = so.src
			}
		}
		if c.srv != nil {
			c.srv.setSelection(true, c, src)
		}
	case 1: // destroy
		if c.primDev == o.id {
			c.primDev = 0
		}
		delete(c.objs, o.id)
	}
	return nil
}

func (s *Server) setSelection(primary bool, from *Client, src *dataSource) {
	if s == nil {
		return
	}
	s.mu.Lock()
	cur := s.clip
	if primary {
		cur = s.prim
	}
	old := cur
	var next *selection
	if src != nil {
		next = &selection{src: from, source: src}
	}
	if primary {
		s.prim = next
	} else {
		s.clip = next
	}
	s.mu.Unlock()
	if old != nil && old.source != nil && old.source != src && old.source.client != nil {
		_ = old.source.client.send(old.source.id, old.source.cancelledOp(), nil, nil)
	}
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.sendSelection(primary)
	}
	if s.clipExport != nil && src != nil && len(src.hostBytes) == 0 && clipbridge.PickPlainMime(src.mimes) != "" {
		s.clipExport(primary, src.mimes)
	}
}

// SetClipExport is called when a worldr client set_selection of text (not a host import).
func (s *Server) SetClipExport(fn func(primary bool, mimes []string)) {
	if s == nil {
		return
	}
	s.clipExport = fn
}

// ImportHostText installs a host-backed text selection (nest Plasma → worldr).
func (s *Server) ImportHostText(primary bool, text []byte) {
	if s == nil {
		return
	}
	if len(text) == 0 {
		s.setSelection(primary, nil, nil)
		return
	}
	if len(text) > clipbridge.MaxTextBytes {
		text = text[:clipbridge.MaxTextBytes]
	}
	src := &dataSource{
		mimes:     clipbridge.TextMimes(),
		primary:   primary,
		hostBytes: append([]byte(nil), text...),
	}
	s.setSelection(primary, nil, src)
}

// SendSelectionTo writes the current selection onto fd (host data_source.send).
func (s *Server) SendSelectionTo(primary bool, mime string, fd int) {
	if s == nil {
		if fd > 0 {
			_ = syscall.Close(fd)
		}
		return
	}
	s.mu.Lock()
	sel := s.clip
	if primary {
		sel = s.prim
	}
	s.mu.Unlock()
	if sel == nil || sel.source == nil {
		if fd > 0 {
			_ = syscall.Close(fd)
		}
		return
	}
	s.transfer(&dataOffer{source: sel.source, prim: primary}, mime, fd)
}

func (s *Server) clearIfCurrent(src *dataSource) {
	if s == nil || src == nil {
		return
	}
	s.mu.Lock()
	if s.clip != nil && s.clip.source == src {
		s.mu.Unlock()
		s.setSelection(false, nil, nil)
		return
	}
	if s.prim != nil && s.prim.source == src {
		s.mu.Unlock()
		s.setSelection(true, nil, nil)
		return
	}
	s.mu.Unlock()
}

func (s *Server) dropClientSelection(c *Client) {
	if s == nil || c == nil {
		return
	}
	s.mu.Lock()
	dropClip := s.clip != nil && s.clip.src == c
	dropPrim := s.prim != nil && s.prim.src == c
	s.mu.Unlock()
	if dropClip {
		s.setSelection(false, nil, nil)
	}
	if dropPrim {
		s.setSelection(true, nil, nil)
	}
}

func (s *Server) transfer(offer *dataOffer, mime string, fd int) {
	if fd <= 0 {
		return
	}
	if s == nil || offer == nil || offer.source == nil {
		_ = syscall.Close(fd)
		return
	}
	src := offer.source
	if len(src.hostBytes) > 0 {
		clipbridge.WriteText(fd, src.hostBytes)
		return
	}
	if src.client == nil {
		_ = syscall.Close(fd)
		return
	}
	use := matchMime(src.mimes, mime)
	if use == "" {
		_ = syscall.Close(fd)
		return
	}
	p := wayland.PutString(nil, mime)
	_ = src.client.send(src.id, src.sendOp(), p, []int{fd})
	_ = syscall.Close(fd)
}

func (c *Client) sendCurrentSelections() {
	c.sendSelection(false)
	c.sendSelection(true)
}

func (c *Client) sendSelection(primary bool) {
	if c == nil || c.srv == nil {
		return
	}
	dev := c.dataDev
	if primary {
		dev = c.primDev
	}
	if dev == 0 {
		return
	}
	c.srv.mu.Lock()
	sel := c.srv.clip
	if primary {
		sel = c.srv.prim
	}
	c.srv.mu.Unlock()
	offerEv, selEv := wlDataDevDataOffer, wlDataDevSelection
	if primary {
		offerEv, selEv = primDevDataOffer, primDevSelection
	}
	if sel == nil || sel.source == nil {
		_ = c.send(dev, selEv, wayland.PutU32(nil, 0), nil)
		return
	}
	oid := c.allocServerID()
	off := &dataOffer{id: oid, source: sel.source, prim: primary}
	kind := kindDataOffer
	c.objs[oid] = &object{id: oid, kind: kind, offer: off}
	_ = c.send(dev, offerEv, wayland.PutU32(nil, oid), nil)
	for _, m := range sel.source.mimes {
		_ = c.send(oid, wlDataOfferOffer, wayland.PutString(nil, m), nil)
	}
	_ = c.send(dev, selEv, wayland.PutU32(nil, oid), nil)
}
