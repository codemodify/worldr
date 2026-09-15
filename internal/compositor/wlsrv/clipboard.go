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
	wlDataDevEnter     uint16 = 1
	wlDataDevLeave     uint16 = 2
	wlDataDevMotion    uint16 = 3
	wlDataDevDrop      uint16 = 4
	wlDataDevSelection uint16 = 5
	wlDataOfferOffer   uint16 = 0
	wlDataOfferSrcActs uint16 = 1
	wlDataOfferAction  uint16 = 2
	wlDataSrcSend      uint16 = 1
	wlDataSrcCancelled uint16 = 2
	wlDataSrcDndDrop   uint16 = 3
	wlDataSrcDndDone   uint16 = 4
	wlDataSrcAction    uint16 = 5
	dndActionCopy      uint32 = 1
	primDevDataOffer   uint16 = 0
	primDevSelection   uint16 = 1
	primSrcSend        uint16 = 0
	primSrcCancelled   uint16 = 1
)

type dataSource struct {
	id        uint32
	client    *Client
	mimes     []string
	primary   bool
	hostBytes []byte            // nest text import (compat)
	hostParts map[string][]byte // nest import by MIME (text + image)
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
	if c := clipbridge.CanonicalImage(want); c != "" {
		for _, m := range offered {
			if clipbridge.CanonicalImage(m) == c {
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
		// Spec: selection is sent immediately before keyboard focus, or
		// when the selection changes while focused — not on get_data_device.
		// Qt6 calls platformIntegration()->clipboard() from that event;
		// emitting it during the init roundtrip SEGVs (ark / Brave shim).
		c.sendSelectionIfFocused(false)
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
			c.srv.cancelDragIfSource(src)
		}
		delete(c.objs, o.id)
	case 2: // set_actions (v3) — copy only
	}
	return nil
}

func (c *Client) reqDataDevice(o *object, op uint16, cur *wayland.Cursor) error {
	switch op {
	case 0: // start_drag(source, origin, icon, serial)
		sid, err := cur.U32()
		if err != nil {
			return err
		}
		_, _ = cur.U32() // origin surface
		_, _ = cur.U32() // icon
		_, _ = cur.U32() // serial
		var src *dataSource
		if sid != 0 {
			if so := c.objs[sid]; so != nil {
				src = so.src
			}
		}
		if c.srv != nil && src != nil {
			c.srv.startDrag(c, src)
		}
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
	case 0: // accept (clipboard unused; dnd target ack)
		if o.offer != nil && o.offer.prim {
			// zwp_primary receive is opcode 0
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
		}
	case recv: // receive(mime, fd) — wl_data_offer
		if o.offer != nil && o.offer.prim {
			break
		}
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
	case 3: // finish (dnd)
		if c.srv != nil {
			c.srv.finishDrag(o.offer)
		}
	case 4: // set_actions
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
		c.sendSelectionIfFocused(true)
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
		c.sendSelectionIfFocused(primary)
	}
	if s.clipExport == nil {
		return
	}
	if src != nil && !src.isHost() && clipbridge.Bridgeable(src.mimes) {
		s.clipExport(primary, src.mimes)
		return
	}
	// Worldr client cleared the selection — drop it on the nest host too.
	if src == nil && old != nil && old.source != nil && !old.source.isHost() {
		s.clipExport(primary, nil)
	}
}

func (src *dataSource) isHost() bool {
	return src != nil && (len(src.hostBytes) > 0 || len(src.hostParts) > 0)
}

func (src *dataSource) addHost(mime string, data []byte) {
	if src == nil {
		return
	}
	if src.hostParts == nil {
		src.hostParts = map[string][]byte{}
	}
	src.hostParts[mime] = data
	if clipbridge.IsPlainText(mime) {
		src.hostBytes = data
		for _, m := range clipbridge.TextMimes() {
			if matchMime(src.mimes, m) == "" {
				src.mimes = append(src.mimes, m)
			}
		}
		return
	}
	if matchMime(src.mimes, mime) == "" {
		src.mimes = append(src.mimes, mime)
	}
}

func (src *dataSource) hostPayload(mime string) []byte {
	if src == nil {
		return nil
	}
	if src.hostParts != nil {
		if b, ok := src.hostParts[mime]; ok {
			return b
		}
		if use := matchMime(keysOf(src.hostParts), mime); use != "" {
			return src.hostParts[use]
		}
	}
	if len(src.hostBytes) > 0 && (mime == "" || isPlainText(mime)) {
		return src.hostBytes
	}
	return nil
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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
	s.ImportHostPayload(primary, mimeTextPlain, text)
}

// ImportHostPayload installs or merges a host-backed MIME (text or image).
func (s *Server) ImportHostPayload(primary bool, mime string, data []byte) {
	if s == nil {
		return
	}
	if len(data) == 0 && clipbridge.IsPlainText(mime) {
		s.setSelection(primary, nil, nil)
		return
	}
	if len(data) == 0 {
		return
	}
	capn := clipbridge.CapFor(mime)
	if len(data) > capn {
		data = data[:capn]
	}
	data = append([]byte(nil), data...)
	s.mu.Lock()
	cur := s.clip
	if primary {
		cur = s.prim
	}
	if cur != nil && cur.source != nil && cur.src == nil {
		cur.source.addHost(mime, data)
		cl := append([]*Client(nil), s.clients...)
		s.mu.Unlock()
		for _, c := range cl {
			c.sendSelectionIfFocused(primary)
		}
		return
	}
	s.mu.Unlock()
	src := &dataSource{primary: primary}
	src.addHost(mime, data)
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
	if b := src.hostPayload(mime); len(b) > 0 {
		clipbridge.WriteBytes(fd, b, clipbridge.CapFor(mime))
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

// hasKeyboardFocus is true after wl_keyboard.enter for a live surface.
// get_data_device during Qt/Chromium platform init has a device but no enter.
func (c *Client) hasKeyboardFocus() bool {
	return c != nil && c.kbdID != 0 && c.kbdSurf != 0
}

func (c *Client) sendSelectionIfFocused(primary bool) {
	if c.hasKeyboardFocus() {
		c.sendSelection(primary)
	}
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
