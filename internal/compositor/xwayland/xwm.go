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

// XWM is a tiny ICCCM window manager: SubstructureRedirect on the root,
// MapRequest → MapWindow, ConfigureRequest → ConfigureWindow.
// Rootless Xwayland will not realize managed X11 windows (xeyes, xterm)
// as Wayland surfaces until something accepts those requests.
type XWM struct {
	xc   *xConn
	once sync.Once
	done chan struct{}
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
	if err := takeWM(xc); err != nil {
		xc.Close()
		return nil, err
	}
	return newXWM(xc), nil
}

// StartXWMConn takes the WM role on an already-connected X11 socket (-wm fd).
func StartXWMConn(c net.Conn) (*XWM, error) {
	xc, err := x11ConnectConn(c)
	if err != nil {
		return nil, err
	}
	if err := takeWM(xc); err != nil {
		xc.Close()
		return nil, err
	}
	return newXWM(xc), nil
}

func takeWM(xc *xConn) error {
	// Rootless Xwayland only creates a wl_surface when redirectDraw is
	// CompositeRedirectManual (ensure_surface_for_window).
	if err := xc.compositeRedirectSubwindows(); err != nil {
		return fmt.Errorf("XWM CompositeRedirectSubwindows: %w", err)
	}
	if err := xc.selectSubstructure(); err != nil {
		return fmt.Errorf("XWM SubstructureRedirect: %w", err)
	}
	return nil
}

func newXWM(xc *xConn) *XWM {
	wm := &XWM{xc: xc, done: make(chan struct{})}
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
			win := binary.LittleEndian.Uint32(ev[8:])
			_ = w.xc.setAllowCommits(win)
			_ = w.xc.setWMStateNormal(win)
			if err := w.xc.mapWindow(win); err != nil {
				fmt.Fprintf(os.Stderr, "xwayland: XWM MapWindow 0x%x: %v\n", win, err)
				return
			}
			fmt.Fprintf(os.Stderr, "xwayland: XWM mapped 0x%x\n", win)
		case xEvConfigureRequest:
			if err := w.xc.configureFromRequest(ev); err != nil {
				fmt.Fprintf(os.Stderr, "xwayland: XWM ConfigureWindow: %v\n", err)
				return
			}
		}
	}
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
