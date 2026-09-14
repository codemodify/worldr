//go:build !linux

package input

type Pointer struct {
	X, Y           int
	W, H           int
	Click, Release bool
	Quit           bool
}

func Open(w, h int) *Pointer { return &Pointer{X: w / 2, Y: h / 2, W: w, H: h} }
func (p *Pointer) Close()    {}
func (p *Pointer) Poll()     {}
