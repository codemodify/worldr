package xwayland

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// XWM is a tiny ICCCM/EWMH window manager: SubstructureRedirect on the root,
// MapRequest → MapWindow, ConfigureRequest → ConfigureWindow, plus the
// _NET_WM bits common X11 clients probe (supported atoms, active window,
// WM_NAME/WM_CLASS, delete/take-focus, override-redirect / transients).
type XWM struct {
	xc        *xConn
	atoms     xAtoms
	check     uint32
	popup     map[uint32]struct{}
	mu        sync.Mutex
	wins      map[uint32]*xWin
	pending   []*xWin
	changes   []SurfaceHints
	activates []uint32
	once      sync.Once
	done      chan struct{}

	// OnChange is invoked when title/class/chrome/geometry is known or updates.
	OnChange func(SurfaceHints)
	// OnActivate is invoked when a client asks to be focused/raised.
	OnActivate func(win uint32)
}

type xWin struct {
	id           uint32
	x, y, w, h   int
	title        string
	instance     string
	class        string
	override     bool
	transient    bool
	transientFor uint32
	types        []uint32
	protocols    []uint32
}

type xAtoms struct {
	wmState, allowCommits                    uint32
	wmName, wmClass, wmProtocols             uint32
	wmDelete, wmTakeFocus, wmTransient, utf8 uint32
	netSupported, netCheck, netWMName        uint32
	netActive, netClientList, netClientStack uint32
	netWMType, typeNormal, typeDialog        uint32
	typeUtility, typeMenu, typeDropdown      uint32
	typePopup, typeTooltip, typeNotif        uint32
	typeCombo, typeDnd, typeSplash           uint32
	netWMState, netClose                     uint32
}

func (a xAtoms) popupMap() map[uint32]struct{} {
	m := map[uint32]struct{}{}
	for _, id := range []uint32{
		a.typeMenu, a.typeDropdown, a.typePopup, a.typeTooltip,
		a.typeCombo, a.typeDnd, a.typeSplash, a.typeNotif,
	} {
		if id != 0 {
			m[id] = struct{}{}
		}
	}
	return m
}

func (a xAtoms) supported() []uint32 {
	out := make([]uint32, 0, 24)
	for _, id := range []uint32{
		a.netSupported, a.netCheck, a.netWMName, a.netActive,
		a.netClientList, a.netClientStack, a.netWMType, a.typeNormal,
		a.typeDialog, a.typeUtility, a.typeMenu, a.typeDropdown,
		a.typePopup, a.typeTooltip, a.typeNotif, a.typeCombo,
		a.typeDnd, a.typeSplash, a.netWMState, a.netClose,
	} {
		if id != 0 {
			out = append(out, id)
		}
	}
	return out
}

func (xw *xWin) hints(popup map[uint32]struct{}) SurfaceHints {
	app := xw.class
	if app == "" {
		app = xw.instance
	}
	title := xw.title
	if title == "" {
		title = app
	}
	return SurfaceHints{
		Win:          xw.id,
		TransientFor: xw.transientFor,
		Title:        title,
		AppID:        app,
		NoChrome:     NoChromeFor(xw.override, xw.transient, xw.types, popup),
		X:            xw.x,
		Y:            xw.y,
		W:            xw.w,
		H:            xw.h,
	}
}

// StartXWM connects to DISPLAY :displayNum and takes the WM role.
func StartXWM(displayNum int, pump func()) (*XWM, error) {
	var xc *xConn
	var err error
	deadline := time.Now().Add(4 * time.Second)
	for {
		if pump != nil {
			pump()
		}
		xc, err = x11Connect(displayNum, 300*time.Millisecond)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("XWM connect :%d: %w", displayNum, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	atoms, check, err := takeWM(xc)
	if err != nil {
		xc.Close()
		return nil, err
	}
	return newXWM(xc, atoms, check), nil
}

// StartXWMConn takes the WM role on an already-connected X11 socket (-wm fd).
func StartXWMConn(c net.Conn) (*XWM, error) {
	xc, err := x11ConnectConn(c)
	if err != nil {
		return nil, err
	}
	atoms, check, err := takeWM(xc)
	if err != nil {
		xc.Close()
		return nil, err
	}
	return newXWM(xc, atoms, check), nil
}

func takeWM(xc *xConn) (xAtoms, uint32, error) {
	atoms, err := internEWMH(xc)
	if err != nil {
		return atoms, 0, err
	}
	xc.wmState = atoms.wmState
	xc.allowCommits = atoms.allowCommits
	check, err := xc.createInputOutput(xc.root, 1, 1)
	if err != nil {
		return atoms, 0, fmt.Errorf("XWM supporting window: %w", err)
	}
	if err := xc.changeProp32(xc.root, atoms.netCheck, xAtomWindow, check); err != nil {
		return atoms, 0, fmt.Errorf("XWM _NET_SUPPORTING_WM_CHECK: %w", err)
	}
	_ = xc.changeProp32(check, atoms.netCheck, xAtomWindow, check)
	_ = xc.changeProp8(check, atoms.netWMName, atoms.utf8, []byte("worldr"))
	if err := xc.changeProp32(xc.root, atoms.netSupported, xAtomAtom, atoms.supported()...); err != nil {
		return atoms, 0, fmt.Errorf("XWM _NET_SUPPORTED: %w", err)
	}
	_ = xc.changeProp32(xc.root, atoms.netClientList, xAtomWindow)
	_ = xc.changeProp32(xc.root, atoms.netClientStack, xAtomWindow)
	_ = xc.changeProp32(xc.root, atoms.netActive, xAtomWindow, 0)
	if err := xc.compositeRedirectSubwindows(); err != nil {
		return atoms, 0, fmt.Errorf("XWM CompositeRedirectSubwindows: %w", err)
	}
	if err := xc.selectSubstructure(); err != nil {
		return atoms, 0, fmt.Errorf("XWM SubstructureRedirect: %w", err)
	}
	return atoms, check, nil
}

func internEWMH(xc *xConn) (xAtoms, error) {
	var a xAtoms
	pairs := []struct {
		dst  *uint32
		name string
	}{
		{&a.wmState, "WM_STATE"},
		{&a.allowCommits, "_XWAYLAND_ALLOW_COMMITS"},
		{&a.wmName, "WM_NAME"},
		{&a.wmClass, "WM_CLASS"},
		{&a.wmProtocols, "WM_PROTOCOLS"},
		{&a.wmDelete, "WM_DELETE_WINDOW"},
		{&a.wmTakeFocus, "WM_TAKE_FOCUS"},
		{&a.wmTransient, "WM_TRANSIENT_FOR"},
		{&a.utf8, "UTF8_STRING"},
		{&a.netSupported, "_NET_SUPPORTED"},
		{&a.netCheck, "_NET_SUPPORTING_WM_CHECK"},
		{&a.netWMName, "_NET_WM_NAME"},
		{&a.netActive, "_NET_ACTIVE_WINDOW"},
		{&a.netClientList, "_NET_CLIENT_LIST"},
		{&a.netClientStack, "_NET_CLIENT_LIST_STACKING"},
		{&a.netWMType, "_NET_WM_WINDOW_TYPE"},
		{&a.typeNormal, "_NET_WM_WINDOW_TYPE_NORMAL"},
		{&a.typeDialog, "_NET_WM_WINDOW_TYPE_DIALOG"},
		{&a.typeUtility, "_NET_WM_WINDOW_TYPE_UTILITY"},
		{&a.typeMenu, "_NET_WM_WINDOW_TYPE_MENU"},
		{&a.typeDropdown, "_NET_WM_WINDOW_TYPE_DROPDOWN_MENU"},
		{&a.typePopup, "_NET_WM_WINDOW_TYPE_POPUP_MENU"},
		{&a.typeTooltip, "_NET_WM_WINDOW_TYPE_TOOLTIP"},
		{&a.typeNotif, "_NET_WM_WINDOW_TYPE_NOTIFICATION"},
		{&a.typeCombo, "_NET_WM_WINDOW_TYPE_COMBO"},
		{&a.typeDnd, "_NET_WM_WINDOW_TYPE_DND"},
		{&a.typeSplash, "_NET_WM_WINDOW_TYPE_SPLASH"},
		{&a.netWMState, "_NET_WM_STATE"},
		{&a.netClose, "_NET_CLOSE_WINDOW"},
	}
	for _, p := range pairs {
		id, err := xc.internAtom(p.name)
		if err != nil {
			return a, fmt.Errorf("InternAtom %s: %w", p.name, err)
		}
		*p.dst = id
	}
	return a, nil
}

func newXWM(xc *xConn, atoms xAtoms, check uint32) *XWM {
	wm := &XWM{
		xc:    xc,
		atoms: atoms,
		check: check,
		popup: atoms.popupMap(),
		wins:  make(map[uint32]*xWin),
		done:  make(chan struct{}),
	}
	go wm.loop()
	return wm
}

func (w *XWM) loop() {
	defer close(w.done)
	for {
		kind, ev, err := w.xc.readEvent()
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "xwayland: XWM read: %v\n", err)
			}
			return
		}
		switch kind {
		case xEvError:
			if len(ev) > 1 {
				fmt.Fprintf(os.Stderr, "xwayland: XWM X11 error code=%d\n", ev[1])
			}
		case xEvMapRequest:
			if len(ev) < 12 {
				continue
			}
			w.handleMapRequest(binary.LittleEndian.Uint32(ev[8:]))
		case xEvMapNotify:
			w.handleMapNotify(ev)
		case xEvConfigureRequest:
			w.handleConfigureRequest(ev)
		case xEvConfigureNotify:
			w.handleConfigureNotify(ev)
		case xEvPropertyNotify:
			w.handlePropertyNotify(ev)
		case xEvClientMessage:
			w.handleClientMessage(ev)
		case xEvUnmapNotify, xEvDestroyNotify:
			if len(ev) >= 12 {
				w.forget(binary.LittleEndian.Uint32(ev[8:]))
			}
		}
	}
}

func (w *XWM) handleMapRequest(win uint32) {
	xw := w.ensure(win)
	w.refresh(xw)
	_ = w.xc.setAllowCommits(win)
	_ = w.xc.setWMStateNormal(win)
	_ = w.xc.selectWindowEvents(win)
	if err := w.xc.mapWindow(win); err != nil {
		fmt.Fprintf(os.Stderr, "xwayland: XWM MapWindow 0x%x: %v\n", win, err)
		return
	}
	w.queuePending(xw)
	w.publishClientList()
	w.emit(xw)
	fmt.Fprintf(os.Stderr, "xwayland: XWM mapped 0x%x title=%q class=%q chrome=%v\n",
		win, xw.title, xw.class, !NoChromeFor(xw.override, xw.transient, xw.types, w.popup))
}

func (w *XWM) handleMapNotify(ev []byte) {
	if len(ev) < 12 {
		return
	}
	override := ev[1] != 0
	win := binary.LittleEndian.Uint32(ev[8:])
	if !override {
		return
	}
	xw := w.ensure(win)
	w.refresh(xw)
	w.mu.Lock()
	xw.override = true
	w.mu.Unlock()
	w.queuePending(xw)
	w.emit(xw)
}

func (w *XWM) handleConfigureRequest(ev []byte) {
	if err := w.xc.configureFromRequest(ev); err != nil {
		fmt.Fprintf(os.Stderr, "xwayland: XWM ConfigureWindow: %v\n", err)
		return
	}
	if len(ev) < 28 {
		return
	}
	win := binary.LittleEndian.Uint32(ev[8:])
	mask := binary.LittleEndian.Uint16(ev[26:])
	xw := w.lookup(win)
	if xw != nil {
		w.mu.Lock()
		if mask&xCfgX != 0 {
			xw.x = int(int16(binary.LittleEndian.Uint16(ev[16:])))
		}
		if mask&xCfgY != 0 {
			xw.y = int(int16(binary.LittleEndian.Uint16(ev[18:])))
		}
		if mask&xCfgWidth != 0 {
			xw.w = int(binary.LittleEndian.Uint16(ev[20:]))
		}
		if mask&xCfgHeight != 0 {
			xw.h = int(binary.LittleEndian.Uint16(ev[22:]))
		}
		or, trans := xw.override, xw.transient
		w.mu.Unlock()
		if or || trans {
			w.emit(xw)
		}
	}
	if mask&xCfgStackMode != 0 && ev[1] == xStackAbove {
		w.FocusWindow(win)
		w.requestActivate(win)
	}
}

func (w *XWM) handleConfigureNotify(ev []byte) {
	if len(ev) < 24 {
		return
	}
	win := binary.LittleEndian.Uint32(ev[8:])
	xw := w.lookup(win)
	if xw == nil {
		return
	}
	w.mu.Lock()
	if !(xw.override || xw.transient) {
		w.mu.Unlock()
		return
	}
	xw.x = int(int16(binary.LittleEndian.Uint16(ev[16:])))
	xw.y = int(int16(binary.LittleEndian.Uint16(ev[18:])))
	xw.w = int(binary.LittleEndian.Uint16(ev[20:]))
	xw.h = int(binary.LittleEndian.Uint16(ev[22:]))
	w.mu.Unlock()
	w.emit(xw)
}

func (w *XWM) handlePropertyNotify(ev []byte) {
	if len(ev) < 12 {
		return
	}
	win := binary.LittleEndian.Uint32(ev[4:])
	xw := w.lookup(win)
	if xw == nil {
		return
	}
	w.refresh(xw)
	w.emit(xw)
}

func (w *XWM) handleClientMessage(ev []byte) {
	if len(ev) < 16 {
		return
	}
	win := binary.LittleEndian.Uint32(ev[4:])
	typ := binary.LittleEndian.Uint32(ev[8:])
	switch typ {
	case w.atoms.netActive:
		w.FocusWindow(win)
		w.requestActivate(win)
	case w.atoms.netClose:
		w.DeleteWindow(win)
	}
}

func (w *XWM) ensure(id uint32) *xWin {
	w.mu.Lock()
	defer w.mu.Unlock()
	if x := w.wins[id]; x != nil {
		return x
	}
	x := &xWin{id: id}
	w.wins[id] = x
	return x
}

func (w *XWM) lookup(id uint32) *xWin {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wins[id]
}

func (w *XWM) queuePending(xw *xWin) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.pending {
		if p.id == xw.id {
			return
		}
	}
	w.pending = append(w.pending, xw)
}

func (w *XWM) forget(id uint32) {
	w.mu.Lock()
	delete(w.wins, id)
	dst := w.pending[:0]
	for _, p := range w.pending {
		if p.id != id {
			dst = append(dst, p)
		}
	}
	w.pending = dst
	w.mu.Unlock()
	w.publishClientList()
}

func (w *XWM) refresh(xw *xWin) {
	or, orErr := w.xc.getOverrideRedirect(xw.id)
	x, y, ww, hh, geomErr := w.xc.getGeometry(xw.id)
	var netName, icccmName, classVal, transVal, protoVal, typeVal []byte
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.netWMName); err == nil {
		netName = val
	}
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.wmName); err == nil {
		icccmName = val
	}
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.wmClass); err == nil {
		classVal = val
	}
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.wmTransient); err == nil {
		transVal = val
	}
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.wmProtocols); err == nil {
		protoVal = val
	}
	if _, _, val, err := w.xc.getProperty(xw.id, w.atoms.netWMType); err == nil {
		typeVal = val
	}
	w.mu.Lock()
	if orErr == nil {
		xw.override = or
	}
	if geomErr == nil {
		xw.x, xw.y, xw.w, xw.h = x, y, ww, hh
	}
	if t := ParseTitle(netName, icccmName); t != "" {
		xw.title = t
	}
	if len(classVal) > 0 {
		xw.instance, xw.class = ParseWMClass(classVal)
	}
	if len(transVal) >= 4 {
		xw.transientFor = binary.LittleEndian.Uint32(transVal)
		xw.transient = xw.transientFor != 0
	}
	if protoVal != nil {
		xw.protocols = parseAtoms32(protoVal)
	}
	if typeVal != nil {
		xw.types = parseAtoms32(typeVal)
	}
	w.mu.Unlock()
}

func (w *XWM) emit(xw *xWin) {
	w.mu.Lock()
	h := xw.hints(w.popup)
	w.changes = append(w.changes, h)
	w.mu.Unlock()
	if w.OnChange != nil {
		w.OnChange(h)
	}
}

func (w *XWM) requestActivate(win uint32) {
	if win == 0 {
		return
	}
	w.mu.Lock()
	w.activates = append(w.activates, win)
	w.mu.Unlock()
	if w.OnActivate != nil {
		w.OnActivate(win)
	}
}

// Drain returns queued property updates and activate requests for the frame loop.
func (w *XWM) Drain() (changes []SurfaceHints, activates []uint32) {
	if w == nil {
		return nil, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	changes = w.changes
	activates = w.activates
	w.changes = nil
	w.activates = nil
	return
}

func (w *XWM) publishClientList() {
	w.mu.Lock()
	ids := make([]uint32, 0, len(w.wins))
	for id, xw := range w.wins {
		if xw != nil && !xw.override {
			ids = append(ids, id)
		}
	}
	w.mu.Unlock()
	if w.atoms.netClientList != 0 {
		_ = w.xc.changeProp32(w.xc.root, w.atoms.netClientList, xAtomWindow, ids...)
	}
	if w.atoms.netClientStack != 0 {
		_ = w.xc.changeProp32(w.xc.root, w.atoms.netClientStack, xAtomWindow, ids...)
	}
}

// ConsumeMap returns hints for a newly mapped xwayland_shell surface.
func (w *XWM) ConsumeMap(width, height int) (SurfaceHints, bool) {
	if w == nil {
		return SurfaceHints{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	picked, rest := pickPending(w.pending, width, height)
	w.pending = rest
	if picked == nil {
		return SurfaceHints{}, false
	}
	return picked.hints(w.popup), true
}

// FocusWindow sets X input focus, optional WM_TAKE_FOCUS, and _NET_ACTIVE_WINDOW.
func (w *XWM) FocusWindow(win uint32) {
	if w == nil || win == 0 {
		return
	}
	w.mu.Lock()
	xw := w.wins[win]
	w.mu.Unlock()
	_ = w.xc.setInputFocus(win)
	if xw != nil && hasAtom(xw.protocols, w.atoms.wmTakeFocus) {
		_ = w.xc.sendClientMessage(win, w.atoms.wmProtocols, [5]uint32{w.atoms.wmTakeFocus, 0})
	}
	if w.atoms.netActive != 0 {
		_ = w.xc.changeProp32(w.xc.root, w.atoms.netActive, xAtomWindow, win)
	}
}

// DeleteWindow sends WM_DELETE_WINDOW when the client listed it in WM_PROTOCOLS.
func (w *XWM) DeleteWindow(win uint32) {
	if w == nil || win == 0 {
		return
	}
	w.mu.Lock()
	xw := w.wins[win]
	w.mu.Unlock()
	if xw == nil || !hasAtom(xw.protocols, w.atoms.wmDelete) {
		return
	}
	_ = w.xc.sendClientMessage(win, w.atoms.wmProtocols, [5]uint32{w.atoms.wmDelete, 0})
}

// Close drops the X11 WM connection (Xwayland keeps running until killed).
func (w *XWM) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		w.xc.Close()
		select {
		case <-w.done:
		case <-time.After(time.Second):
		}
	})
}
