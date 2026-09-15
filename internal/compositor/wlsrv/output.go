package wlsrv

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxOutputs is the number of logical wl_output globals we advertise.
const MaxOutputs = 4

const globalOutput2 uint32 = 30 // extras after 17; 30–32 are outputs 1–3

// Output is one logical wl_output (tiled across the present framebuffer).
type Output struct {
	Name     string
	Desc     string
	X, Y     int
	W, H     int
	Scale120 uint32
}

// IntegerScale is the coherent wl_output.scale for this output.
func (o Output) IntegerScale() int32 {
	return IntegerScaleFrom120ths(o.Scale120)
}

// Contains is true when (px,py) is inside the output rect.
func (o Output) Contains(px, py int) bool {
	return px >= o.X && py >= o.Y && px < o.X+o.W && py < o.Y+o.H
}

// LayoutOutputs tiles n logical outputs left-to-right across screenW×screenH.
// n is clamped to 1..MaxOutputs. scales[i] (or the last / 1.0) sets each scale.
func LayoutOutputs(screenW, screenH, n int, scales []float64) []Output {
	if n < 1 {
		n = 1
	}
	if n > MaxOutputs {
		n = MaxOutputs
	}
	if screenW < n {
		screenW = n
	}
	if screenH < 1 {
		screenH = 1
	}
	out := make([]Output, n)
	base := screenW / n
	rem := screenW - base*n
	x := 0
	for i := 0; i < n; i++ {
		w := base
		if i == n-1 {
			w += rem
		}
		sc := 1.0
		if len(scales) > 0 {
			if i < len(scales) {
				sc = scales[i]
			} else {
				sc = scales[len(scales)-1]
			}
		}
		out[i] = Output{
			Name:     fmt.Sprintf("WL-%d", i+1),
			Desc:     fmt.Sprintf("worldr output %d", i+1),
			X:        x,
			Y:        0,
			W:        w,
			H:        screenH,
			Scale120: ScaleTo120ths(sc),
		}
		x += w
	}
	return out
}

// HitOutput returns the index containing (px,py), or 0 if none.
func HitOutput(outs []Output, px, py int) int {
	for i, o := range outs {
		if o.Contains(px, py) {
			return i
		}
	}
	if len(outs) == 0 {
		return 0
	}
	return 0
}

// ParseOutputScales reads a comma list ("1,1.5"). Empty → nil.
func ParseOutputScales(s string) ([]float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("output scale %q (need a positive number)", p)
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	if len(out) > MaxOutputs {
		out = out[:MaxOutputs]
	}
	return out, nil
}

func outputGlobal(i int) uint32 {
	if i <= 0 {
		return globalOutput
	}
	return globalOutput2 + uint32(i-1)
}

func outputIndexFromGlobal(name uint32) (int, bool) {
	if name == globalOutput {
		return 0, true
	}
	if name >= globalOutput2 && name < globalOutput2+uint32(MaxOutputs-1) {
		return int(name-globalOutput2) + 1, true
	}
	return 0, false
}

func (s *Server) OutputList() []Output {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outputListLocked()
}

func (s *Server) outputListLocked() []Output {
	if len(s.outputs) > 0 {
		out := make([]Output, len(s.outputs))
		copy(out, s.outputs)
		return out
	}
	w, h := s.ScreenW, s.ScreenH
	if w < 1 {
		w = 1920
	}
	if h < 1 {
		h = 1080
	}
	sc := s.scale120
	if sc == 0 {
		sc = PreferredScale120ths
	}
	return []Output{{
		Name: "WL-1", Desc: "worldr nested output",
		W: w, H: h, Scale120: sc,
	}}
}

// SetOutputs installs the logical output layout (copies).
func (s *Server) SetOutputs(outs []Output) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.outputs = append(s.outputs[:0], outs...)
	if len(s.outputs) > 0 && s.outputs[0].Scale120 != 0 {
		s.scale120 = s.outputs[0].Scale120
	}
	s.mu.Unlock()
}

// SetOutputScaleAt sets one output's scale and broadcasts.
func (s *Server) SetOutputScaleAt(i int, scale float64) {
	if s == nil {
		return
	}
	n := ScaleTo120ths(scale)
	s.mu.Lock()
	if len(s.outputs) == 0 {
		s.outputs = s.outputListLocked()
	}
	if i < 0 || i >= len(s.outputs) {
		s.mu.Unlock()
		return
	}
	prev := s.outputs[i].Scale120
	s.outputs[i].Scale120 = n
	if i == 0 {
		s.scale120 = n
	}
	s.mu.Unlock()
	if n != prev {
		s.BroadcastScale()
	}
}

// RelayoutOutputs re-tiles existing outputs to the current ScreenW/H.
func (s *Server) RelayoutOutputs() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if len(s.outputs) == 0 {
		s.mu.Unlock()
		return
	}
	n := len(s.outputs)
	scales := make([]float64, n)
	for i := range s.outputs {
		scales[i] = ScaleFrom120ths(s.outputs[i].Scale120)
	}
	next := LayoutOutputs(s.ScreenW, s.ScreenH, n, scales)
	changed := len(next) != len(s.outputs)
	if !changed {
		for i := range next {
			if next[i].X != s.outputs[i].X || next[i].Y != s.outputs[i].Y ||
				next[i].W != s.outputs[i].W || next[i].H != s.outputs[i].H {
				changed = true
				break
			}
		}
	}
	if changed {
		s.outputs = next
	}
	s.mu.Unlock()
	if changed {
		s.BroadcastOutputs()
	}
}

// BroadcastOutputs pushes geometry/mode/done for every bound wl_output.
func (s *Server) BroadcastOutputs() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.sendOutputLayout()
	}
}

// SyncSurfaceOutputs sends enter/leave when a window crosses an output.
func (s *Server) SyncSurfaceOutputs() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cl := append([]*Client(nil), s.clients...)
	s.mu.Unlock()
	for _, c := range cl {
		c.syncAllSurfaceOutputs()
	}
}

// OutputSeams are interior X edges (for the desktop divider).
func OutputSeams(outs []Output) []int {
	return OutputSeamsInto(nil, outs)
}

// OutputSeamsInto appends interior X edges into dst (reuses dst).
func OutputSeamsInto(dst []int, outs []Output) []int {
	dst = dst[:0]
	if len(outs) < 2 {
		return dst
	}
	for i := 1; i < len(outs); i++ {
		dst = append(dst, outs[i].X)
	}
	return dst
}
