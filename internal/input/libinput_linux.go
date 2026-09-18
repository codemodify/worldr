//go:build linux

package input

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/seat"
	"math"
)

type managedReader interface {
	Poll() ([]seat.InputEvent, error)
	Close() error
}

// OpenManaged uses libinput's udev discovery and seat-controlled device FDs.
// Closing it releases every input device before the host acknowledges disable.
func OpenManaged(session *seat.Session, w, h int) (*Pointer, error) {
	if w < 1 || h < 1 || w > 32768 || h > 32768 {
		return nil, errors.New("invalid input extent")
	}
	reader, err := session.OpenInput()
	if err != nil {
		return nil, err
	}
	return &Pointer{X: w / 2, Y: h / 2, W: w, H: h, managedX: float64(w / 2), managedY: float64(h / 2), managed: reader}, nil
}
func (p *Pointer) Err() error {
	if p == nil {
		return nil
	}
	return p.managedErr
}
func (p *Pointer) Resize(w, h int) {
	if p == nil || w < 1 || h < 1 {
		return
	}
	p.W, p.H = w, h
	p.X, p.Y = min(p.X, w-1), min(p.Y, h-1)
	p.managedX, p.managedY = math.Min(p.managedX, float64(w-1)), math.Min(p.managedY, float64(h-1))
}
func (p *Pointer) pollManaged() {
	p.Events = p.Events[:0]
	if p.managedErr != nil {
		return
	}
	events, err := p.managed.Poll()
	if err != nil {
		p.managedErr = err
		p.Events = append(p.Events, Event{Kind: Cancel, X: p.X, Y: p.Y, Time: p.lastTime})
		return
	}
	for _, raw := range events {
		if math.IsNaN(raw.X) || math.IsNaN(raw.Y) || math.IsInf(raw.X, 0) || math.IsInf(raw.Y, 0) {
			continue
		}
		event := Event{Time: raw.Time, Code: raw.Code, Pressed: raw.Pressed}
		switch raw.Kind {
		case seat.Motion:
			p.managedX += raw.X
			p.managedY += raw.Y
			event.Kind = Move
		case seat.Absolute:
			p.managedX = raw.X * float64(max(0, p.W-1))
			p.managedY = raw.Y * float64(max(0, p.H-1))
			event.Kind = Move
		case seat.Button:
			event.Kind = Up
			if raw.Pressed {
				event.Kind = Down
			}
		case seat.Key:
			event.Kind = KeyInput
		case seat.Scroll:
			event.Kind = Scroll
			event.ScrollX = float32(raw.X)
			event.ScrollY = float32(raw.Y)
		case seat.Cancel:
			event.Kind = Cancel
		default:
			continue
		}
		p.managedX = math.Max(0, math.Min(float64(max(0, p.W-1)), p.managedX))
		p.managedY = math.Max(0, math.Min(float64(max(0, p.H-1)), p.managedY))
		p.X, p.Y = int(math.Round(p.managedX)), int(math.Round(p.managedY))
		event.X, event.Y = p.X, p.Y
		p.lastTime = raw.Time
		p.Events = append(p.Events, event)
	}
}
