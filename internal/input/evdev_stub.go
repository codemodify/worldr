//go:build !linux

package input

import (
	"errors"
	"github.com/codemodify/worldr/internal/platform/linux/seat"
)

type Pointer struct {
	X, Y, W, H int
	Events     []Event
}

func Open(w, h int) *Pointer { return &Pointer{X: w / 2, Y: h / 2, W: w, H: h} }
func (p *Pointer) Close()    { p.Events = nil }
func (p *Pointer) Poll()     { p.Events = p.Events[:0] }
func OpenManaged(*seat.Session, int, int) (*Pointer, error) {
	return nil, errors.New("managed input requires Linux")
}
func (*Pointer) Err() error        { return nil }
func (p *Pointer) Resize(w, h int) { p.W, p.H = w, h }
