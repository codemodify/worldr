//go:build linux

package wlclient

import (
	"io"
	"os"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/clipbridge"
	"github.com/codemodify/worldr/internal/wayland"
)

const (
	wlDataDevOffer     uint16 = 0
	wlDataDevSelection uint16 = 5
	wlDataOfferOffer   uint16 = 0
	wlDataOfferRecv    uint16 = 1
	wlDataSrcOffer     uint16 = 0
	wlDataSrcDestroy   uint16 = 1
	wlDataSrcSend      uint16 = 1
	wlDataSrcCancelled uint16 = 2
	wlDataDevSetSel    uint16 = 1
	wlDataMgrCreateSrc uint16 = 0
	wlDataMgrGetDev    uint16 = 1

	primDevOffer     uint16 = 0
	primDevSelection uint16 = 1
	primDevSetSel    uint16 = 0
	primOfferRecv    uint16 = 0
	primSrcOffer     uint16 = 0
	primSrcDestroy   uint16 = 1
	primSrcSend      uint16 = 0
	primSrcCancelled uint16 = 1
	primMgrCreateSrc uint16 = 0
	primMgrGetDev    uint16 = 1
)

type hostOffer struct {
	id      uint32
	mimes   []string
	primary bool
}

func (w *Window) setupClip() error {
	if w.ddmgr != 0 && w.seat != 0 {
		w.dataDev = w.alloc()
		p := wayland.PutU32(nil, w.dataDev)
		p = wayland.PutU32(p, w.seat)
		if err := w.send(w.ddmgr, wlDataMgrGetDev, p, nil); err != nil {
			return err
		}
		logClient("data_device id=%d (host clipboard bridge)", w.dataDev)
	}
	if w.primmgr != 0 && w.seat != 0 {
		w.primDev = w.alloc()
		p := wayland.PutU32(nil, w.primDev)
		p = wayland.PutU32(p, w.seat)
		if err := w.send(w.primmgr, primMgrGetDev, p, nil); err != nil {
			return err
		}
		logClient("primary_device id=%d (host primary bridge)", w.primDev)
	}
	return nil
}

// HostClipBound is true when the nest client bound host wl_data_device_manager.
func (w *Window) HostClipBound() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ddmgr != 0 && w.dataDev != 0
}

// HostPrimaryBound is true when the nest client bound zwp_primary_selection.
func (w *Window) HostPrimaryBound() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.primmgr != 0 && w.primDev != 0
}

// SetClipImport receives a host MIME payload (Plasma → worldr).
func (w *Window) SetClipImport(fn func(primary bool, mime string, data []byte)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.onClipImport = fn
	w.mu.Unlock()
}

// SetClipFulfill writes worldr’s current selection onto a host send fd.
func (w *Window) SetClipFulfill(fn func(primary bool, mime string, fd int)) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.onClipFulfill = fn
	w.mu.Unlock()
}

// OfferHostText publishes worldr’s selection on the host data device
// (text/plain and image/png when the source offered them).
func (w *Window) OfferHostText(primary bool, mimes []string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	offer := clipbridge.HostOfferMimes(mimes)
	if len(offer) == 0 {
		return
	}
	if primary {
		w.offerHostLocked(true, w.primmgr, w.primDev, primMgrCreateSrc, primSrcOffer, primSrcDestroy, primDevSetSel, &w.hostPrimSrc, offer)
		return
	}
	w.offerHostLocked(false, w.ddmgr, w.dataDev, wlDataMgrCreateSrc, wlDataSrcOffer, wlDataSrcDestroy, wlDataDevSetSel, &w.hostSrc, offer)
}

func (w *Window) offerHostLocked(primary bool, mgr, dev uint32, createOp, offerOp, destroyOp, setOp uint16, srcID *uint32, mimes []string) {
	if mgr == 0 || dev == 0 {
		return
	}
	if *srcID != 0 {
		_ = w.send(*srcID, destroyOp, nil, nil)
		*srcID = 0
		w.ownHost.Set(primary, false)
	}
	id := w.alloc()
	if err := w.send(mgr, createOp, wayland.PutU32(nil, id), nil); err != nil {
		return
	}
	for _, m := range mimes {
		_ = w.send(id, offerOp, wayland.PutString(nil, m), nil)
	}
	p := wayland.PutU32(nil, id)
	p = wayland.PutU32(p, w.ptrSerial)
	if err := w.send(dev, setOp, p, nil); err != nil {
		return
	}
	*srcID = id
	w.ownHost.Set(primary, true)
	logClient("offered clipboard to host primary=%v source=%d mimes=%v", primary, id, mimes)
}

func (w *Window) handleClip(msg wayland.Message) error {
	if w.dataDev != 0 && msg.Object == w.dataDev {
		return w.handleDataDev(msg, false)
	}
	if w.primDev != 0 && msg.Object == w.primDev {
		return w.handleDataDev(msg, true)
	}
	if o := w.hostOffers[msg.Object]; o != nil && msg.Opcode == wlDataOfferOffer {
		m, err := wayland.NewCursor(msg.Payload, nil).String()
		if err == nil && m != "" {
			o.mimes = append(o.mimes, m)
		}
		return nil
	}
	if w.hostSrc != 0 && msg.Object == w.hostSrc {
		return w.handleHostSource(msg, false)
	}
	if w.hostPrimSrc != 0 && msg.Object == w.hostPrimSrc {
		return w.handleHostSource(msg, true)
	}
	return nil
}

func (w *Window) handleDataDev(msg wayland.Message, primary bool) error {
	offerOp, selOp := wlDataDevOffer, wlDataDevSelection
	if primary {
		offerOp, selOp = primDevOffer, primDevSelection
	}
	cur := wayland.NewCursor(msg.Payload, nil)
	switch msg.Opcode {
	case offerOp:
		id, err := cur.U32()
		if err != nil || id == 0 {
			return nil
		}
		if w.hostOffers == nil {
			w.hostOffers = map[uint32]*hostOffer{}
		}
		w.hostOffers[id] = &hostOffer{id: id, primary: primary}
	case selOp:
		id, _ := cur.U32()
		w.importHostSelection(primary, id)
	}
	return nil
}

func (w *Window) handleHostSource(msg wayland.Message, primary bool) error {
	sendOp, cancelOp := wlDataSrcSend, wlDataSrcCancelled
	if primary {
		sendOp, cancelOp = primSrcSend, primSrcCancelled
	}
	switch msg.Opcode {
	case sendOp:
		cur := wayland.NewCursor(msg.Payload, nil)
		mime, _ := cur.String()
		fd, err := w.rd.TakeFD()
		if err != nil || fd <= 0 {
			return nil
		}
		fn := w.onClipFulfill
		if fn == nil {
			_ = syscall.Close(fd)
			return nil
		}
		// Unlock is held by readLoop; fulfill on another socket/fd.
		go fn(primary, mime, fd)
	case cancelOp:
		if primary {
			w.hostPrimSrc = 0
		} else {
			w.hostSrc = 0
		}
		w.ownHost.Set(primary, false)
	}
	return nil
}

func (w *Window) importHostSelection(primary bool, offerID uint32) {
	if offerID == 0 {
		return
	}
	if w.ownHost.Owns(primary) {
		return
	}
	o := w.hostOffers[offerID]
	if o == nil {
		return
	}
	if w.wr == nil {
		return
	}
	var recvs []string
	if m := clipbridge.PickImageMime(o.mimes); m != "" {
		recvs = append(recvs, m)
	}
	if m := clipbridge.PickPlainMime(o.mimes); m != "" {
		recvs = append(recvs, m)
	}
	fn := w.onClipImport
	for _, mime := range recvs {
		w.receiveHost(offerID, primary, mime, fn)
	}
}

func (w *Window) receiveHost(offerID uint32, primary bool, mime string, fn func(bool, string, []byte)) {
	recvOp := wlDataOfferRecv
	if primary {
		recvOp = primOfferRecv
	}
	r, wr, err := os.Pipe()
	if err != nil {
		return
	}
	wfd, err := syscall.Dup(int(wr.Fd()))
	if err != nil {
		_ = r.Close()
		_ = wr.Close()
		return
	}
	_ = wr.Close()
	if err := w.send(offerID, recvOp, wayland.PutString(nil, mime), []int{wfd}); err != nil {
		_ = syscall.Close(wfd)
		_ = r.Close()
		return
	}
	go func() {
		defer r.Close()
		_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
		b, err := io.ReadAll(r)
		if err != nil || fn == nil {
			return
		}
		capn := clipbridge.CapFor(mime)
		if len(b) > capn {
			b = b[:capn]
		}
		fn(primary, mime, b)
	}()
}
