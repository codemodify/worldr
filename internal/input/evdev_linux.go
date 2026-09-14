//go:build linux

// Package input reads a relative pointer and a few keys from evdev (Phase 3).
package input

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"time"
)

const (
	evSyn = 0
	evKey = 1
	evRel = 2
	evAbs = 3

	relX = 0
	relY = 1

	btnLeft = 0x110
	keyEsc  = 1
	keyQ    = 16
)

type evdevEvent struct {
	Sec   uint64
	Usec  uint64
	Type  uint16
	Code  uint16
	Value int32
}

// Key is one evdev key edge this poll.
type Key struct {
	Code    uint32
	Pressed bool
}

// Pointer is a software cursor plus quit request.
type Pointer struct {
	X, Y    int
	W, H    int
	Click   bool
	Release bool
	Quit    bool
	Keys    []Key
	files   []*os.File
}

// Open scans /dev/input/event* (best-effort; missing nodes are OK).
func Open(w, h int) *Pointer {
	p := &Pointer{X: w / 2, Y: h / 2, W: w, H: h}
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, m := range matches {
		f, err := os.Open(m)
		if err != nil {
			continue
		}
		_ = f.SetReadDeadline(time.Time{})
		p.files = append(p.files, f)
	}
	return p
}

func (p *Pointer) Close() {
	for _, f := range p.files {
		_ = f.Close()
	}
	p.files = nil
}

// Poll applies pending evdev events. Click/Release/Quit are edge-triggered.
func (p *Pointer) Poll() {
	p.Click = false
	p.Release = false
	p.Keys = p.Keys[:0]
	buf := make([]byte, 24)
	for _, f := range p.files {
		for i := 0; i < 32; i++ {
			_ = f.SetReadDeadline(time.Now().Add(50 * time.Microsecond))
			n, err := f.Read(buf)
			if err != nil || n < 24 {
				break
			}
			typ := binary.LittleEndian.Uint16(buf[16:18])
			code := binary.LittleEndian.Uint16(buf[18:20])
			val := int32(binary.LittleEndian.Uint32(buf[20:24]))
			switch typ {
			case evRel:
				if code == relX {
					p.X += int(val)
				} else if code == relY {
					p.Y += int(val)
				}
			case evKey:
				if code == btnLeft && val == 1 {
					p.Click = true
				}
				if code == btnLeft && val == 0 {
					p.Release = true
				}
				if code < 0x100 && (val == 0 || val == 1) {
					p.Keys = append(p.Keys, Key{Code: uint32(code), Pressed: val == 1})
				}
				if (code == keyEsc || code == keyQ) && val == 1 {
					p.Quit = true
				}
			}
		}
	}
	if p.X < 0 {
		p.X = 0
	}
	if p.Y < 0 {
		p.Y = 0
	}
	if p.W > 0 && p.X >= p.W {
		p.X = p.W - 1
	}
	if p.H > 0 && p.Y >= p.H {
		p.Y = p.H - 1
	}
}
